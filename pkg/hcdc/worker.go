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

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type WorkerOption func(w *Worker)

type AddData struct {
	columns string
	rows    [][]RawBytes
	action  Action
}

func WithFlushDuration(d time.Duration) WorkerOption {
	return func(w *Worker) {
		w.flushDuration = d
	}
}

func WithRotateSize(size int64) WorkerOption {
	return func(w *Worker) {
		w.rotateSize = size
	}
}

func WithAddDataChanSize(size int) WorkerOption {
	return func(w *Worker) {
		w.addData = make(chan *AddData, size)
	}
}

func NewWorker(schema Schema, table Table, logDir string, options ...WorkerOption) *Worker {
	w := &Worker{
		schema:        schema,
		table:         table,
		logDir:        logDir,
		rotateSize:    10 * 1024 * 1024,
		flushDuration: 5 * time.Second,
		addData:       make(chan *AddData, 1024),
	}
	for _, option := range options {
		option(w)
	}
	return w
}

type Worker struct {
	schema        Schema
	table         Table
	logDir        string
	columns       string // 缓存当前 Schema 字符串
	addData       chan *AddData
	file          *os.File
	writtenSize   int64
	countSize     int64
	rotateSize    int64
	flushDuration time.Duration // 刷新间隔
}

func (t *Worker) AddData(ctx context.Context, action Action, columns []Column, rows [][]RawBytes) error {
	// 组装当前批次的列字符串
	var sb strings.Builder
	if len(columns) > 0 {
		sb.WriteString(string(columns[0]))
		for _, col := range columns[1:] {
			sb.WriteString(",")
			sb.WriteString(string(col))
		}
	}

	t.addData <- &AddData{
		columns: sb.String(),
		rows:    rows,
		action:  action,
	}
	return nil
}

func (t *Worker) loop(ctx context.Context) {
	flushTicker := time.NewTicker(t.flushDuration)
	defer flushTicker.Stop()

	defer func() {
		err := t.closeFileUnderLock(ctx)
		if err != nil {
			hlog.Error(ctx, "close file failed: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-flushTicker.C:
			_ = t.checkAndRotate(ctx, true)
		case data := <-t.addData:
			err := t.save(ctx, data.action, data.columns, data.rows)
			if err != nil {
				herror.PrintStack(ctx, err)
				return
			}
			_ = t.checkAndRotate(ctx, false)
		}
	}
}

func (t *Worker) checkAndRotate(ctx context.Context, force bool) error {
	if (t.writtenSize >= t.rotateSize && t.rotateSize > 0) || (force && t.countSize > 0) {
		_, err := t.createFileUnderLock(ctx)
		return err
	}
	return nil
}

func (t *Worker) createFileUnderLock(ctx context.Context) (*os.File, error) {
	err := t.closeFileUnderLock(ctx)
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(t.logDir, string(t.schema), string(t.table))
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, herror.Wrap(err)
	}

	path := filepath.Join(dir, strconv.FormatInt(time.Now().UnixNano(), 10)+".csv.temp")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, herror.Wrap(err)
	}

	// ✨ 核心机制 2：在创建新文件的第一行，强行写入当前 Schema Header 加上系统扩展字段名
	header := fmt.Sprintf("%s,__op\n", t.columns)
	n, err := file.WriteString(header)
	if err != nil {
		_ = file.Close()
		return nil, herror.Wrap(err)
	}

	t.file = (file)
	t.writtenSize = int64(n) // 初始大小包含 Header
	return file, nil
}

func (t *Worker) closeFileUnderLock(ctx context.Context) error {
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
		newName := name[:len(name)-5] + ".active"
		if err := os.Rename(name, newName); err != nil {
			return err
		}
	}
	return nil
}

func (t *Worker) save(ctx context.Context, action Action, columns string, rows [][]RawBytes) error {
	file := t.file
	if file == nil || t.columns != columns {
		t.columns = columns
		var err error
		file, err = t.createFileUnderLock(ctx)
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

	t.writtenSize += int64(n)
	return nil
}

func (t *Worker) readFile(ctx context.Context, fn func(ctx context.Context, columns string, reader io.Reader) error) error {
	pattern := filepath.Join(t.logDir, string(t.schema), string(t.table), "*.active")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return herror.Wrap(err)
	}

	for _, path := range paths {
		var columns string

		err := func() error {
			file, err := os.Open(path)
			if err != nil {
				return herror.Wrap(err)
			}
			defer file.Close()

			// ✨ 核心机制 3：使用 bufio 动态剥离第一行 Header
			bufReader := bufio.NewReader(file)
			headerLine, err := bufReader.ReadString('\n')
			if err != nil {
				return herror.Wrap(fmt.Errorf("read csv header failed: %v", err))
			}

			// 擦除末尾的换行符，拿到该文件专属性的 columns 字段集
			columns = strings.TrimSpace(headerLine)
			if columns == "" {
				return herror.NewError("empty csv header in active file")
			}

			// 把剥离了首行、剩下纯纯数据行的 bufReader 流直接喂给外部的 Doris 加载器
			if err = fn(ctx, columns, bufReader); err != nil {
				return err
			}
			return nil
		}()

		if err != nil {
			return err // 失败则保留文件，等下个周期重试
		}

		// 成功上传后物理清理
		if err := os.Remove(path); err != nil {
			return herror.Wrap(err)
		}
	}
	return nil
}
