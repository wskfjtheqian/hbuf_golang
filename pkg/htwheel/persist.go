package htwheel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

/*
========================
   错误
========================
*/

var (
	// ErrClosed 表示调度器已停止。
	ErrClosed = errors.New("htwheel: scheduler closed")
	// ErrNotFound 表示任务不存在。
	ErrNotFound = errors.New("htwheel: task not found")
	// ErrRetriesExhausted 表示任务执行失败且重试次数已用尽，记录已被删除。
	ErrRetriesExhausted = errors.New("htwheel: retries exhausted")
)

/*
========================
   Record / Store
========================
*/

// Record 是任务的可持久化描述。
//
// 回调函数无法序列化，因此用 Name 标识处理器、Payload 承载业务参数；
// 恢复任务时通过 Register 注册的处理器按 Name 还原执行逻辑。
//
// 用户可用自定义结构体实现该接口，以接入任意存储后端；调度器只通过
// 方法访问字段，不关心底层具体类型。内置实现见 TaskRecord。
type Record interface {
	GetID() uint64
	GetExpire() time.Time
	GetName() string
	GetRetries() int32
	GetRetryDelay() time.Duration

	SetID(uint64)
	SetExpire(time.Time)
	SetName(string)
	SetRetries(int32)
	SetRetryDelay(time.Duration)
}

// Store 是存储层接口，可对接数据库、文件或任意后端。
//
// 实现须保证方法并发安全：PersistentScheduler 的轮询协程与业务协程会同时访问。
// 所有方法都应尊重 ctx 的取消。
type Store[R Record] interface {
	// Put 新增或覆盖一条记录（按 ID 去重）。
	Put(ctx context.Context, rec R) error
	// Get 读取一条记录。
	Get(ctx context.Context, id uint64) (R, bool, error)
	// Delete 删除一条记录；记录不存在不应视为错误。
	Delete(ctx context.Context, id uint64) error
	// Scan 返回 Expire <= before 的记录，按 Expire 升序排列；limit>0 时最多返回 limit 条。
	Scan(ctx context.Context, before time.Time, limit int) ([]R, error)
}

// Handler 是任务的执行逻辑。exec 为计划执行时间，t 为任务记录本身（含 Payload 等字段）。
//
// 返回非 nil 表示本次执行失败：调度器会按记录里的重试策略改期重试。
// Handler 内 panic 也被视为失败（会被捕获并转为 error）。
type Handler[T Record] func(id uint64, exec time.Time, t T) error

/*
========================
   Options
========================
*/

const (
	defaultPollInterval = time.Second
	defaultLookahead    = 30 * time.Second
	defaultBatchSize    = 256
	// minRetryDelay 是重试间隔下限，避免重试风暴打满 CPU。
	minRetryDelay = time.Millisecond
)

type persistConfig struct {
	poll    time.Duration
	ahead   time.Duration
	batch   int
	onErr   func(error)
	retries int           // 新任务默认重试次数
	delay   time.Duration // 新任务默认重试间隔
	backoff float64       // 重试间隔增长倍数
	maxWait time.Duration // 重试间隔上限
}

func defaultPersistConfig() persistConfig {
	return persistConfig{
		poll:    defaultPollInterval,
		ahead:   defaultLookahead,
		batch:   defaultBatchSize,
		backoff: 1,
	}
}

// PersistOption 用于定制 PersistentScheduler。
type PersistOption func(*persistConfig)

// WithPollInterval 设置扫描存储层的间隔（默认 1s）。
func WithPollInterval(d time.Duration) PersistOption {
	return func(c *persistConfig) {
		if d > 0 {
			c.poll = d
		}
	}
}

// WithLookahead 设置扫描窗口：把 Expire 在 now+lookahead 之前的任务提前读入内存（默认 30s）。
func WithLookahead(d time.Duration) PersistOption {
	return func(c *persistConfig) {
		if d > 0 {
			c.ahead = d
		}
	}
}

// WithBatchSize 设置单次扫描的最大条数（默认 256）。
func WithBatchSize(n int) PersistOption {
	return func(c *persistConfig) {
		if n > 0 {
			c.batch = n
		}
	}
}

// WithErrorHandler 设置存储层/处理器错误的回调（默认忽略）。
//
// 每次失败尝试与被放弃的任务都会通过它上报，可用 errors.Is(err, ErrRetriesExhausted)
// 区分「这次失败还会重试」与「重试已用完、任务被丢弃」。
func WithErrorHandler(fn func(error)) PersistOption {
	return func(c *persistConfig) {
		c.onErr = fn
	}
}

// WithDefaultRetry 设置新建任务的默认重试策略：失败后最多重试 retries 次，
// 每次间隔 delay（默认 0，即不重试）。单条任务可用 WithTaskRetry 覆盖。
//
// 仅影响之后 Schedule 的任务；直接写入存储层的记录以 Record 自身字段为准。
func WithDefaultRetry(retries int, delay time.Duration) PersistOption {
	return func(c *persistConfig) {
		if retries < 0 {
			retries = 0
		}
		c.retries = retries
		if delay > 0 {
			c.delay = delay
		}
	}
}

// WithRetryBackoff 设置重试间隔的增长倍数（默认 1，即固定间隔）。
//
// 例如 2 表示间隔依次为 delay、2*delay、4*delay……便于给下游故障留出恢复时间。
func WithRetryBackoff(factor float64) PersistOption {
	return func(c *persistConfig) {
		if factor >= 1 {
			c.backoff = factor
		}
	}
}

// WithMaxRetryDelay 设置退避后的间隔上限（0 表示不限制）。
func WithMaxRetryDelay(d time.Duration) PersistOption {
	return func(c *persistConfig) {
		if d > 0 {
			c.maxWait = d
		}
	}
}

// TaskOption 用于定制单条任务。
type TaskOption[R Record] func(R)

// WithTaskRetry 覆盖该任务的重试策略：失败后最多重试 retries 次，首次间隔 delay。
func WithTaskRetry[R Record](retries int32, delay time.Duration) TaskOption[R] {
	return func(r R) {
		if retries < 0 {
			retries = 0
		}
		r.SetRetries(retries)
		if delay > 0 {
			r.SetRetryDelay(delay)
		}
	}
}

// SetName 修改任务的处理器名（配合 UpdateWith 使用，可把任务换成另一个已注册的处理器）。
func SetName[R Record](name string) TaskOption[R] {
	return func(r R) {
		r.SetName(name)
	}
}

// SetRetries 修改任务的剩余重试次数（配合 UpdateWith 使用，可在不改变间隔的前提下增减次数）。
func SetRetries[R Record](n int32) TaskOption[R] {
	return func(r R) {
		if n < 0 {
			n = 0
		}
		r.SetRetries(n)
	}
}

// SetRetryDelay 修改任务的重试间隔（配合 UpdateWith 使用）。
func SetRetryDelay[R Record](d time.Duration) TaskOption[R] {
	return func(r R) {
		if d > 0 {
			r.SetRetryDelay(d)
		}
	}
}

// cloneBytes 深拷贝字节切片，避免调用方后续改动影响已持久化的记录。
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

/*
========================
   PersistentScheduler
========================
*/

// PersistentScheduler 在 Scheduler 之上增加持久化与重试：
//
//   - Schedule/Cancel/Update 同时写存储层；
//   - 后台按 PollInterval 扫描 Expire <= now+Lookahead 的记录，把尚未加载的任务预热进内存时间轮；
//   - 任务触发时按 Name 找到注册的 Handler 执行，成功后从存储层删除；
//   - Handler 返回 error（或 panic）时，按 Record 里的重试策略扣减 Retries 并改期到
//     now+RetryDelay 后重新入轮；Retries 用尽则删除记录并上报 ErrRetriesExhausted。
//
// 存储层是事实来源（source of truth）：Get/TTL/Update 直接读存储层，
// 因此尚未预热进内存的远期任务同样可以被查询和修改。
//
// 语义为「至少一次」：若进程在执行完 Handler、删除记录前崩溃，重启后会再次执行。
type PersistentScheduler[R Record] struct {
	s     *Scheduler
	store Store[R]

	baseCtx context.Context // 后台扫描/异步执行等无上层请求场景的根 ctx

	poll    time.Duration
	ahead   time.Duration
	batch   int
	onErr   func(error)
	retries int
	delay   time.Duration
	backoff float64
	maxWait time.Duration

	mu       sync.RWMutex
	handlers map[string]Handler[R]
	loaded   map[uint64]int64 // id -> 已预热到内存的 expire（UnixNano）
	running  map[uint64]bool  // 正在执行中的任务，防止轮询重复预热

	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
	closed   atomic.Bool
}

// NewPersistentScheduler 创建持久化调度器并启动后台扫描。
//
//   - s：为 nil 时使用 NewScheduler() 的默认配置。
//   - baseCtx：后台扫描与异步执行所使用的基础上下文（可为 nil，nil 时用 context.Background()）。
//     所有存储访问都由此 ctx 派生，取消它将停止后台扫描与后续任务的存储操作。
func NewPersistentScheduler[R Record](store Store[R], s *Scheduler, baseCtx context.Context, opts ...PersistOption) *PersistentScheduler[R] {
	if store == nil {
		panic("htwheel: nil store")
	}
	if s == nil {
		s = NewScheduler()
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	cfg := defaultPersistConfig()
	for _, o := range opts {
		o(&cfg)
	}

	p := &PersistentScheduler[R]{
		s:        s,
		store:    store,
		baseCtx:  baseCtx,
		poll:     cfg.poll,
		ahead:    cfg.ahead,
		batch:    cfg.batch,
		onErr:    cfg.onErr,
		retries:  cfg.retries,
		delay:    cfg.delay,
		backoff:  cfg.backoff,
		maxWait:  cfg.maxWait,
		handlers: make(map[string]Handler[R]),
		loaded:   make(map[uint64]int64),
		running:  make(map[uint64]bool),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go p.run()
	return p
}

// Scheduler 返回底层的内存调度器，便于使用 Get/TTL 等内存能力。
func (p *PersistentScheduler[R]) Scheduler() *Scheduler { return p.s }

// Register 注册处理器。同名重复注册会覆盖。应在任务到期前完成注册。
func (p *PersistentScheduler[R]) Register(name string, h Handler[R]) {
	p.mu.Lock()
	p.handlers[name] = h
	p.mu.Unlock()
}

// Schedule 按 ID 新增或整体更新一条任务：可同时设置执行时间、处理器名、业务参数与重试策略。
//
//   - ID 已存在：整体替换为 rec 的内容（语义等同「改」），并把内存里的旧任务同 ID 覆盖；
//   - ID 不存在：新增一条（语义等同「增」）。
//
// Schedule 直接使用给定的 Record（包含 Name/Payload/Retries/RetryDelay），不套用全局默认重试策略；
// 因此需要调用方自行填充完整的记录字段。传入 nil 的 Payload 表示清空业务参数。
func (p *PersistentScheduler[R]) Schedule(ctx context.Context, rec R) error {
	if p.closed.Load() {
		return ErrClosed
	}
	if rec.GetName() == "" {
		return fmt.Errorf("htwheel: record name is empty")
	}
	if err := p.store.Put(ctx, rec); err != nil {
		return err
	}
	p.load(rec)
	return nil
}

// Update 修改任务执行时间并保持其余字段（Name/Payload/重试策略）不变。
func (p *PersistentScheduler[R]) Update(ctx context.Context, id uint64, at time.Time) error {
	return p.UpdateWith(ctx, id, at)
}

// UpdateWith 修改任务，可改执行时间、处理器名、业务参数与重试策略。
//
// 传入的 TaskOption（如 WithTaskRetry）会应用到记录上；用 SetName/SetPayload
// 可同时替换处理器名与业务参数。不传选项时仅改期，其余字段保持不变。
func (p *PersistentScheduler[R]) UpdateWith(ctx context.Context, id uint64, at time.Time, opts ...TaskOption[R]) error {
	if p.closed.Load() {
		return ErrClosed
	}
	rec, ok, err := p.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	rec.SetExpire(at)
	for _, o := range opts {
		o(rec)
	}
	if err := p.store.Put(ctx, rec); err != nil {
		return err
	}
	// 内存中的旧任务会被同 ID 覆盖；即使已经提交执行，
	// fire 会比对存储层的 Expire 而跳过。
	p.load(rec)
	return nil
}

// Get 从存储层读取任务。
func (p *PersistentScheduler[R]) Get(ctx context.Context, id uint64) (R, bool, error) {
	return p.store.Get(ctx, id)
}

// TTL 返回距离下一次执行的剩余时间；任务不存在或已到期返回 false。
// 处于重试等待期的任务，返回的是距下次重试的时间。
func (p *PersistentScheduler[R]) TTL(ctx context.Context, id uint64) (time.Duration, bool, error) {
	rec, ok, err := p.store.Get(ctx, id)
	if err != nil || !ok {
		return 0, false, err
	}
	ttl := time.Until(rec.GetExpire())
	if ttl <= 0 {
		return 0, false, nil
	}
	return ttl, true, nil
}

// Cancel 取消任务：先从存储层删除，再撤销内存中的调度。
// 处于重试等待期的任务同样可以取消。
func (p *PersistentScheduler[R]) Cancel(ctx context.Context, id uint64) error {
	return p.remove(ctx, id)
}

// Remove 删除任务（Cancel 的别名）。返回 nil 表示删除成功（无论任务是否存在）。
func (p *PersistentScheduler[R]) Remove(ctx context.Context, id uint64) error {
	return p.remove(ctx, id)
}

func (p *PersistentScheduler[R]) remove(ctx context.Context, id uint64) error {
	if p.closed.Load() {
		return ErrClosed
	}
	if err := p.store.Delete(ctx, id); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.loaded, id)
	p.mu.Unlock()
	p.s.Cancel(id)
	return nil
}

// Stop 停止后台扫描与内存调度，等待已触发任务执行完毕。可重复调用。
func (p *PersistentScheduler[R]) Stop() {
	p.stopOnce.Do(func() {
		p.closed.Store(true)
		close(p.stopCh)
		<-p.doneCh
		p.s.Stop()
	})
}

/*
========================
   内部实现
========================
*/

func (p *PersistentScheduler[R]) run() {
	ticker := time.NewTicker(p.poll)
	defer ticker.Stop()
	defer close(p.doneCh)

	p.pollOnce()
	for {
		select {
		case <-ticker.C:
			p.pollOnce()
		case <-p.stopCh:
			return
		}
	}
}

// pollOnce 把即将到期（或已到期）且尚未预热的任务读入内存。
func (p *PersistentScheduler[R]) pollOnce() {
	if p.closed.Load() {
		return
	}
	recs, err := p.store.Scan(p.baseCtx, time.Now().Add(p.ahead), p.batch)
	if err != nil {
		p.report(err)
		return
	}
	for _, rec := range recs {
		p.mu.RLock()
		exp, ok := p.loaded[rec.GetID()]
		p.mu.RUnlock()
		if ok && exp == rec.GetExpire().UnixNano() {
			continue
		}
		p.load(rec)
	}
}

// load 把一条记录预热进内存时间轮（同 ID 覆盖）。
func (p *PersistentScheduler[R]) load(rec R) {
	if p.closed.Load() {
		return
	}

	id := rec.GetID()
	expire := rec.GetExpire()

	p.mu.Lock()
	p.loaded[id] = expire.UnixNano()
	p.mu.Unlock()

	ok := p.s.Schedule(id, expire, func(uint64, time.Time) {
		p.fire(rec)
	})
	if !ok {
		p.mu.Lock()
		delete(p.loaded, id)
		p.mu.Unlock()
	}
}

// fire 在校验存储层状态后执行处理器：成功则删除记录，失败则按重试策略改期。
func (p *PersistentScheduler[R]) fire(rec R) {
	id := rec.GetID()
	expire := rec.GetExpire()

	// 标记运行中：handler 执行期间轮询不会重复预热同一任务。
	p.mu.Lock()
	if p.running[id] {
		p.mu.Unlock()
		return
	}
	p.running[id] = true
	p.mu.Unlock()

	var retry R
	hasRetry := false
	defer func() {
		p.mu.Lock()
		delete(p.running, id)
		delete(p.loaded, id)
		p.mu.Unlock()
		// 重排必须放在清理之后：load 会重新写入 loaded。
		if hasRetry {
			p.load(retry)
		}
	}()

	cur, ok, err := p.store.Get(p.baseCtx, id)
	if err != nil {
		p.report(err)
		return
	}
	// 已被取消：直接跳过。
	// 已被改期：跳过，由下一次轮询按新时间重新预热。
	if !ok || !cur.GetExpire().Equal(expire) {
		return
	}

	p.mu.RLock()
	h := p.handlers[cur.GetName()]
	p.mu.RUnlock()
	if h == nil {
		// 处理器尚未注册：保留记录，等待注册后的轮询重试（不消耗重试次数）。
		p.report(fmt.Errorf("htwheel: handler %q not registered (task %d)", cur.GetName(), cur.GetID()))
		return
	}

	if cause := invoke(h, cur); cause != nil {
		if next, ok := p.reschedule(cur, cause); ok {
			retry = next
			hasRetry = true
		}
		return
	}

	if err := p.store.Delete(p.baseCtx, cur.GetID()); err != nil {
		p.report(err)
	}
}

// reschedule 处理一次执行失败：扣减重试次数并把记录改期到 now+间隔。
// 返回 false 表示不再重试（记录已删除或已被外部改动）。
func (p *PersistentScheduler[R]) reschedule(cur R, cause error) (R, bool) {
	// 执行期间可能被取消或改期，此时不覆盖外部改动。
	latest, ok, err := p.store.Get(p.baseCtx, cur.GetID())
	if err != nil {
		p.report(err)
		return latest, false
	}
	if !ok || !latest.GetExpire().Equal(cur.GetExpire()) {
		return latest, false
	}

	if latest.GetRetries() <= 0 {
		p.report(fmt.Errorf("htwheel: task %d (%s) failed: %w: %v", latest.GetID(), latest.GetName(), ErrRetriesExhausted, cause))
		if err := p.store.Delete(p.baseCtx, latest.GetID()); err != nil {
			p.report(err)
		}
		return latest, false
	}

	delay := latest.GetRetryDelay()
	if delay <= 0 {
		delay = p.delay
	}
	if delay <= 0 {
		delay = minRetryDelay
	}
	if p.maxWait > 0 && delay > p.maxWait {
		delay = p.maxWait
	}

	latest.SetRetries(latest.GetRetries() - 1)
	latest.SetExpire(time.Now().Add(delay))
	latest.SetRetryDelay(p.nextDelay(delay))

	p.report(fmt.Errorf("htwheel: task %d (%s) failed, retry in %s (%d left): %v",
		latest.GetID(), latest.GetName(), delay, latest.GetRetries(), cause))

	if err := p.store.Put(p.baseCtx, latest); err != nil {
		p.report(err)
		return latest, false
	}
	return latest, true
}

// nextDelay 计算下一次重试的间隔；backoff <= 1 时为固定间隔。
func (p *PersistentScheduler[R]) nextDelay(cur time.Duration) time.Duration {
	if p.backoff <= 1 {
		return cur
	}
	f := float64(cur) * p.backoff
	if math.IsInf(f, 0) || f >= float64(math.MaxInt64) {
		// 溢出：停在当前间隔，避免退化成负数导致忙循环。
		return cur
	}
	next := time.Duration(f)
	if next <= cur {
		next = cur + time.Millisecond
	}
	if p.maxWait > 0 && next > p.maxWait {
		next = p.maxWait
	}
	return next
}

// invoke 执行处理器并把 panic 转成 error，使 panic 也能走重试流程。
func invoke[R Record](h Handler[R], rec R) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h(rec.GetID(), rec.GetExpire(), rec)
}

func (p *PersistentScheduler[R]) report(err error) {
	if err != nil && p.onErr != nil {
		p.onErr(err)
	}
}
