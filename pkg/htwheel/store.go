package htwheel

import "time"

// Recoverable 是持久化任务参数 P 需要实现的能力接口：
// 存储层只保存裸的 *P，进程重启后要恢复任务，必须能从 P 自身
// 重建出「到期时间」与「执行回调」。因此任何想要跨进程持久化并
// 恢复执行的 P 都应实现该接口。
//
//   - ID / Expire：用于去重与重新入轮，恢复后无需外部再传 id/时间。
//   - Recover：根据参数重建执行回调；返回 nil 表示该参数不可执行，
//     加载时会被跳过（例如参数已失效、业务已不可重放）。
type Recoverable interface {
	// ID 返回任务唯一标识。
	ID() uint64
	// Expire 返回计划执行时间。
	Expire() time.Time
	// Recover 返回重建后的执行回调，签名与 Schedule 的 fn 一致。
	Recover() func(uint64, time.Time)
}

// Store 是任务持久化抽象，用于在进程重启后恢复定时任务。
//
// 约定：
//   - 实现方保存的是参数 P 及其 id/到期时间（由 P 自身实现 Recoverable 提供）。
//   - Load 返回 before 之前到期、且按到期时间升序的前 limit 条，
//     供调度器轮询拉取并重新入轮；返回空切片表示当前没有待加载任务。
//   - 设置了存储层且任务要求持久化（经 ScheduleWithParam 提交、或从 Load 恢复）
//     时，所有任务都会写存储；其中「远任务」（到期时间 - 现在 > ahead 扫描窗口）
//     只写存储、不入内存时间轮，待临近到期时由 Load 拉回入轮，以减少常驻内存。
//   - Add / Remove / Update 由调度器自动调用：
//     · 入轮/覆盖 → Add（新 id）或 Update（已存在 id）；
//     · Cancel 命中（含已下沉到存储的远任务）→ Remove；
//     · 任务执行成功或重试用尽 → Remove。
//
// 注意：实现方的所有方法必须线程安全——Add/Remove/Update 由调度器的
// 驱动协程串行调用，Load 由同一驱动协程的 loadTasks 周期调用，因此整体串行。
type Store[P Recoverable] interface {
	// Add 添加
	Add(id uint64, t time.Time, task P) error
	// Remove 移除
	Remove(id uint64) bool
	// Update 更新
	Update(id uint64, t time.Time, task P) bool
	// Get 获取
	Get(id uint64) (P, time.Time, bool)
	// Load 加载
	Load(before time.Time, limit int) ([]P, error)
}
