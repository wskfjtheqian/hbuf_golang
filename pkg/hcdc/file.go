package hcdc

import (
	"bufio"
	"context"
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

type ScanFileCall func(ctx context.Context, schema Schema, table Table, columns string, reader io.Reader) error

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

func (t *File) AddData(ctx context.Context, action Action, columns []ColumnInfo, rows [][]RawBytes, position mysql.Position) error {
	err := t.save(ctx, action, columns, rows, position)
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

	path := filepath.Join(dir, strconv.FormatInt(time.Now().UnixNano(), 10)+".csv.temp")
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
		newName := filepath.Base(t.position.Name) + "_" + strconv.FormatUint(uint64(t.position.Pos), 10) + ".csv.active"
		path, _ := filepath.Split(name)
		newName = filepath.Join(path, newName)
		if err := os.Rename(name, newName); err != nil {
			return err
		}
	}
	return nil
}

func (t *File) save(ctx context.Context, action Action, columns []ColumnInfo, rows [][]RawBytes, position mysql.Position) error {
	file := t.file
	if file == nil || !hutl.EqualSlice(t.columns, columns) {
		t.columns = columns
		var err error
		if err = t.CloseFileUnderLock(ctx); err != nil {
			return err
		}

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
	t.writtenSize += int64(n)
	return nil
}

func (t *File) ScanFile(ctx context.Context, fn ScanFileCall) (*mysql.Position, error) {
	pattern := filepath.Join(t.logDir, string(t.schema), string(t.table), "*.active")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, herror.Wrap(err)
	}

	var position *mysql.Position
	for _, path := range paths {
		pos, err := t.ReadFile(ctx, path, fn)
		if err != nil {
			return nil, err // 失败则保留文件，等下个周期重试
		}

		if position == nil || pos.Compare(*position) < 0 {
			position = pos
		}

		// 成功上传后物理清理
		if err := os.Remove(path); err != nil {
			return nil, herror.Wrap(err)
		}
	}
	return position, nil
}

func (t *File) ReadFile(ctx context.Context, path string, fn ScanFileCall) (*mysql.Position, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer file.Close()

	// ✨ 核心机制 3：使用 bufio 动态剥离第一行 Header
	bufReader := bufio.NewReader(file)
	headerLine, err := bufReader.ReadString('\n')
	if err != nil {
		return nil, herror.Wrap(fmt.Errorf("read csv header failed: %v", err))
	}

	// 擦除末尾的换行符，拿到该文件专属性的 columns 字段集
	columns := strings.TrimSpace(headerLine)
	if columns == "" {
		return nil, herror.NewError("empty csv header in active file")
	}

	// 把剥离了首行、剩下纯纯数据行的 bufReader 流直接喂给外部的 Doris 加载器
	if err = fn(ctx, t.schema, t.table, columns, bufReader); err != nil {
		return nil, err
	}

	_, name := filepath.Split(path)
	if name == "SELECT" {
		return nil, herror.NewError("empty file name")
	}

	name = strings.TrimSuffix(name, ".csv.active")
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return nil, herror.NewError("invalid position line: " + name)
	}
	pos, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	position := &mysql.Position{
		Name: parts[0],
		Pos:  uint32(pos),
	}

	return position, nil
}
