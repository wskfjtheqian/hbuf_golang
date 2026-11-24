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
	info     TableInfo // DDL barrier：DDL 后该表的完整结构快照（仅 Create/Alter/Drop 时非零）
	position mysql.Position
	gtid     string
	ack      chan error
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

func (t *Worker) AddData(ctx context.Context, schema Schema, table Table, action Action, columns []ColumnInfo, rows [][]RawBytes, position mysql.Position, gtid string) error {
	t.ensureFile(schema, table)

	t.addData <- &AddData{
		columns:  columns,
		rows:     rows,
		action:   action,
		schema:   schema,
		table:    table,
		position: position,
		gtid:     gtid,
	}
	return nil
}

// ensureFile 确保该表的 File 缓冲存在（懒创建，双重检查加锁）。
func (t *Worker) ensureFile(schema Schema, table Table) {
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
}

// Truncate 处理上游 TRUNCATE 事件：与 AddData 共用同一 channel，保证该表在
// TRUNCATE 位点之前的 DML 已全部落盘，之后才清理旧文件并写入控制文件。
// 调用方（canal.OnDDL）在 canal 同步 goroutine 内串行发起，并阻塞等待 ack。
func (t *Worker) Truncate(ctx context.Context, schema Schema, table Table, position mysql.Position, gtid string) error {
	t.ensureFile(schema, table)

	ack := make(chan error, 1)
	t.addData <- &AddData{
		schema:   schema,
		table:    table,
		position: position,
		gtid:     gtid,
		action:   Truncate,
		ack:      ack,
	}
	return <-ack
}

// ApplyDDL 处理上游 DDL：与 AddData 共用串行通道保证 DDL 位点前的 DML 已落盘，
// 再写 barrier 控制文件（info 为 DDL 后该表的完整结构快照）。
func (t *Worker) ApplyDDL(ctx context.Context, schema Schema, table Table, info TableInfo, position mysql.Position, gtid string) error {
	t.ensureFile(schema, table)

	ack := make(chan error, 1)
	t.addData <- &AddData{
		schema:   schema,
		table:    table,
		position: position,
		gtid:     gtid,
		action:   info.Action,
		info:     info,
		ack:      ack,
	}
	return <-ack
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
				val, ok := t.files[SchemaTable(string(data.schema)+"."+string(data.table))]
				if !ok {
					return nil
				}
				if data.action == Truncate {
					err := val.Truncate(ctx, data.position, data.gtid)
					if data.ack != nil {
						data.ack <- err
					}
					return err
				}
				if data.action == Create || data.action == Alter || data.action == Drop {
					err := val.ApplyDDL(ctx, data.info, data.position, data.gtid)
					if data.ack != nil {
						data.ack <- err
					}
					return err
				}
				err := val.AddData(ctx, data.action, data.columns, data.rows, data.position, data.gtid)
				if err != nil {
					return err
				}
				return nil
			}()
			if err != nil {
				herror.PrintStack(ctx, err)
			}
		}
	}
}

func (t *Worker) ScanFile(ctx context.Context, fn ScanFileCall, truncateFn ScanTruncateCall, ddlFn ScanDDLCall) (InstanceId, *mysql.Position, string, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	var position *mysql.Position
	var gtid string
	for _, val := range t.files {
		pos, g, err := val.ScanFile(ctx, fn, truncateFn, ddlFn)
		if err != nil {
			return t.instanceId, nil, "", err
		}
		if pos != nil && (position == nil || pos.Compare(*position) < 0) {
			position = pos // pos is *mysql.Position
		}
		if g != "" {
			gtid = g
		}
	}
	return t.instanceId, position, gtid, nil
}

// Backlog 聚合该 worker 下所有表的积压 .active 文件数与总字节数。
func (t *Worker) Backlog() (files int64, bytes int64) {
	t.lock.RLock()
	defer t.lock.RUnlock()
	for _, val := range t.files {
		f, b := val.Backlog()
		files += f
		bytes += b
	}
	return files, bytes
}
