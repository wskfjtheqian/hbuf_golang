package htwheel

import (
	"errors"
	"fmt"
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
type Handler func(id uint64, exec time.Time, payload []byte)

/*
========================
   Options
========================
*/

const (
	defaultPollInterval = time.Second
	defaultLookahead    = 30 * time.Second
	defaultBatchSize    = 256
)

type persistConfig struct {
	poll  time.Duration
	ahead time.Duration
	batch int
	onErr func(error)
}

func defaultPersistConfig() persistConfig {
	return persistConfig{
		poll:  defaultPollInterval,
		ahead: defaultLookahead,
		batch: defaultBatchSize,
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
func WithErrorHandler(fn func(error)) PersistOption {
	return func(c *persistConfig) {
		c.onErr = fn
	}
}

/*
========================
   PersistentScheduler
========================
*/

// PersistentScheduler 在 Scheduler 之上增加持久化：
//
//   - Schedule/Cancel/Update 同时写存储层；
//   - 后台按 PollInterval 扫描 Expire <= now+Lookahead 的记录，把尚未加载的任务预热进内存时间轮；
//   - 任务触发时按 Name 找到注册的 Handler 执行，成功后从存储层删除。
//
// 存储层是事实来源（source of truth）：Get/TTL/Update 直接读存储层，
// 因此尚未预热进内存的远期任务同样可以被查询和修改。
//
// 语义为「至少一次」：若进程在执行完 Handler、删除记录前崩溃，重启后会再次执行。
type PersistentScheduler struct {
	s     *Scheduler
	store Store

	poll  time.Duration
	ahead time.Duration
	batch int
	onErr func(error)

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

// Schedule 持久化并在 at 时刻执行名为 name 的处理器（同 ID 覆盖）。
func (p *PersistentScheduler) Schedule(id uint64, at time.Time, name string, payload []byte) error {
	if p.closed.Load() {
		return ErrClosed
	}
	rec := Record{ID: id, Expire: at, Name: name, Payload: payload}
	if err := p.store.Put(cloneRecord(rec)); err != nil {
		return err
	}
	p.load(rec)
	return nil
}

// Update 修改任务执行时间并保持其余字段不变。
func (p *PersistentScheduler) Update(id uint64, at time.Time) error {
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

// TTL 返回剩余时间；任务不存在或已到期返回 false。
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

// fire 在校验存储层状态后执行处理器，并删除记录。
func (p *PersistentScheduler) fire(rec Record) {
	// 标记运行中：handler 执行期间轮询不会重复预热同一任务。
	p.mu.Lock()
	if p.running[rec.ID] {
		p.mu.Unlock()
		return
	}
	p.running[rec.ID] = true
	p.mu.Unlock()

	// 结束时清掉 loaded，使失败/panic 的任务能在下一轮被重新预热重试。
	defer func() {
		p.mu.Lock()
		delete(p.running, rec.ID)
		delete(p.loaded, rec.ID)
		p.mu.Unlock()
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
		// 处理器尚未注册：保留记录，等待注册后的轮询重试。
		p.report(fmt.Errorf("htwheel: handler %q not registered (task %d)", cur.Name, cur.ID))
		return
	}

	h(cur.ID, cur.Expire, cur.Payload)

	if err := p.store.Delete(cur.ID); err != nil {
		p.report(err)
	}
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
