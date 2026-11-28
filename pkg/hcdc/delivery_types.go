// Package cdc 提供按序交付和故障恢复的变更数据捕获能力。
package cdc

import "context"

// StreamIdentity 标识某条源流水线的一个代次。同一标识同时只能由一个进程向目标交付。
type StreamIdentity struct {
	PipelineID string `yaml:"PipelineID"` // 流水线标识。
	SourceID   string `yaml:"SourceID"`   // 逻辑源标识。
	Generation string `yaml:"Generation"` // 流代次。
}

// Cursor 表示源端已提交的位置。SourceIdentity 标识产生 File 和 Position 的物理成员。
// GTIDSet 覆盖截至该位置已提交的完整事务前缀。
type Cursor struct {
	SourceIdentity string `yaml:"SourceIdentity"` // 产生该物理位点的源成员标识。
	File           string `yaml:"File"`           // binlog 文件名。
	Position       uint32 `yaml:"Position"`       // binlog 文件中的字节偏移。
	Flavor         string `yaml:"Flavor"`         // GTID 方言。
	GTIDSet        string `yaml:"GTIDSet"`        // 位点处已提交的 GTID 集。
}

// Checkpoint 记录源端已连续交付的位置。序号零代表该代次的起始位置。
type Checkpoint struct {
	Identity          StreamIdentity
	Sequence          uint64
	Cursor            Cursor
	StructureRevision string
	// ZeroDateRows 为快照之后已推进至当前检查点的累计零日期映射行数。
	ZeroDateRows uint64 `json:",omitempty"`
}

// DeliveryConfig 将本地缓冲绑定到指定代次和起始游标。
// MappingVersion 标识重放时固定的路由和转换设置。
type DeliveryConfig struct {
	Directory      string         `yaml:"Directory"`      // 本地持久缓冲目录。
	Identity       StreamIdentity `yaml:"Identity"`       // 流水线及其代次标识。
	Baseline       Cursor         `yaml:"Baseline"`       // 本代次的起始游标。
	MappingVersion string         `yaml:"MappingVersion"` // 重放使用的路由及转换版本。
	// GTIDAutoPosition 允许在同一代次内切换物理成员。
	// 调用方必须先校验替代源，再追加该源的记录。
	GTIDAutoPosition bool `yaml:"GTIDAutoPosition"`
}

// OrderedRecord 描述已提交的事务及其目标交付计划。
// 源过滤条件排除事务时，空计划仍会推进游标。
type OrderedRecord struct {
	Sequence          uint64
	TransactionID     string
	Cursor            Cursor
	StructureRevision string
	Batches           []LoadBatch
	DDL               *DDLPlan `json:",omitempty"`
	// ZeroDateRows 为截至本记录的累计增量零日期映射行数。
	ZeroDateRows uint64 `json:",omitempty"`
}

// LoadBatch 描述一次不可变的目标请求。AppendRecord 设置 Label 和 Checksum。
// Payload 必须编码每次重试都要发送的确切字节。
type LoadBatch struct {
	TargetTable string
	RowCount    uint64
	Payload     []byte
	Label       string
	Checksum    string
}

// LoadState 描述查询或提交目标请求的结果。
type LoadState uint8

const (
	// LoadAbsent 表示目标适配器已确认可安全使用原 Label 提交批次。
	// 结果未知或确认信息过期时必须返回错误。
	LoadAbsent LoadState = iota + 1
	LoadPending
	LoadSucceeded
)

// LoadConfirmation 记录目标请求的标识和已观测到的结果。
type LoadConfirmation struct {
	State        LoadState
	Label        string
	Checksum     string
	LoadedRows   uint64
	FilteredRows uint64
}

// Target 确认一个或多个流的交付结果，并持久化检查点。
// 确认成功时必须将结果绑定到对应的不可变请求，结果不确定时禁止返回 LoadAbsent。
// 检查点必须单调推进，并支持写入后读回校验。
type Target interface {
	// LoadCheckpoint 读取指定流的最近检查点。
	LoadCheckpoint(ctx context.Context, identity StreamIdentity) (*Checkpoint, error)
	// PersistCheckpoint 持久化已确认的检查点。
	PersistCheckpoint(ctx context.Context, checkpoint Checkpoint) error
	// ConfirmLoad 核对指定导入批次的执行结果。
	ConfirmLoad(ctx context.Context, batch LoadBatch) (LoadConfirmation, error)
	// SubmitLoad 向目标提交一个不可变的导入批次。
	SubmitLoad(ctx context.Context, batch LoadBatch) (LoadConfirmation, error)
}

// DeliveryStatus 汇报流在本地和远端已校验的进度。
type DeliveryStatus struct {
	Durable      Checkpoint
	Delivered    Checkpoint
	PendingBytes int64
}

// DDLKind 表示有序屏障处的结构操作类型。
type DDLKind string

const (
	// DDLAddColumns 添加兼容的可空列。
	DDLAddColumns DDLKind = "add_columns"
	// DDLTruncate 在审批后清空所选表。
	DDLTruncate DDLKind = "truncate"
	// DDLDropColumn 在审批后删除一个支持的值列。
	DDLDropColumn DDLKind = "drop_column"
	// DDLWidenInteger 扩大源端整数类型的范围，保留 Doris 表结构。
	DDLWidenInteger DDLKind = "widen_integer"
	// DDLRenameColumn 在审批后重命名一个支持的值列。
	DDLRenameColumn DDLKind = "rename_column"
	// DDLUnsupported 记录需要新计划或新代次的屏障。
	DDLUnsupported DDLKind = "unsupported"
)

// DDLPlan 将源端操作与变更前后的 Doris 表结构关联起来。
// ID 将审批绑定到具体的流水线代次和源端位置。
type DDLPlan struct {
	ID               string
	Kind             DDLKind
	Before           DorisTable
	After            DorisTable
	RequiresApproval bool
	Sequence         uint64
	Cursor           Cursor
	Reason           string
}
