package hcdc

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type InstanceId int64
type Schema string
type Table string
type Column string
type SchemaTable string
type RawBytes []byte
type Action int

const (
	Insert Action = iota
	Update
	Delete
	Create
	Alter
	Drop
	Truncate
)

func (a Action) String() string {
	switch a {
	case Insert:
		return "insert"
	case Update:
		return "update"
	case Delete:
		return "delete"
	case Create:
		return "create"
	case Alter:
		return "alter"
	case Drop:
		return "drop"
	case Truncate:
		return "truncate"
	default:
		return "unknown"
	}
}

// SyncStatus 表示 svc -> Doris 数据同步的运行状态。
type SyncStatus int32

const (
	// SyncRunning 同步正常运行。
	SyncRunning SyncStatus = iota
	// SyncPaused 同步已暂停（通常是源表结构发生变化后自动暂停，等待人工确认并恢复）。
	SyncPaused
)

func (s SyncStatus) String() string {
	switch s {
	case SyncRunning:
		return "running"
	case SyncPaused:
		return "paused"
	default:
		return "unknown"
	}
}

// SyncState 描述当前 svc -> Doris 同步状态快照。
type SyncState struct {
	Status     SyncStatus `json:"status"`     // 当前状态
	Reason     string     `json:"reason"`     // 暂停原因
	PausedAt   time.Time  `json:"pausedAt"`   // 暂停时间（零值表示未暂停）
	Schema     Schema     `json:"schema"`     // 触发暂停的表结构变化所在库
	Table      Table      `json:"table"`      // 触发暂停的表
	InstanceId InstanceId `json:"instanceId"` // 触发暂停的实例 ID
}

type ColumnInfo struct {
	Name     Column
	Type     string
	Args     string
	Comment  string
	KeyIndex int
	IsNull   string
	Default  *string
	Action   string
	OldName  Column
	Charset  string // 源表列字符集（如 utf8mb4）。仅显式声明 CHARACTER SET 时非空：未声明或仅 COLLATE 时 parser 不回填，与 INFORMATION_SCHEMA.CHARACTER_SET_NAME（回填默认值）语义略有出入；数值类型/binary 归一化为空。Doris 原生 UTF-8 不消费，保留供审计与扩展（如转码）。
	Unsigned bool   // 无符号整数列，用于重建 canal 表缓存时恢复 UnsignedColumns；Doris 建表不消费，仅随快照持久化。
}

func (i ColumnInfo) String() string {
	return string(i.Name) + ":" + i.Type
}

type TableInfo struct {
	Schema         Schema
	Table          Table
	Columns        []ColumnInfo
	Index          map[Column]int
	Keys           []Column
	PartitionField Column // 分区字段名
	PartitionType  string // 分区类型，目前主流为 "RANGE"
	Action         Action
}

// TableSchemaVersion 是 cdc_record.table_schema 表的一行：某表自 (DDLFile, DDLPos)
// 位点起生效的结构快照。续传时按「生效位点 <= 恢复位点」选取最新版本还原表结构。
type TableSchemaVersion struct {
	Schema  Schema
	Table   Table
	DDLFile string    // 结构生效的 binlog 文件
	DDLPos  uint32    // 结构生效的 binlog 位点
	Info    TableInfo // 该位点起生效的完整列结构
}

type Config struct {
	Canals []CanalConfig `yaml:"Canals"` // Canal 配置
	Doris  *DorisConfig  `yaml:"Doris"`  // Doris 配置
}

func (c *Config) Validate(ctx context.Context) bool {
	if c == nil {
		hlog.Error(ctx, "config is nil")
		return false
	}
	if c.Doris == nil {
		hlog.Error(ctx, "config doris is nil")
		return false
	}
	if !c.Doris.Validate(ctx) {
		return false
	}
	for i := range c.Canals {
		if !c.Canals[i].Validate(ctx) {
			return false
		}
	}
	return true
}

func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	if len(c.Canals) != len(other.Canals) {
		return false
	}
	for i := range c.Canals {
		if !c.Canals[i].Equal(&other.Canals[i]) {
			return false
		}
	}
	return c.Doris.Equal(other.Doris)

}

type HCDC struct {
	canal atomic.Pointer[[]*Canal]
	doris atomic.Pointer[Doris]
	cfg   *Config
}

func NewHCDC() *HCDC {
	return &HCDC{}
}

func (h *HCDC) SetConfig(ctx context.Context, cfg *Config) error {
	if h.cfg != nil && h.cfg.Equal(cfg) {
		return nil
	}

	if cfg == nil {
		if h.doris.Load() != nil {
			_ = h.doris.Load().Close()
		}
		h.doris.Store(nil)

		canals := h.canal.Load()
		if canals != nil {
			for _, canal := range *canals {
				canal.Close()
			}
		}
		h.canal.Store(nil)
		return nil
	}

	if !cfg.Validate(ctx) {
		return herror.NewError("config validate failed")
	}

	doris := NewDoris(cfg.Doris)
	err := doris.Open(ctx)
	if err != nil {
		return err
	}
	h.doris.Store(doris)

	canals := make([]*Canal, len(cfg.Canals))
	for i, item := range cfg.Canals {
		canals[i] = NewCanal(&item)
		h.setCanalCall(ctx, canals[i])
		err := canals[i].Open(ctx)
		if err != nil {
			for j := 0; j < i; j++ {
				canals[j].Close()
			}
			_ = doris.Close()
			return err
		}
	}
	h.canal.Store(&canals)

	doris.setInitialized()
	return nil
}

func (h *HCDC) setCanalCall(ctx context.Context, canal *Canal) {
	canal.setOnCreateSchema(func(ctx context.Context, schema Schema) error {
		doris := h.doris.Load()
		if doris == nil {
			return nil
		}
		return doris.CreateSchema(ctx, schema)
	})

	canal.setOnCreateTable(func(ctx context.Context, infos []TableInfo) error {
		doris := h.doris.Load()
		if doris == nil {
			return nil
		}
		doris.ChangeTables(ctx, infos)
		return nil
	})

	canal.setOnGetPosition(func(ctx context.Context, instanceId InstanceId) (*SyncPosition, error) {
		doris := h.doris.Load()
		if doris == nil {
			return nil, nil
		}
		return doris.GetPosition(ctx, instanceId)
	})

	canal.setOnLoadSchemas(func(ctx context.Context, instanceId InstanceId) (map[Schema]map[Table][]TableSchemaVersion, error) {
		doris := h.doris.Load()
		if doris == nil {
			return nil, nil
		}
		return doris.LoadTableSchemas(ctx, instanceId)
	})

	canal.setOnSaveTableSchema(func(ctx context.Context, instanceId InstanceId, info TableInfo, pos mysql.Position) error {
		doris := h.doris.Load()
		if doris == nil {
			return nil
		}
		return doris.SaveTableSchema(ctx, instanceId, info, pos)
	})

	doris := h.doris.Load()
	doris.RegisterWorker(ctx, canal.worker)
	// 采集端与上传端共享同一个监控器，便于聚合端到端指标。
	canal.setMonitor(doris.monitor)
}

// Pause 暂停 svc -> Doris 的数据同步。MySQL -> svc 的采集不受影响，数据继续缓冲到本地文件。  若 doris 尚未初始化（未 SetConfig），则为无操作。
func (h *HCDC) Pause(ctx context.Context, reason string) {
	doris := h.doris.Load()
	if doris == nil {
		return
	}
	doris.Pause(ctx, reason)
}

// Resume 手动恢复 svc -> Doris 的数据同步。
func (h *HCDC) Resume(ctx context.Context) {
	doris := h.doris.Load()
	if doris == nil {
		return
	}
	doris.Resume(ctx)
}

// GetSyncState 返回当前 svc -> Doris 同步状态快照。
func (h *HCDC) GetSyncState() SyncState {
	doris := h.doris.Load()
	if doris == nil {
		return SyncState{Status: SyncRunning}
	}
	return doris.GetSyncState()
}

// GetMetrics 返回当前监控指标快照（采集/上传吞吐、延迟、错误、DDL、暂停事件等）。
func (h *HCDC) GetMetrics() Snapshot {
	doris := h.doris.Load()
	if doris == nil {
		return Snapshot{}
	}
	return doris.monitor.Snapshot()
}

// GetColumnType 获取列类型，处理 bitmap 类型的特殊注释。
func GetColumnType(col *ColumnInfo) string {
	if col.Type == "binary" || col.Type == "varbinary" || col.Type == "blob" || col.Type == "longblob" || col.Type == "mediumblob" {
		if strings.Contains(col.Comment, "CustomType=Bitmap64") {
			return "bitmap64"
		} else if strings.Contains(col.Comment, "CustomType=Bitmap32") {
			return "bitmap32"
		}
	}
	return col.Type
}
