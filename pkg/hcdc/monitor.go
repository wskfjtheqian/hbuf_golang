package hcdc

import (
	"sync"
	"sync/atomic"
	"time"
)

// Monitor 是 hcdc 同步链路的内置监控器。
// 采用「原子计数器 + 读写锁」实现，零第三方依赖，多 goroutine 并发安全。
// 监控维度覆盖：采集/上传吞吐、错误计数、延迟统计、同步状态事件、缓冲积压。
type Monitor struct {
	// 采集端（MySQL -> svc，即 Canal.OnRow -> Worker.AddData）
	CaptureRows     atomic.Int64 // 累计采集行数
	CaptureBytes    atomic.Int64 // 累计采集字节数
	CaptureErrors   atomic.Int64 // 采集失败次数

	// 上传端（svc -> Doris，即 Doris.scanFile -> StreamSave）
	UploadRows       atomic.Int64 // 累计上传行数
	UploadBytes      atomic.Int64 // 累计上传字节数
	UploadFiles      atomic.Int64 // 累计上传文件数
	UploadErrors     atomic.Int64 // 上传失败次数
	UploadTotalMs    atomic.Int64 // 上传累计耗时（毫秒）
	uploadMaxMs      atomic.Int64 // 上传单次最大耗时（毫秒）

	// DDL 结构变更
	DDLCount      atomic.Int64 // 累计处理 DDL 次数
	DDLErrors     atomic.Int64 // DDL 处理失败次数
	CreateTable   atomic.Int64 // 建表次数
	AlterTable    atomic.Int64 // 改表次数
	DropTable     atomic.Int64 // 删表次数

	// 同步状态事件
	PauseCount   atomic.Int64 // 累计暂停次数
	ResumeCount  atomic.Int64 // 累计恢复次数
	autoPause    atomic.Int64 // 因表结构变化自动暂停的次数

	// 延迟统计（毫秒）：记录「采集到位点 → 上传完成」的时间差
	delayMu   sync.Mutex
	delaySum  int64
	delayMax  int64
	delayCnt  int64

	// 启动时间与缓冲积压（由外部在 scanFile/ChangeTables 时更新）
	startedAt     time.Time
	backlogMu     sync.RWMutex
	pendingFiles  int64 // 当前积压待上传的文件数
	pendingBytes  int64 // 当前积压待上传的字节数
}

// NewMonitor 创建一个监控器，记录启动时间。
func NewMonitor() *Monitor {
	return &Monitor{startedAt: time.Now()}
}

// recordDelay 记录一次端到端延迟（毫秒）。延迟为负（如时钟回拨或位点异常）时忽略。
func (m *Monitor) recordDelay(ms int64) {
	if ms < 0 {
		return
	}
	m.delayMu.Lock()
	m.delaySum += ms
	m.delayCnt++
	if ms > m.delayMax {
		m.delayMax = ms
	}
	m.delayMu.Unlock()
}

// setBacklog 更新当前缓冲积压（待上传文件数与字节数）。
func (m *Monitor) setBacklog(files, bytes int64) {
	m.backlogMu.Lock()
	m.pendingFiles = files
	m.pendingBytes = bytes
	m.backlogMu.Unlock()
}

// Snapshot 返回监控器的不可变快照。
type Snapshot struct {
	UptimeSeconds int64 `json:"uptimeSeconds"` // 运行时长（秒）

	CaptureRows   int64 `json:"captureRows"`   // 累计采集行数
	CaptureBytes  int64 `json:"captureBytes"`  // 累计采集字节数
	CaptureErrors int64 `json:"captureErrors"` // 采集失败次数

	UploadRows    int64 `json:"uploadRows"`    // 累计上传行数
	UploadBytes   int64 `json:"uploadBytes"`   // 累计上传字节数
	UploadFiles   int64 `json:"uploadFiles"`   // 累计上传文件数
	UploadErrors  int64 `json:"uploadErrors"`  // 上传失败次数
	UploadAvgMs   int64 `json:"uploadAvgMs"`   // 上传平均耗时（毫秒）
	UploadMaxMs   int64 `json:"uploadMaxMs"`   // 上传最大耗时（毫秒）

	DDLCount    int64 `json:"ddlCount"`    // 累计 DDL 次数
	DDLErrors   int64 `json:"ddlErrors"`   // DDL 失败次数
	CreateTable int64 `json:"createTable"` // 建表次数
	AlterTable  int64 `json:"alterTable"`  // 改表次数
	DropTable   int64 `json:"dropTable"`   // 删表次数

	PauseCount  int64 `json:"pauseCount"`  // 累计暂停次数
	ResumeCount int64 `json:"resumeCount"` // 累计恢复次数
	AutoPause   int64 `json:"autoPause"`   // 自动暂停次数

	DelayAvgMs    int64 `json:"delayAvgMs"`    // 平均端到端延迟（毫秒）
	DelayMaxMs    int64 `json:"delayMaxMs"`    // 最大端到端延迟（毫秒）
	DelaySamples  int64 `json:"delaySamples"`  // 延迟采样数

	PendingFiles int64 `json:"pendingFiles"` // 当前积压待上传文件数
	PendingBytes int64 `json:"pendingBytes"` // 当前积压待上传字节数
}

// Snapshot 生成当前监控快照。
func (m *Monitor) Snapshot() Snapshot {
	uploadTotal := m.UploadTotalMs.Load()
	uploadCount := m.UploadFiles.Load()

	m.delayMu.Lock()
	delaySum, delayCnt, delayMax := m.delaySum, m.delayCnt, m.delayMax
	m.delayMu.Unlock()

	m.backlogMu.RLock()
	pendingFiles, pendingBytes := m.pendingFiles, m.pendingBytes
	m.backlogMu.RUnlock()

	s := Snapshot{
		UptimeSeconds: int64(time.Since(m.startedAt).Seconds()),
		CaptureRows:   m.CaptureRows.Load(),
		CaptureBytes:  m.CaptureBytes.Load(),
		CaptureErrors: m.CaptureErrors.Load(),
		UploadRows:    m.UploadRows.Load(),
		UploadBytes:   m.UploadBytes.Load(),
		UploadFiles:   uploadCount,
		UploadErrors:  m.UploadErrors.Load(),
		UploadMaxMs:   m.uploadMaxMs.Load(),
		DDLCount:      m.DDLCount.Load(),
		DDLErrors:     m.DDLErrors.Load(),
		CreateTable:   m.CreateTable.Load(),
		AlterTable:    m.AlterTable.Load(),
		DropTable:     m.DropTable.Load(),
		PauseCount:    m.PauseCount.Load(),
		ResumeCount:   m.ResumeCount.Load(),
		AutoPause:     m.autoPause.Load(),
		DelayMaxMs:    delayMax,
		DelaySamples:  delayCnt,
		PendingFiles:  pendingFiles,
		PendingBytes:  pendingBytes,
	}
	if uploadCount > 0 {
		s.UploadAvgMs = uploadTotal / uploadCount
	}
	if delayCnt > 0 {
		s.DelayAvgMs = delaySum / delayCnt
	}
	return s
}
