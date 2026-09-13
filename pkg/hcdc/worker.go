package hcdc

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

type WorkerOption func(w *Worker)

type AddData struct {
	schema   Schema
	table    Table
	action   Action
	columns  []ColumnInfo
	rows     [][]RawBytes
	position mysql.Position
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

func NewWorker(instanceId InstanceId, logDir string, options ...WorkerOption) *Worker {
	w := &Worker{
		instanceId:    instanceId,
		logDir:        logDir,
		rotateSize:    10 * 1024 * 1024,
		flushDuration: 5 * time.Second,
		addData:       make(chan *AddData, 1024),
		files:         make(map[SchemaTable]*File),
	}
	for _, option := range options {
		option(w)
	}
	return w
}

type Worker struct {
	instanceId    InstanceId
	logDir        string
	addData       chan *AddData
	rotateSize    int64
	flushDuration time.Duration // 刷新间隔
	lock          sync.RWMutex
	files         map[SchemaTable]*File
}

func (t *Worker) AddData(ctx context.Context, schema Schema, table Table, action Action, columns []ColumnInfo, rows [][]RawBytes, position mysql.Position) error {
	key := SchemaTable(string(schema) + "." + string(table))
	t.lock.RLock()
	_, ok := t.files[key]
	t.lock.RUnlock()

	if !ok {
		t.lock.Lock()
		if _, ok := t.files[key]; !ok {
			t.files[key] = NewFile(
				t.instanceId,
				schema,
				table,
				filepath.Join(t.logDir, strconv.FormatInt(int64(t.instanceId), 10)),
				WithFileRotateSize(t.rotateSize),
			)
		}
		t.lock.Unlock()
	}

	t.addData <- &AddData{
		columns:  columns,
		rows:     rows,
		action:   action,
		schema:   schema,
		table:    table,
		position: position,
	}
	return nil
}

func (t *Worker) loop(ctx context.Context) {
	flushTicker := time.NewTicker(t.flushDuration)
	defer flushTicker.Stop()

	defer func() {
		err := func() error {
			t.lock.RLock()
			defer t.lock.RUnlock()
			for _, val := range t.files {
				err := val.CheckAndRotate(ctx, false)
				if err != nil {
					return err
				}
			}
			return nil
		}()
		if err != nil {
			herror.PrintStack(ctx, err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-flushTicker.C:
			err := func() error {
				t.lock.RLock()
				defer t.lock.RUnlock()
				for _, val := range t.files {
					err := val.CloseFileUnderLock(ctx)
					if err != nil {
						return err
					}
				}
				return nil
			}()
			if err != nil {
				herror.PrintStack(ctx, err)
			}
		case data := <-t.addData:
			err := func() error {
				t.lock.RLock()
				defer t.lock.RUnlock()
				if val, ok := t.files[SchemaTable(string(data.schema)+"."+string(data.table))]; ok {
					err := val.AddData(ctx, data.action, data.columns, data.rows, data.position)
					if err != nil {
						return err
					}
					return nil
				}
				return nil
			}()
			if err != nil {
				herror.PrintStack(ctx, err)
			}
		}
	}
}

func (t *Worker) ScanFile(ctx context.Context, fn ScanFileCall) (InstanceId, *mysql.Position, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	var position *mysql.Position
	for _, val := range t.files {
		pos, err := val.ScanFile(ctx, fn)
		if err != nil {
			return t.instanceId, nil, err
		}
		if pos != nil && (position == nil || pos.Compare(*position) < 0) {
			position = pos // pos is *mysql.Position
		}
	}
	return t.instanceId, position, nil
}
