package hcdc

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

// ScanFileCall 上传回调。capturedAt 为该文件的数据被采集（写入 .temp）的时间，
// 用于在 Doris 侧计算端到端延迟（采集 -> 上传完成）。
type ScanFileCall func(ctx context.Context, schema Schema, table Table, columns string, capturedAt time.Time, reader io.Reader) error

// ScanTruncateCall 是 TRUNCATE 控制文件的处理回调。由 Doris 侧执行目标表的
// TRUNCATE TABLE SQL。position / gtid 随控制文件一起传入，用于位点汇总。
type ScanTruncateCall func(ctx context.Context, schema Schema, table Table, position mysql.Position, gtid string, capturedAt time.Time) error

type FileOption func(w *File)

func WithFileRotateSize(size int64) FileOption {
	return func(w *File) {
		w.rotateSize = size
	}
}

type File struct {
	instanceId  InstanceId
	schema      Schema
	table       Table
	logDir      string
	rotateSize  int64
	file        *os.File
	writtenSize int64
	countSize   int64
	columns     []ColumnInfo
	position    mysql.Position
	gtid        string    // 当前数据对应的 GTID 集合字符串，随 .active 文件名持久化（base64 编码）
	captureTime time.Time // 当前 .temp 文件首个数据被采集的时间，随 .active 文件名持久化
}

func NewFile(id InstanceId, schema Schema, table Table, logDir string, options ...FileOption) *File {
	ret := &File{
		instanceId: id,
		schema:     schema,
		table:      table,
		logDir:     logDir,
		rotateSize: 10 * 1024 * 1024,
	}
	for _, option := range options {
		option(ret)
	}
	return ret
}

func (t *File) AddData(ctx context.Context, action Action, columns []ColumnInfo, rows [][]RawBytes, position mysql.Position, gtid string) error {
	err := t.save(ctx, action, columns, rows, position, gtid)
	if err != nil {
		return err
	}
	return t.CheckAndRotate(ctx, false)
}

func (t *File) CheckAndRotate(ctx context.Context, force bool) error {
	if (t.writtenSize >= t.rotateSize && t.rotateSize > 0) || (force && t.countSize > 0) {
		return t.CloseFileUnderLock(ctx)
	}
	return nil
}

// truncateFileSuffix 是 TRUNCATE 控制文件的扩展名。上传端 scanFile 优先识别
// 该后缀：命中则对目标表执行 TRUNCATE TABLE，成功后删除控制文件并推进位点。
const truncateFileSuffix = ".csv.truncate"

// Truncate 处理上游 TRUNCATE 事件：
//  1. 关闭并删除当前 .temp 句柄（TRUNCATE 之前采集的数据全部作废，不再上传）；
//  2. 删除该表目录下此前积压的全部 .temp / .active / .truncate 文件；
//  3. 写一个独立的 .truncate 控制文件，文件名携带位点、GTID 与采集时间戳。
//
// 由 worker.loop 在持有 worker 读锁的串行上下文中调用，与 DML 严格有序。
func (t *File) Truncate(ctx context.Context, position mysql.Position, gtid string) error {
	// 1. 关闭当前写入句柄并从磁盘删除（若已被 rotate 成 .active，会在步骤 2 统一清理）。
	if t.file != nil {
		name := t.file.Name()
		_ = t.file.Close()
		t.file = nil
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			return herror.Wrap(err)
		}
	}
	t.writtenSize = 0
	t.countSize = 0
	t.columns = nil

	// 2. 清理该表目录下此前的全部缓冲文件。
	dir := filepath.Join(t.logDir, string(t.schema), string(t.table))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return herror.Wrap(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return herror.Wrap(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".csv.temp") || strings.HasSuffix(name, ".csv.active") || strings.HasSuffix(name, truncateFileSuffix) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				return herror.Wrap(err)
			}
		}
	}

	// 3. 写独立控制文件。文件名格式与 .active 保持一致的前缀，便于解析位点/GTID。
	captureTime := time.Now()
	gtidB64 := base64.RawURLEncoding.EncodeToString([]byte(gtid))
	name := filepath.Base(position.Name) + "_" + strconv.FormatUint(uint64(position.Pos), 10) + "_" + gtidB64 + "_" + strconv.FormatInt(captureTime.UnixNano(), 10) + truncateFileSuffix
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("TRUNCATE\n"), 0644); err != nil {
		return herror.Wrap(err)
	}

	// 同步更新内存位点：TRUNCATE 之后的新 DML 会落在更新的位点之后。
	t.position = position
	t.gtid = gtid
	t.captureTime = captureTime
	return nil
}

func (t *File) createFileUnderLock(ctx context.Context, columns []ColumnInfo, position mysql.Position) (*os.File, error) {
	dir := filepath.Join(t.logDir, string(t.schema), string(t.table))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, herror.Wrap(err)
	}

	t.captureTime = time.Now()
	path := filepath.Join(dir, strconv.FormatInt(t.captureTime.UnixNano(), 10)+".csv.temp")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, herror.Wrap(err)
	}

	// ✨ 核心机制 2：在创建新文件的第一行，强行写入当前 Schema Header 加上系统扩展字段名
	header := hutl.Slice(columns, func(i int, col ColumnInfo) string {
		switch col.Type {
		case "boolean", "bool", "decimal", "numeric", "double", "real", "float", "tinyint", "smallint", "int", "integer", "mediumint", "bigint", "largeint", "time", "date", "datetime", "timestamp", "year":
			return "`" + string(col.Name) + "`"
		}
		return "`" + string(col.Name) + "_base`"
	})
	header = append(header, "`__op`")

	decoders := hutl.Slice(hutl.Filter(columns, func(info ColumnInfo) bool {
		switch info.Type {
		case "boolean", "bool", "decimal", "numeric", "double", "real", "float", "tinyint", "smallint", "int", "integer", "mediumint", "bigint", "largeint", "time", "date", "datetime", "timestamp":
			return false
		}
		return true
	}), func(i int, v ColumnInfo) string {
		typ := GetColumnType(&v)
		if typ == "bitmap32" || typ == "bitmap64" {
			return "`" + string(v.Name) + "`= bitmap_from_string(from_base64(`" + string(v.Name) + "_base`))"
		}
		return "`" + string(v.Name) + "`= from_base64(`" + string(v.Name) + "_base`)"
	})

	header = append(header, decoders...)
	n, err := file.WriteString(strings.Join(header, ",") + "\n")
	if err != nil {
		_ = file.Close()
		return nil, herror.Wrap(err)
	}

	t.file = file
	t.writtenSize = int64(n) // 初始大小包含 Header
	return file, nil
}

func (t *File) CloseFileUnderLock(ctx context.Context) error {
	file := t.file
	if t.file == nil {
		return nil
	}

	_ = file.Sync()
	_ = file.Close()
	t.file = nil
	t.writtenSize = 0
	t.countSize = 0

	name := file.Name()
	if strings.HasSuffix(name, ".temp") {
		// 文件名保留采集时间戳（供上传端计算端到端延迟）与 GTID（供断点续传），
		// GTID 用 base64 编码避免其中的 '-' ':' ',' 等字符与 '_' 分隔符冲突。
		gtidB64 := base64.RawURLEncoding.EncodeToString([]byte(t.gtid))
		newName := filepath.Base(t.position.Name) + "_" + strconv.FormatUint(uint64(t.position.Pos), 10) + "_" + gtidB64 + "_" + strconv.FormatInt(t.captureTime.UnixNano(), 10) + ".csv.active"
		path, _ := filepath.Split(name)
		newName = filepath.Join(path, newName)
		if err := os.Rename(name, newName); err != nil {
			return err
		}
	}
	return nil
}

func (t *File) save(ctx context.Context, action Action, columns []ColumnInfo, rows [][]RawBytes, position mysql.Position, gtid string) error {
	file := t.file
	// 事务边界检测：GTID 变化表示「新事务开始」。此时先 flush 上一个事务的数据，
	// 保证单个 .active 文件不跨事务切分（Doris 单次 Stream Load 具备事务性，
	// 崩溃时该事务要么完整、要么完全未上传，绝不出现半截事务）。
	// 仅当新旧 GTID 都非空时才判定（空 GTID 表示源库未开启 GTID，无法识别边界，
	// 退化为按 rotateSize/flushDuration 切分）。
	newTxn := gtid != "" && t.gtid != "" && t.gtid != gtid
	if file == nil || !hutl.EqualSlice(t.columns, columns) || newTxn {
		t.columns = columns
		var err error
		if err = t.CloseFileUnderLock(ctx); err != nil {
			return err
		}

		// 先记录位点再建文件：createFileUnderLock 与 CloseFileUnderLock 生成的
		// .active 文件名依赖 t.position（binlog 文件名 + offset），
		// 若延后到 save 末尾赋值，首个文件会用零值位点（空文件名）导致断点续传失败。
		t.position = position
		t.gtid = gtid
		file, err = t.createFileUnderLock(ctx, columns, position)
		if err != nil {
			return err
		}
	}

	var sb strings.Builder
	for _, row := range rows {
		for i, val := range row {
			if i > 0 {
				sb.WriteString(",")
			}
			if val != nil {
				s := string(val)
				s = strings.ReplaceAll(s, ",", " ")
				s = strings.ReplaceAll(s, "\n", " ")
				s = strings.ReplaceAll(s, "\r", " ")
				sb.WriteString(s)
			}
		}
		sb.WriteString(",")
		sb.WriteString(strconv.Itoa(int(action)))
		sb.WriteString("\n")
		t.countSize++
	}

	strData := sb.String()
	n, err := file.WriteString(strData)
	if err != nil {
		return herror.Wrap(err)
	}
	t.position = position
	t.gtid = gtid
	t.writtenSize += int64(n)
	return nil
}

func (t *File) ScanFile(ctx context.Context, fn ScanFileCall, truncateFn ScanTruncateCall) (*mysql.Position, string, error) {
	dir := filepath.Join(t.logDir, string(t.schema), string(t.table))

	var position *mysql.Position
	var gtid string

	// 1. 优先处理 TRUNCATE 控制文件：必须先清表，再上传该表后续的数据文件。
	// 控制文件在执行成功后才删除，失败则保留，下个扫描周期重试（TRUNCATE 幂等）。
	truncates, err := filepath.Glob(filepath.Join(dir, "*"+truncateFileSuffix))
	if err != nil {
		return nil, "", herror.Wrap(err)
	}
	for _, path := range truncates {
		pos, g, capturedAt, err := parseTruncateFileName(path)
		if err != nil {
			return nil, "", err
		}
		if truncateFn != nil {
			if err := truncateFn(ctx, t.schema, t.table, *pos, g, capturedAt); err != nil {
				return nil, "", err
			}
		}
		if err := os.Remove(path); err != nil {
			return nil, "", herror.Wrap(err)
		}
		if position == nil || pos.Compare(*position) < 0 {
			position = pos
		}
		if g != "" {
			gtid = g
		}
	}

	// 2. 处理常规 .active 数据文件。
	pattern := filepath.Join(dir, "*.active")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, "", herror.Wrap(err)
	}

	for _, path := range paths {
		pos, g, err := t.ReadFile(ctx, path, fn)
		if err != nil {
			return nil, "", err // 失败则保留文件，等下个周期重试
		}

		if pos != nil && (position == nil || pos.Compare(*position) < 0) {
			position = pos
		}
		if g != "" {
			gtid = g
		}

		// 成功上传后物理清理
		if err := os.Remove(path); err != nil {
			return nil, "", herror.Wrap(err)
		}
	}
	return position, gtid, nil
}

// parseTruncateFileName 从 TRUNCATE 控制文件名解析位点、GTID 与采集时间戳。
// 文件名格式：{binlogName}_{pos}_{gtidB64}_{captureNano}.csv.truncate
func parseTruncateFileName(path string) (*mysql.Position, string, time.Time, error) {
	_, name := filepath.Split(path)
	name = strings.TrimSuffix(name, truncateFileSuffix)
	parts := strings.SplitN(name, "_", 4)
	if len(parts) != 4 {
		return nil, "", time.Time{}, herror.NewError("invalid truncate control file name: " + name)
	}
	pos, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, "", time.Time{}, herror.Wrap(err)
	}
	gtid := ""
	if parts[2] != "" {
		if raw, err := base64.RawURLEncoding.DecodeString(parts[2]); err == nil {
			gtid = string(raw)
		}
	}
	nano, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return nil, "", time.Time{}, herror.Wrap(err)
	}
	return &mysql.Position{Name: parts[0], Pos: uint32(pos)}, gtid, time.Unix(0, nano), nil
}

func (t *File) ReadFile(ctx context.Context, path string, fn ScanFileCall) (*mysql.Position, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", herror.Wrap(err)
	}
	defer file.Close()

	// ✨ 核心机制 3：使用 bufio 动态剥离第一行 Header
	bufReader := bufio.NewReader(file)
	headerLine, err := bufReader.ReadString('\n')
	if err != nil {
		return nil, "", herror.Wrap(fmt.Errorf("read csv header failed: %v", err))
	}

	// 擦除末尾的换行符，拿到该文件专属性的 columns 字段集
	columns := strings.TrimSpace(headerLine)
	if columns == "" {
		return nil, "", herror.NewError("empty csv header in active file")
	}

	// 从文件名解析采集时间戳（格式：{binlogName}_{pos}_{gtidB64}_{captureNano}.csv.active），
	// 用于在上传端计算端到端延迟。
	capturedAt := parseCaptureTime(path)

	// 把剥离了首行、剩下纯纯数据行的 bufReader 流直接喂给外部的 Doris 加载器
	if err = fn(ctx, t.schema, t.table, columns, capturedAt, bufReader); err != nil {
		return nil, "", err
	}

	_, name := filepath.Split(path)
	if strings.HasPrefix(name, "SELECT_") {
		return nil, "", nil
	}
	name = strings.TrimSuffix(name, ".csv.active")
	parts := strings.SplitN(name, "_", 4)
	if len(parts) != 4 {
		return nil, "", herror.NewError("invalid position line: " + name)
	}
	pos, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, "", herror.Wrap(err)
	}
	position := &mysql.Position{
		Name: parts[0],
		Pos:  uint32(pos),
	}

	gtid := ""
	if parts[2] != "" {
		if raw, err := base64.RawURLEncoding.DecodeString(parts[2]); err == nil {
			gtid = string(raw)
		}
	}

	return position, gtid, nil
}

// parseCaptureTime 从 .active 文件名中解析采集时间戳。
// 文件名格式：{binlogName}_{pos}_{gtidB64}_{captureNano}.csv.active。
// 解析失败或格式不匹配（如 SELECT_ 前缀的位点文件）时返回零值时间。
func parseCaptureTime(path string) time.Time {
	_, name := filepath.Split(path)
	if strings.HasPrefix(name, "SELECT_") {
		return time.Time{}
	}
	name = strings.TrimSuffix(name, ".csv.active")
	parts := strings.SplitN(name, "_", 4)
	if len(parts) != 4 {
		return time.Time{}
	}
	nano, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// Backlog 返回该表当前积压待上传的 .active 文件数与总字节数。
func (t *File) Backlog() (files int64, bytes int64) {
	pattern := filepath.Join(t.logDir, string(t.schema), string(t.table), "*.active")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return 0, 0
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		files++
		bytes += info.Size()
	}
	return files, bytes
}
