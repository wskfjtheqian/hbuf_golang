package htwheel

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

/*
========================
   Task
========================
*/

// Task 是一条调度条目。除 canceled 外，字段在发布给调度器后不再被修改，
// 因此外部通过 Expire 读取是安全的。
type Task struct {
	id       uint64
	fn       func(uint64, time.Time)
	expire   int64 // 计划执行时间（UnixNano）
	canceled int32
}

// Expire 返回计划执行时间（纳秒）。
// 使用指针接收者，避免复制含 atomic 字段的结构体。
func (t *Task) Expire() int64 { return t.expire }

/*
========================
   WorkerPool
========================
*/

// WorkerPool 是固定大小的 goroutine 池。
//
// 并发约定：Submit 与 Stop 可以并发调用。Stop 之后 Submit 立即返回 false，
// 不会再出现「向已关闭 channel 发送」或 WaitGroup 计数泄漏。
type WorkerPool struct {
	mu     sync.Mutex
	ch     chan func()
	wg     sync.WaitGroup
	closed bool
	once   sync.Once
}

// NewWorkerPool 创建 size 个 worker 的任务池。
func NewWorkerPool(size int) *WorkerPool {
	if size <= 0 {
		size = 1
	}
	wp := &WorkerPool{ch: make(chan func(), 1024)}
	for i := 0; i < size; i++ {
		go wp.worker()
	}
	return wp
}

func (wp *WorkerPool) worker() {
	for fn := range wp.ch {
		wp.exec(fn)
	}
}

func (wp *WorkerPool) exec(fn func()) {
	defer wp.wg.Done()
	// 单个任务 panic 不应拖垮整个进程，也不能让 wg 计数泄漏。
	defer func() { _ = recover() }()
	fn()
}

// Submit 提交任务，返回 false 表示任务池已停止。
func (wp *WorkerPool) Submit(fn func()) bool {
	wp.mu.Lock()
	if wp.closed {
		wp.mu.Unlock()
		return false
	}
	// 在锁内 Add 并投递：与 Stop 串行化，避免 Add 与 Wait 并发。
	wp.wg.Add(1)
	wp.ch <- fn
	wp.mu.Unlock()
	return true
}

// Stop 停止接收新任务，等待已提交任务执行完毕后退出所有 worker。可重复调用。
func (wp *WorkerPool) Stop() {
	wp.once.Do(func() {
		wp.mu.Lock()
		wp.closed = true
		wp.mu.Unlock()

		// closed 置位后不会再有人 Add；此处仅等待存量任务跑完。
		wp.wg.Wait()
		close(wp.ch)
	})
}

/*
========================
   bucket / wheel
========================
*/

type bucket struct {
	tasks map[uint64]*Task
}

func newBucket() *bucket {
	return &bucket{tasks: make(map[uint64]*Task)}
}

// wheel 是单层时间轮。它不持有 goroutine、也不做任何加锁：
// 所有读写都只发生在 Scheduler 的驱动协程内，由结构保证串行。
type wheel struct {
	tick    time.Duration
	size    int
	cursor  int
	buckets []*bucket
}

func newWheel(tick time.Duration, size int) *wheel {
	// size 必须 >= 2，否则单层永远装不下任何任务（ticks 恒 >= 1）。
	if size < 2 {
		size = 2
	}
	w := &wheel{tick: tick, size: size}
	w.buckets = make([]*bucket, size)
	for i := range w.buckets {
		w.buckets[i] = newBucket()
	}
	return w
}

/*
========================
   Options
========================
*/

const (
	defaultTick    = 100 * time.Millisecond
	defaultSize    = 512
	defaultWorkers = 8
)

type config struct {
	tick    time.Duration
	size    int
	workers int
}

func defaultConfig() config {
	return config{tick: defaultTick, size: defaultSize, workers: defaultWorkers}
}

// Option 用于定制 Scheduler。
type Option func(*config)

// WithTick 设置最小时间刻度（默认 100ms）。
func WithTick(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.tick = d
		}
	}
}

// WithSize 设置每层时间轮的槽位数（默认 512，最小 2）。
func WithSize(n int) Option {
	return func(c *config) {
		if n >= 2 {
			c.size = n
		}
	}
}

// WithWorkers 设置执行任务的 worker 数量（默认 8）。
func WithWorkers(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.workers = n
		}
	}
}

/*
========================
   Scheduler
========================
*/

// Scheduler 是分层时间轮调度器。
//
// 线程模型：所有状态（各层时间轮、全局任务索引）只由唯一的驱动协程读写。
// 外部 API 通过 channel 把命令投递给驱动协程，查询/更新通过带响应 channel
// 的请求完成，因此不存在并发读写 map 的问题。
type Scheduler struct {
	tick time.Duration
	wp   *WorkerPool

	levels []*wheel           // levels[0] 最细，tick 逐层放大 size 倍
	index  map[uint64]*bucket // 全局索引：id -> 所在桶，跨层可见

	addCh    chan addReq
	cancelCh chan cancelReq
	getCh    chan getReq
	updCh    chan updReq
	stopCh   chan struct{}
	doneCh   chan struct{}

	stopped  atomic.Bool
	stopOnce sync.Once
}

type addReq struct{ task *Task }

type getReq struct {
	id   uint64
	resp chan getResp
}

type getResp struct {
	task *Task
	ok   bool
}

type updReq struct {
	id     uint64
	expire int64
	resp   chan bool
}

type cancelReq struct {
	id   uint64
	resp chan bool
}

// NewScheduler 创建并启动一个调度器。
func NewScheduler(opts ...Option) *Scheduler {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	s := &Scheduler{
		tick:     cfg.tick,
		wp:       NewWorkerPool(cfg.workers),
		levels:   []*wheel{newWheel(cfg.tick, cfg.size)},
		index:    make(map[uint64]*bucket),
		addCh:    make(chan addReq, 1024),
		cancelCh: make(chan cancelReq, 1024),
		getCh:    make(chan getReq, 64),
		updCh:    make(chan updReq, 64),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go s.loop()
	return s
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	defer close(s.doneCh)

	for {
		select {
		case <-ticker.C:
			// 用真实时间推进，避免 ticker 抖动累积成延迟。
			s.advance(0, time.Now().UnixNano())
		case req := <-s.addCh:
			s.add(req.task, time.Now().UnixNano())
		case req := <-s.cancelCh:
			req.resp <- s.cancel(req.id)
		case req := <-s.getCh:
			t, ok := s.get(req.id)
			req.resp <- getResp{task: t, ok: ok}
		case req := <-s.updCh:
			req.resp <- s.update(req.id, req.expire)
		case <-s.stopCh:
			return
		}
	}
}

/*
========================
   对外 API
========================
*/

// Schedule 在 t 时刻执行 fn（同 id 覆盖旧任务）。返回 false 表示调度器已停止。
//
// 入轮是异步的：返回 true 仅代表命令已投递，若要确认任务已可查询请用 Get 轮询。
func (s *Scheduler) Schedule(id uint64, t time.Time, fn func(uint64, time.Time)) bool {
	if fn == nil || s.stopped.Load() {
		return false
	}
	task := &Task{id: id, fn: fn, expire: t.UnixNano()}
	select {
	case s.addCh <- addReq{task: task}:
		return true
	case <-s.stopCh:
		return false
	}
}

// Update 修改已有任务的执行时间（保持原回调）。任务不存在或调度器已停止时返回 false。
func (s *Scheduler) Update(id uint64, t time.Time) bool {
	resp := make(chan bool, 1)
	if !s.sendUpd(updReq{id: id, expire: t.UnixNano(), resp: resp}) {
		return false
	}
	select {
	case ok := <-resp:
		return ok
	case <-s.doneCh:
		select {
		case ok := <-resp:
			return ok
		default:
			return false
		}
	}
}

// Get 查询任务快照（只读，返回的 *Task 不可改）。
func (s *Scheduler) Get(id uint64) (*Task, bool) {
	resp := make(chan getResp, 1)
	if !s.sendGet(getReq{id: id, resp: resp}) {
		return nil, false
	}
	select {
	case r := <-resp:
		return r.task, r.ok
	case <-s.doneCh:
		select {
		case r := <-resp:
			return r.task, r.ok
		default:
			return nil, false
		}
	}
}

// TTL 返回剩余时间；任务不存在或已到期返回 false。
func (s *Scheduler) TTL(id uint64) (time.Duration, bool) {
	task, ok := s.Get(id)
	if !ok {
		return 0, false
	}
	ttl := time.Until(time.Unix(0, task.expire))
	if ttl <= 0 {
		return 0, false
	}
	return ttl, true
}

// Cancel 取消任务并返回是否命中。已进入执行队列的任务会在真正运行前
// 再次检查取消标记（尽力而为）。
func (s *Scheduler) Cancel(id uint64) bool {
	resp := make(chan bool, 1)
	if !s.sendCancel(cancelReq{id: id, resp: resp}) {
		return false
	}
	select {
	case ok := <-resp:
		return ok
	case <-s.doneCh:
		select {
		case ok := <-resp:
			return ok
		default:
			return false
		}
	}
}

// Stop 停止调度器，等待驱动协程退出并执行完已提交的任务。可重复调用。
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		s.stopped.Store(true)
		close(s.stopCh)
		<-s.doneCh
		s.wp.Stop()
	})
}

/*
========================
   驱动协程内部实现（无锁，全部串行）
========================
*/

func (s *Scheduler) sendGet(req getReq) bool {
	if s.stopped.Load() {
		return false
	}
	select {
	case s.getCh <- req:
		return true
	case <-s.stopCh:
		return false
	}
}

func (s *Scheduler) sendCancel(req cancelReq) bool {
	if s.stopped.Load() {
		return false
	}
	select {
	case s.cancelCh <- req:
		return true
	case <-s.stopCh:
		return false
	}
}

func (s *Scheduler) sendUpd(req updReq) bool {
	if s.stopped.Load() {
		return false
	}
	select {
	case s.updCh <- req:
		return true
	case <-s.stopCh:
		return false
	}
}

func (s *Scheduler) get(id uint64) (*Task, bool) {
	if b, ok := s.index[id]; ok {
		if t, ok := b.tasks[id]; ok {
			return t, true
		}
	}
	return nil, false
}

func (s *Scheduler) update(id uint64, expire int64) bool {
	t, ok := s.get(id)
	if !ok {
		return false
	}
	// 复用原回调，通过 add 完成覆盖语义（旧条目会被标记取消）。
	s.add(&Task{id: id, fn: t.fn, expire: expire}, time.Now().UnixNano())
	return true
}

// add 负责覆盖旧任务并把新任务放到合适的层级。
func (s *Scheduler) add(task *Task, now int64) {
	if b, ok := s.index[task.id]; ok {
		if old, ok := b.tasks[task.id]; ok {
			atomic.StoreInt32(&old.canceled, 1)
			delete(b.tasks, task.id)
		}
		delete(s.index, task.id)
	}
	s.place(task, now)
}

func (s *Scheduler) cancel(id uint64) bool {
	b, ok := s.index[id]
	if !ok {
		return false
	}
	t, hit := b.tasks[id]
	if hit {
		atomic.StoreInt32(&t.canceled, 1)
		delete(b.tasks, id)
	}
	delete(s.index, id)
	return hit
}

// place 把任务放进「能装下它的最细一层」；已经到期则立即提交执行。
func (s *Scheduler) place(task *Task, now int64) {
	delay := task.expire - now
	if delay <= 0 {
		s.submit(task)
		return
	}

	for i := 0; ; i++ {
		if i >= len(s.levels) && !s.addLevel() {
			// 已达 tick 可表示的上限（极端远期），用最粗一层兜底。
			i = len(s.levels) - 1
		}
		lv := s.levels[i]
		tick := int64(lv.tick)

		// 向上取整：保证不会早于 expire 触发（最多晚一个 tick）。
		ticks := (delay + tick - 1) / tick
		if i == len(s.levels)-1 && ticks >= int64(lv.size) {
			// 最粗一层也装不下：落桶后在到期时靠 advance 的重排继续纠正。
			ticks %= int64(lv.size)
			if ticks == 0 {
				ticks = 1
			}
		}
		if ticks < int64(lv.size) {
			pos := (lv.cursor + int(ticks)) % lv.size
			b := lv.buckets[pos]
			b.tasks[task.id] = task
			s.index[task.id] = b
			return
		}
	}
}

// addLevel 追加更粗的一层；tick 会溢出 int64 时返回 false。
func (s *Scheduler) addLevel() bool {
	last := s.levels[len(s.levels)-1]
	if int64(last.tick) > math.MaxInt64/int64(last.size) {
		return false
	}
	next := newWheel(time.Duration(int64(last.tick)*int64(last.size)), last.size)
	s.levels = append(s.levels, next)
	return true
}

// advance 推进第 i 层；某层转满一圈时，让上一层前进一格。
func (s *Scheduler) advance(i int, now int64) {
	lv := s.levels[i]
	b := lv.buckets[lv.cursor]
	// 先换上新桶，保证重新入桶（place）不会写回正在遍历的 map。
	lv.buckets[lv.cursor] = newBucket()

	for id, task := range b.tasks {
		delete(s.index, id)

		if atomic.LoadInt32(&task.canceled) == 1 {
			continue
		}
		if task.expire <= now {
			s.submit(task)
		} else {
			// 还没到期（层级粒度导致），按当前时间重新安置。
			s.place(task, now)
		}
	}

	lv.cursor = (lv.cursor + 1) % lv.size
	// 本层转满一圈 == 上一层的一格。
	if lv.cursor == 0 && i+1 < len(s.levels) {
		s.advance(i+1, now)
	}
}

func (s *Scheduler) submit(task *Task) {
	execTime := time.Unix(0, task.expire)
	id := task.id
	fn := task.fn
	// 提交时再查一次取消标记，缩小「已取消仍执行」的窗口。
	s.wp.Submit(func() {
		if atomic.LoadInt32(&task.canceled) == 1 {
			return
		}
		fn(id, execTime)
	})
}
