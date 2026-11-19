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
	if file == nil || !hutl.EqualSlice(t.columns, columns) {
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

func (t *File) ScanFile(ctx context.Context, fn ScanFileCall) (*mysql.Position, string, error) {
	pattern := filepath.Join(t.logDir, string(t.schema), string(t.table), "*.active")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, "", herror.Wrap(err)
	}

	var position *mysql.Position
	var gtid string
	for _, path := range paths {
		pos, g, err := t.ReadFile(ctx, path, fn)
		if err != nil {
			return nil, "", err // 失败则保留文件，等下个周期重试
		}

		if position == nil || pos.Compare(*position) < 0 {
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
