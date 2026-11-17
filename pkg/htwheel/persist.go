package htwheel

import (
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
type Record struct {
	ID      uint64    `json:"id"`
	Expire  time.Time `json:"expire"`
	Name    string    `json:"name"`
	Payload []byte    `json:"payload,omitempty"`

	// Retries 是剩余重试次数：每次执行失败后减 1，减到 0 后不再重试。
	// 为 0 表示失败即放弃。
	Retries int `json:"retries,omitempty"`
	// RetryDelay 是「本次失败后等待多久再重试」。单位为纳秒（JSON 中为整数）。
	// 开启退避时，每次重试后该字段会按倍数增长并回写到记录中。
	RetryDelay time.Duration `json:"retry_delay,omitempty"`
}

// Store 是存储层接口，可对接数据库、文件或任意后端。
//
// 实现须保证方法并发安全：PersistentScheduler 的轮询协程与业务协程会同时访问。
type Store interface {
	// Put 新增或覆盖一条记录（按 ID 去重）。
	Put(rec Record) error
	// Get 读取一条记录。
	Get(id uint64) (Record, bool, error)
	// Delete 删除一条记录；记录不存在不应视为错误。
	Delete(id uint64) error
	// Scan 返回 Expire <= before 的记录，按 Expire 升序排列；limit>0 时最多返回 limit 条。
	Scan(before time.Time, limit int) ([]Record, error)
}

// Handler 是任务的执行逻辑。exec 为计划执行时间，payload 为持久化时写入的业务参数。
//
// 返回非 nil 表示本次执行失败：调度器会按记录里的重试策略改期重试。
// Handler 内 panic 也被视为失败（会被捕获并转为 error）。
type Handler func(id uint64, exec time.Time, payload []byte) error

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
type TaskOption func(*Record)

// WithTaskRetry 覆盖该任务的重试策略：失败后最多重试 retries 次，首次间隔 delay。
func WithTaskRetry(retries int, delay time.Duration) TaskOption {
	return func(r *Record) {
		if retries < 0 {
			retries = 0
		}
		r.Retries = retries
		if delay > 0 {
			r.RetryDelay = delay
		}
	}
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
type PersistentScheduler struct {
	s     *Scheduler
	store Store

	poll    time.Duration
	ahead   time.Duration
	batch   int
	onErr   func(error)
	retries int
	delay   time.Duration
	backoff float64
	maxWait time.Duration

	mu       sync.RWMutex
	handlers map[string]Handler
	loaded   map[uint64]int64 // id -> 已预热到内存的 expire（UnixNano）
	running  map[uint64]bool  // 正在执行中的任务，防止轮询重复预热

	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
	closed   atomic.Bool
}

// NewPersistentScheduler 创建持久化调度器并启动后台扫描。
// s 为 nil 时使用 NewScheduler() 的默认配置。
func NewPersistentScheduler(store Store, s *Scheduler, opts ...PersistOption) *PersistentScheduler {
	if store == nil {
		panic("htwheel: nil store")
	}
	if s == nil {
		s = NewScheduler()
	}

	cfg := defaultPersistConfig()
	for _, o := range opts {
		o(&cfg)
	}

	p := &PersistentScheduler{
		s:        s,
		store:    store,
		poll:     cfg.poll,
		ahead:    cfg.ahead,
		batch:    cfg.batch,
		onErr:    cfg.onErr,
		retries:  cfg.retries,
		delay:    cfg.delay,
		backoff:  cfg.backoff,
		maxWait:  cfg.maxWait,
		handlers: make(map[string]Handler),
		loaded:   make(map[uint64]int64),
		running:  make(map[uint64]bool),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go p.run()
	return p
}

// Scheduler 返回底层的内存调度器，便于使用 Get/TTL 等内存能力。
func (p *PersistentScheduler) Scheduler() *Scheduler { return p.s }

// Register 注册处理器。同名重复注册会覆盖。应在任务到期前完成注册。
func (p *PersistentScheduler) Register(name string, h Handler) {
	p.mu.Lock()
	p.handlers[name] = h
	p.mu.Unlock()
}

// Schedule 持久化并在 at 时刻执行名为 name 的处理器（同 ID 覆盖），使用默认重试策略。
func (p *PersistentScheduler) Schedule(id uint64, at time.Time, name string, payload []byte) error {
	return p.ScheduleWith(id, at, name, payload)
}

// ScheduleWith 与 Schedule 相同，但可以覆盖重试策略，例如：
//
//	p.ScheduleWith(1001, at, "send_email", payload, htwheel.WithTaskRetry(3, 5*time.Second))
func (p *PersistentScheduler) ScheduleWith(id uint64, at time.Time, name string, payload []byte, opts ...TaskOption) error {
	if p.closed.Load() {
		return ErrClosed
	}
	rec := Record{
		ID:         id,
		Expire:     at,
		Name:       name,
		Payload:    payload,
		Retries:    p.retries,
		RetryDelay: p.delay,
	}
	for _, o := range opts {
		o(&rec)
	}
	if err := p.store.Put(cloneRecord(rec)); err != nil {
		return err
	}
	p.load(rec)
	return nil
}

// Update 修改任务执行时间并保持其余字段不变。
func (p *PersistentScheduler) Update(id uint64, at time.Time) error {
	return p.UpdateWith(id, at)
}

// UpdateWith 修改任务执行时间，并可选地重置重试策略（如失败后想再给几次机会）。
func (p *PersistentScheduler) UpdateWith(id uint64, at time.Time, opts ...TaskOption) error {
	if p.closed.Load() {
		return ErrClosed
	}
	rec, ok, err := p.store.Get(id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	rec.Expire = at
	for _, o := range opts {
		o(&rec)
	}
	if err := p.store.Put(rec); err != nil {
		return err
	}
	// 内存中的旧任务会被同 ID 覆盖；即使已经提交执行，
	// fire 会比对存储层的 Expire 而跳过。
	p.load(rec)
	return nil
}

// Get 从存储层读取任务。
func (p *PersistentScheduler) Get(id uint64) (Record, bool, error) {
	return p.store.Get(id)
}

// TTL 返回距离下一次执行的剩余时间；任务不存在或已到期返回 false。
// 处于重试等待期的任务，返回的是距下次重试的时间。
func (p *PersistentScheduler) TTL(id uint64) (time.Duration, bool, error) {
	rec, ok, err := p.store.Get(id)
	if err != nil || !ok {
		return 0, false, err
	}
	ttl := time.Until(rec.Expire)
	if ttl <= 0 {
		return 0, false, nil
	}
	return ttl, true, nil
}

// Cancel 取消任务：先从存储层删除，再撤销内存中的调度。
// 处于重试等待期的任务同样可以取消。
func (p *PersistentScheduler) Cancel(id uint64) error {
	if p.closed.Load() {
		return ErrClosed
	}
	if err := p.store.Delete(id); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.loaded, id)
	p.mu.Unlock()
	p.s.Cancel(id)
	return nil
}

// Stop 停止后台扫描与内存调度，等待已触发任务执行完毕。可重复调用。
func (p *PersistentScheduler) Stop() {
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

func (p *PersistentScheduler) run() {
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
func (p *PersistentScheduler) pollOnce() {
	if p.closed.Load() {
		return
	}
	recs, err := p.store.Scan(time.Now().Add(p.ahead), p.batch)
	if err != nil {
		p.report(err)
		return
	}
	for _, rec := range recs {
		rec = cloneRecord(rec)
		p.mu.RLock()
		exp, ok := p.loaded[rec.ID]
		p.mu.RUnlock()
		if ok && exp == rec.Expire.UnixNano() {
			continue
		}
		p.load(rec)
	}
}

// load 把一条记录预热进内存时间轮（同 ID 覆盖）。
func (p *PersistentScheduler) load(rec Record) {
	if p.closed.Load() {
		return
	}
	r := cloneRecord(rec)

	p.mu.Lock()
	p.loaded[r.ID] = r.Expire.UnixNano()
	p.mu.Unlock()

	ok := p.s.Schedule(r.ID, r.Expire, func(uint64, time.Time) {
		p.fire(r)
	})
	if !ok {
		p.mu.Lock()
		delete(p.loaded, r.ID)
		p.mu.Unlock()
	}
}

// fire 在校验存储层状态后执行处理器：成功则删除记录，失败则按重试策略改期。
func (p *PersistentScheduler) fire(rec Record) {
	// 标记运行中：handler 执行期间轮询不会重复预热同一任务。
	p.mu.Lock()
	if p.running[rec.ID] {
		p.mu.Unlock()
		return
	}
	p.running[rec.ID] = true
	p.mu.Unlock()

	var retry *Record
	defer func() {
		p.mu.Lock()
		delete(p.running, rec.ID)
		delete(p.loaded, rec.ID)
		p.mu.Unlock()
		// 重排必须放在清理之后：load 会重新写入 loaded。
		if retry != nil {
			p.load(*retry)
		}
	}()

	cur, ok, err := p.store.Get(rec.ID)
	if err != nil {
		p.report(err)
		return
	}
	// 已被取消：直接跳过。
	// 已被改期：跳过，由下一次轮询按新时间重新预热。
	if !ok || !cur.Expire.Equal(rec.Expire) {
		return
	}

	p.mu.RLock()
	h := p.handlers[cur.Name]
	p.mu.RUnlock()
	if h == nil {
		// 处理器尚未注册：保留记录，等待注册后的轮询重试（不消耗重试次数）。
		p.report(fmt.Errorf("htwheel: handler %q not registered (task %d)", cur.Name, cur.ID))
		return
	}

	if cause := invoke(h, cur); cause != nil {
		if next, ok := p.reschedule(cur, cause); ok {
			retry = &next
		}
		return
	}

	if err := p.store.Delete(cur.ID); err != nil {
		p.report(err)
	}
}

// reschedule 处理一次执行失败：扣减重试次数并把记录改期到 now+间隔。
// 返回 false 表示不再重试（记录已删除或已被外部改动）。
func (p *PersistentScheduler) reschedule(cur Record, cause error) (Record, bool) {
	// 执行期间可能被取消或改期，此时不覆盖外部改动。
	latest, ok, err := p.store.Get(cur.ID)
	if err != nil {
		p.report(err)
		return Record{}, false
	}
	if !ok || !latest.Expire.Equal(cur.Expire) {
		return Record{}, false
	}

	if latest.Retries <= 0 {
		p.report(fmt.Errorf("htwheel: task %d (%s) failed: %w: %v", latest.ID, latest.Name, ErrRetriesExhausted, cause))
		if err := p.store.Delete(latest.ID); err != nil {
			p.report(err)
		}
		return Record{}, false
	}

	delay := latest.RetryDelay
	if delay <= 0 {
		delay = p.delay
	}
	if delay <= 0 {
		delay = minRetryDelay
	}
	if p.maxWait > 0 && delay > p.maxWait {
		delay = p.maxWait
	}

	latest.Retries--
	latest.Expire = time.Now().Add(delay)
	latest.RetryDelay = p.nextDelay(delay)

	p.report(fmt.Errorf("htwheel: task %d (%s) failed, retry in %s (%d left): %v",
		latest.ID, latest.Name, delay, latest.Retries, cause))

	if err := p.store.Put(cloneRecord(latest)); err != nil {
		p.report(err)
		return Record{}, false
	}
	return latest, true
}

// nextDelay 计算下一次重试的间隔；backoff <= 1 时为固定间隔。
func (p *PersistentScheduler) nextDelay(cur time.Duration) time.Duration {
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
func invoke(h Handler, rec Record) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h(rec.ID, rec.Expire, rec.Payload)
}

func (p *PersistentScheduler) report(err error) {
	if err != nil && p.onErr != nil {
		p.onErr(err)
	}
}

func cloneRecord(r Record) Record {
	if r.Payload != nil {
		r.Payload = append([]byte(nil), r.Payload...)
	}
	return r
}
