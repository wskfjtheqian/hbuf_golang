package htwheel

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// ErrRetriesExhausted 表示任务重试次数已用尽、被最终放弃。
// 可通过 errors.Is(err, ErrRetriesExhausted) 在错误回调中区分
// 「本次失败仍会重试」与「重试已用完、任务被丢弃」。
var ErrRetriesExhausted = errors.New("htwheel: retries exhausted")

// ErrStopped 表示调度器已停止，无法再接收/操作任务。
var ErrStopped = errors.New("htwheel: scheduler stopped")

// ErrNotFound 表示任务不存在（Get/TTL/Update/Cancel 未命中）。
var ErrNotFound = errors.New("htwheel: task not found")

// ErrNilTask 表示传入的任务为 nil。
var ErrNilTask = errors.New("htwheel: nil task")

type Handler[P Recoverable] func(id uint64, name string, execTime time.Time, param P) error

/*
========================
   Task
========================
*/

// Task 是一条调度条目。除 canceled 外，字段在发布给调度器后不再被修改，
// 因此外部通过 Expire 读取是安全的。
type Task[P Recoverable] struct {
	id       uint64
	expire   int64 // 计划执行时间（UnixNano）
	canceled int32
	retries  int           // 剩余可重试次数（默认 0，即不重试）。
	delay    time.Duration // 重试间隔。
	param    P             // 参数
	persist  bool          // 是否持久化到 Store（由 ScheduleWithParam / load 置位）。
	name     string        // 任务名称
}

// Expire 返回计划执行时间（纳秒）。
// 使用指针接收者，避免复制含 atomic 字段的结构体。
func (t *Task[P]) Expire() int64 { return t.expire }

// WithTaskRetry 覆盖单条任务的重试策略：失败后最多重试 retries 次，每次间隔 delay。
// 需配合 ScheduleTask 使用（先构造 Task 再传入）。
func (t *Task[P]) WithTaskRetry(retries int, delay time.Duration) *Task[P] {
	if retries < 0 {
		retries = 0
	}
	t.retries = retries
	if delay > 0 {
		t.delay = delay
	}
	return t
}

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

type bucket[P Recoverable] struct {
	tasks map[uint64]*Task[P]
}

func newBucket[P Recoverable]() *bucket[P] {
	return &bucket[P]{tasks: make(map[uint64]*Task[P])}
}

// wheel 是单层时间轮。它不持有 goroutine、也不做任何加锁：
// 所有读写都只发生在 Scheduler 的驱动协程内，由结构保证串行。
type wheel[P Recoverable] struct {
	tick    time.Duration
	size    int
	cursor  int
	buckets []*bucket[P]
}

func newWheel[P Recoverable](tick time.Duration, size int) *wheel[P] {
	// size 必须 >= 2，否则单层永远装不下任何任务（ticks 恒 >= 1）。
	if size < 2 {
		size = 2
	}
	w := &wheel[P]{tick: tick, size: size}
	w.buckets = make([]*bucket[P], size)
	for i := range w.buckets {
		w.buckets[i] = newBucket[P]()
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

type config[P Recoverable] struct {
	tick    time.Duration // 设置最小时间刻度（默认 100ms）。
	size    int           // 设置每层时间轮的槽位数（默认 512，最小 2）。
	workers int           // 设置执行任务的 worker 数量（默认 8）。
	store   Store[P]
	delay   time.Duration // 设置默认重试间隔（默认 0，即不重试）。
	retries int           // 设置默认重试次数（默认 0，即不重试）。
	batch   int           // 设置单次扫描的最大条数（默认 256）。
	ahead   time.Duration // 设置扫描窗口（默认 30s）。
	poll    time.Duration // 设置扫描存储层的间隔（默认 1s）。
	onErr   func(error)
}

func defaultConfig[P Recoverable]() config[P] {
	return config[P]{
		tick:    defaultTick,
		size:    defaultSize,
		workers: defaultWorkers,
		poll:    time.Second,
		ahead:   30 * time.Second,
		batch:   256,
		delay:   0,
		retries: 0,
	}
}

// Option 用于定制 Scheduler。
type Option[P Recoverable] func(*config[P])

// WithTick 设置最小时间刻度（默认 100ms）。
func WithTick[P Recoverable](d time.Duration) Option[P] {
	return func(c *config[P]) {
		if d > 0 {
			c.tick = d
		}
	}
}

// WithSize 设置每层时间轮的槽位数（默认 512，最小 2）。
func WithSize[P Recoverable](n int) Option[P] {
	return func(c *config[P]) {
		if n >= 2 {
			c.size = n
		}
	}
}

// WithWorkers 设置执行任务的 worker 数量（默认 8）。
func WithWorkers[P Recoverable](n int) Option[P] {
	return func(c *config[P]) {
		if n > 0 {
			c.workers = n
		}
	}
}

// WithStore 设置任务存储。
func WithStore[P Recoverable](s Store[P]) Option[P] {
	return func(c *config[P]) {
		c.store = s
	}
}

// WithPollInterval 设置扫描存储层的间隔（默认 1s）。
func WithPollInterval[P Recoverable](d time.Duration) Option[P] {
	return func(c *config[P]) {
		if d > 0 {
			c.poll = d
		}
	}
}

// WithLookahead 设置扫描窗口：把 Expire 在 now+lookahead 之前的任务提前读入内存（默认 30s）。
func WithLookahead[P Recoverable](d time.Duration) Option[P] {
	return func(c *config[P]) {
		if d > 0 {
			c.ahead = d
		}
	}
}

// WithBatchSize 设置单次扫描的最大条数（默认 256）。
func WithBatchSize[P Recoverable](n int) Option[P] {
	return func(c *config[P]) {
		if n > 0 {
			c.batch = n
		}
	}
}

// WithErrorHandler 设置存储层/处理器错误的回调（默认忽略）。
// 每次失败尝试与被放弃的任务都会通过它上报，可用 errors.Is(err, ErrRetriesExhausted)
// 区分「这次失败还会重试」与「重试已用完、任务被丢弃」。
func WithErrorHandler[P Recoverable](fn func(error)) Option[P] {
	return func(c *config[P]) {
		c.onErr = fn
	}
}

// WithDefaultRetry 设置新建任务的默认重试策略：失败后最多重试 retries 次，
// 每次间隔 delay（默认 0，即不重试）。单条任务可用 WithTaskRetry 覆盖。
// 仅影响之后 Schedule 的任务；直接写入存储层的记录以 Record 自身字段为准。
func WithDefaultRetry[P Recoverable](retries int, delay time.Duration) Option[P] {
	return func(c *config[P]) {
		if retries < 0 {
			retries = 0
		}
		c.retries = retries
		if delay > 0 {
			c.delay = delay
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
type Scheduler[P Recoverable] struct {
	tick time.Duration
	wp   *WorkerPool

	levels []*wheel[P]           // levels[0] 最细，tick 逐层放大 size 倍
	index  map[uint64]*bucket[P] // 全局索引：id -> 所在桶，跨层可见
	sunk   map[uint64]*Task[P]   // 下沉索引：id -> 已下沉到存储、未入轮的任务

	addCh    chan addReq[P]
	cancelCh chan cancelReq
	getCh    chan getReq[P]
	updCh    chan updReq[P]
	finishCh chan uint64
	stopCh   chan struct{}
	doneCh   chan struct{}

	stopped  atomic.Bool
	stopOnce sync.Once

	store Store[P]
	poll  time.Duration //设置扫描存储层的间隔（默认 1s）。
	ahead time.Duration //设置扫描窗口（默认 30s）。
	batch int           //设置单次扫描的最大条数（默认 256）。

	// 默认重试策略（新建任务时生效，单条任务可用 WithTaskRetry 覆盖）。
	retries int
	delay   time.Duration

	onErr func(error) // 存储层/处理器错误回调（默认忽略）。

	// loaded 记录已从存储加载过、且到期时间未变的任务 id -> expire，
	// 用于在轮询加载时去重，避免重复入轮。
	loaded  map[uint64]int64
	handler Handler[P]
}

type addReq[P Recoverable] struct{ task *Task[P] }

type getReq[P Recoverable] struct {
	id   uint64
	resp chan getResp[P]
}

type getResp[P Recoverable] struct {
	task *Task[P]
	err  error
}

type updReq[P Recoverable] struct {
	id     uint64
	expire int64
	resp   chan error
	param  P
}

type cancelReq struct {
	id   uint64
	resp chan error
}

// NewScheduler 创建并启动一个调度器。
func NewScheduler[P Recoverable](handler Handler[P], opts ...Option[P]) *Scheduler[P] {
	cfg := defaultConfig[P]()
	for _, o := range opts {
		o(&cfg)
	}

	s := &Scheduler[P]{
		handler:  handler,
		store:    cfg.store,
		tick:     cfg.tick,
		poll:     cfg.poll,
		ahead:    cfg.ahead,
		batch:    cfg.batch,
		retries:  cfg.retries,
		delay:    cfg.delay,
		onErr:    cfg.onErr,
		wp:       NewWorkerPool(cfg.workers),
		levels:   []*wheel[P]{newWheel[P](cfg.tick, cfg.size)},
		index:    make(map[uint64]*bucket[P]),
		sunk:     make(map[uint64]*Task[P]),
		loaded:   make(map[uint64]int64),
		addCh:    make(chan addReq[P], 1024),
		cancelCh: make(chan cancelReq, 1024),
		getCh:    make(chan getReq[P], 64),
		updCh:    make(chan updReq[P], 64),
		finishCh: make(chan uint64, 1024),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	go s.loop()
	return s
}

func (s *Scheduler[P]) loop() {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	defer close(s.doneCh)

	poll := time.NewTicker(s.poll)
	defer poll.Stop()

	s.loadTasks()
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
			t, err := s.get(req.id)
			req.resp <- getResp[P]{task: t, err: err}
		case req := <-s.updCh:
			req.resp <- s.update(req.id, req.expire, req.param)
		case id := <-s.finishCh:
			s.finish(id)
		case <-poll.C:
			s.loadTasks()
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

// Schedule 在 t 时刻执行任务（同 id 覆盖旧任务）。返回错误表示调度器已停止。
//
// 入轮是异步的：返回 nil 仅代表命令已投递，若要确认任务已可查询请用 Get 轮询。
// 配置了 Store 时任务会自动持久化；p 需实现 Recoverable，重启后可经 Store.Load 恢复。
func (s *Scheduler[P]) Schedule(id uint64, name string, t time.Time, p P) error {
	if s.stopped.Load() {
		return ErrStopped
	}
	task := &Task[P]{
		id:      id,
		expire:  t.UnixNano(),
		name:    name,
		param:   p,
		persist: s.store != nil,
		retries: s.retries,
		delay:   s.delay,
	}
	return s.ScheduleTask(task)
}

// ScheduleTask 提交一条已构造好的任务（可携带单条任务的重试策略）。
// task 为 nil 时返回 ErrNilTask，调度器已停止时返回 ErrStopped。
func (s *Scheduler[P]) ScheduleTask(task *Task[P]) error {
	if task == nil {
		return ErrNilTask
	}
	if s.stopped.Load() {
		return ErrStopped
	}
	// 未显式指定重试时，套用默认重试策略。
	if task.retries == 0 && s.retries > 0 {
		task.retries = s.retries
		task.delay = s.delay
	}
	select {
	case s.addCh <- addReq[P]{task: task}:
		return nil
	case <-s.stopCh:
		return ErrStopped
	}
}

// Update 修改已有任务的执行时间（保持原回调）。任务不存在返回 ErrNotFound，
// 调度器已停止返回 ErrStopped。对已下沉到存储的远任务同样生效。
func (s *Scheduler[P]) Update(id uint64, t time.Time, p P) error {
	resp := make(chan error, 1)
	if !s.sendUpd(updReq[P]{id: id, expire: t.UnixNano(), resp: resp, param: p}) {
		return ErrStopped
	}
	select {
	case err := <-resp:
		return err
	case <-s.doneCh:
		select {
		case err := <-resp:
			return err
		default:
			return ErrStopped
		}
	}
}

// Get 查询任务快照（只读，返回的 *Task 不可改）。任务不存在返回 ErrNotFound，
// 调度器已停止返回 ErrStopped。已下沉到存储的远任务同样可查询。
func (s *Scheduler[P]) Get(id uint64) (*Task[P], error) {
	resp := make(chan getResp[P], 1)
	if !s.sendGet(getReq[P]{id: id, resp: resp}) {
		return nil, ErrStopped
	}
	select {
	case r := <-resp:
		return r.task, r.err
	case <-s.doneCh:
		select {
		case r := <-resp:
			return r.task, r.err
		default:
			return nil, ErrStopped
		}
	}
}

// TTL 返回剩余时间；任务不存在返回 ErrNotFound，已到期或已停止返回相应错误。
func (s *Scheduler[P]) TTL(id uint64) (time.Duration, error) {
	task, err := s.Get(id)
	if err != nil {
		return 0, err
	}
	ttl := time.Until(time.Unix(0, task.expire))
	if ttl <= 0 {
		return 0, ErrNotFound
	}
	return ttl, nil
}

// Cancel 取消任务。任务不存在返回 ErrNotFound，调度器已停止返回 ErrStopped。
// 已进入执行队列的任务会在真正运行前再次检查取消标记（尽力而为）。
func (s *Scheduler[P]) Cancel(id uint64) error {
	resp := make(chan error, 1)
	if !s.sendCancel(cancelReq{id: id, resp: resp}) {
		return ErrStopped
	}
	select {
	case err := <-resp:
		return err
	case <-s.doneCh:
		select {
		case err := <-resp:
			return err
		default:
			return ErrStopped
		}
	}
}

// Stop 停止调度器，等待驱动协程退出并执行完已提交的任务。可重复调用。
func (s *Scheduler[P]) Stop() {
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

func (s *Scheduler[P]) sendGet(req getReq[P]) bool {
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

func (s *Scheduler[P]) sendCancel(req cancelReq) bool {
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

func (s *Scheduler[P]) sendUpd(req updReq[P]) bool {
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

func (s *Scheduler[P]) get(id uint64) (*Task[P], error) {
	if b, ok := s.index[id]; ok {
		if t, ok := b.tasks[id]; ok {
			return t, nil
		}
	}
	// 下沉到存储的远任务也能查到（保留 id/expire/fn 等元信息）。
	if t, ok := s.sunk[id]; ok {
		return t, nil
	}
	// 内存中没有时，回退到存储层查询（例如重启前遗留、尚未被 Load 拉回的任务）。
	if s.store != nil {
		if p, exp, err := s.store.Get(id); err == nil {
			return &Task[P]{id: id, expire: exp.UnixNano(), param: p, persist: true, name: p.Name()}, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Scheduler[P]) update(id uint64, expire int64, param P) error {
	t, err := s.get(id)
	if err != nil {
		// 不在内存轮：查下沉索引，仍可更新（保留原回调与参数）。
		if st, sunk := s.sunk[id]; sunk {
			// 已取消的下沉任务不应被更新复活。
			if atomic.LoadInt32(&st.canceled) == 1 {
				return ErrNotFound
			}
			s.add(&Task[P]{
				id:      id,
				expire:  expire,
				param:   param,
				persist: st.persist,
				retries: st.retries,
				delay:   st.delay,
				name:    st.name,
			}, time.Now().UnixNano())
			return nil
		}
		return ErrNotFound
	}
	// 复用原回调与参数，通过 add 完成覆盖语义（旧条目会被标记取消，
	// 持久化任务会经 add 自动调用 Store.Add/Update 完成更新；若新时间仍很远，
	// add 会继续将其下沉到存储）。
	s.add(&Task[P]{
		id:      id,
		expire:  expire,
		param:   param,
		persist: t.persist,
		retries: t.retries,
		delay:   t.delay,
		name:    t.name,
	}, time.Now().UnixNano())
	return nil
}

// add 负责覆盖旧任务并把新任务放到合适的层级。
func (s *Scheduler[P]) add(task *Task[P], now int64) {
	existed := false
	if b, ok := s.index[task.id]; ok {
		if old, ok := b.tasks[task.id]; ok {
			atomic.StoreInt32(&old.canceled, 1)
			delete(b.tasks, task.id)
			existed = true
		}
		delete(s.index, task.id)
	}
	// 之前若下沉过，入轮前先清掉下沉索引（无论本次是否继续下沉都会重建）。
	if _, ok := s.sunk[task.id]; ok {
		delete(s.sunk, task.id)
		existed = true
	}

	// 设置了存储层且任务要求持久化时：所有任务都写存储。
	// 若任务还很远（expire - now > ahead），则「只写存储、不入内存轮」，
	// 由 loadTasks 在临近到期时（Load(now+ahead)）再拉回内存，减少常驻内存的任务量。
	persist := task.persist && s.store != nil
	far := persist && task.expire-now > int64(s.ahead)
	if persist {
		t := time.Unix(0, task.expire)
		if existed {
			if err := s.store.Update(task.id, t, task.param); err != nil {
				s.report(err)
			}
		} else if err := s.store.Add(task.id, t, task.param); err != nil {
			s.report(err)
		}
	}

	if far {
		// 远任务只保留在存储，不入时间轮；记录到下沉索引，供 Cancel/Update/Get 命中。
		s.sunk[task.id] = task
		return
	}

	s.place(task, now)
}

func (s *Scheduler[P]) cancel(id uint64) error {
	b, ok := s.index[id]
	if !ok {
		// 不在内存轮：可能是已下沉到存储的远任务。
		if t, sunk := s.sunk[id]; sunk {
			delete(s.sunk, id)
			atomic.StoreInt32(&t.canceled, 1)
			if s.store != nil {
				if err := s.store.Remove(id); err != nil {
					s.report(err)
				}
			}
			return nil
		}
		// 也不在下沉索引：尝试从存储移除（可能是重启前遗留的持久化任务）。
		if s.store != nil {
			if err := s.store.Remove(id); err == nil {
				return nil
			}
		}
		return ErrNotFound
	}
	t, hit := b.tasks[id]
	if hit {
		atomic.StoreInt32(&t.canceled, 1)
		delete(b.tasks, id)
	}
	delete(s.index, id)
	// 命中且该任务曾持久化时，同步移除存储记录。
	if hit && t.persist && s.store != nil {
		if err := s.store.Remove(id); err != nil {
			s.report(err)
		}
	}
	if !hit {
		return ErrNotFound
	}
	return nil
}

// place 把任务放进「能装下它的最细一层」；已经到期则立即提交执行。
func (s *Scheduler[P]) place(task *Task[P], now int64) {
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
func (s *Scheduler[P]) addLevel() bool {
	last := s.levels[len(s.levels)-1]
	if int64(last.tick) > math.MaxInt64/int64(last.size) {
		return false
	}
	next := newWheel[P](time.Duration(int64(last.tick)*int64(last.size)), last.size)
	s.levels = append(s.levels, next)
	return true
}

// advance 推进第 i 层；某层转满一圈时，让上一层前进一格。
func (s *Scheduler[P]) advance(i int, now int64) {
	lv := s.levels[i]
	b := lv.buckets[lv.cursor]
	// 先换上新桶，保证重新入桶（place）不会写回正在遍历的 map。
	lv.buckets[lv.cursor] = newBucket[P]()

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

// submit 把到期任务交给 worker 池执行；回调 panic 时按重试策略重试。
func (s *Scheduler[P]) submit(task *Task[P]) {
	execTime := time.Unix(0, task.expire)
	id := task.id
	// 提交时再查一次取消标记，缩小「已取消仍执行」的窗口。
	s.wp.Submit(func() {
		if atomic.LoadInt32(&task.canceled) == 1 {
			return
		}
		s.execTask(task, id, execTime)
	})
}

// execTask 真正执行一次任务；handler 返回 error 或发生 panic 都会走重试流程。
// 它运行在 worker 协程内，重试会重新入轮（走 addCh），由驱动协程串行处理。
func (s *Scheduler[P]) execTask(task *Task[P], id uint64, execTime time.Time) {
	failed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				failed = true
				s.onTaskPanic(task, id)
			}
		}()
		// handler 返回 error 等价于失败，触发重试。
		if err := s.handler(id, task.name, execTime, task.param); err != nil {
			failed = true
			s.onTaskPanic(task, id)
		}
	}()
	// 执行成功（未失败）时，通知驱动协程清理持久化记录。
	if !failed && task.persist {
		s.notifyFinish(id)
	}
}

// notifyFinish 把「任务执行完成」投递给驱动协程，由其串行移除持久化记录。
// 采用阻塞投递：保证「执行成功 → 必删存储」的强一致，避免完成通知丢失导致
// 重启后被 Load 拉回重复执行。finishCh 有 1024 缓冲，正常情况下不会阻塞。
func (s *Scheduler[P]) notifyFinish(id uint64) {
	s.finishCh <- id
}

// finish 在驱动协程内清理已执行完成任务的持久化记录。
func (s *Scheduler[P]) finish(id uint64) {
	if s.store == nil {
		return
	}
	if err := s.store.Remove(id); err != nil {
		s.report(err)
	}
	s.deleteLoaded(id)
}

// deleteLoaded 移除 loaded 去重记录，避免重启后误判为「已加载」。
func (s *Scheduler[P]) deleteLoaded(id uint64) {
	delete(s.loaded, id)
}

// onTaskPanic 在回调 panic 后决定重试或放弃。
func (s *Scheduler[P]) onTaskPanic(task *Task[P], id uint64) {
	if atomic.LoadInt32(&task.canceled) == 1 {
		return
	}
	if task.retries > 0 {
		task.retries--
		delay := task.delay
		if delay <= 0 {
			delay = s.tick
		}
		next := &Task[P]{
			id:      id,
			expire:  time.Now().Add(delay).UnixNano(),
			retries: task.retries,
			delay:   task.delay,
			param:   task.param,
			persist: task.persist,
			name:    task.name,
		}
		// 重新入轮；调度器停止时直接放弃。
		select {
		case s.addCh <- addReq[P]{task: next}:
		default:
			s.report(ErrRetriesExhausted)
			if task.persist {
				s.notifyFinish(id)
			}
		}
		return
	}
	// 重试次数用尽：上报、清理持久化并丢弃。
	s.report(ErrRetriesExhausted)
	if task.persist {
		s.notifyFinish(id)
	}
}

// loadTasks 从存储层加载待执行任务（重启恢复 + 周期性轮询）。
func (s *Scheduler[P]) loadTasks() {
	if s.store == nil {
		return
	}
	now := time.Now().UnixNano()
	s.pruneLoaded(now)

	recs, err := s.store.Load(time.Unix(0, now).Add(s.ahead), s.batch)
	if err != nil {
		s.report(err)
		return
	}
	for _, rec := range recs {
		s.load(rec)
	}
}

// pruneLoaded 清理 loaded 去重 map 中已严重过期的条目，防止其无限增长。
// loaded 记录「已加载任务的 id -> expire」，正常路径下任务执行完成后由 finish
// 删除；但 Recover 返回 nil 被跳过、或异常导致 finish 未触发的任务会永久残留。
// 这里清理 expire 早于 now-ahead 的条目：这些任务早已超出扫描窗口，即便误删，
// 下次 Load 拉回时也会重新入轮（过期任务会被 place 立即提交执行），影响极小。
func (s *Scheduler[P]) pruneLoaded(now int64) {
	threshold := now - int64(s.ahead)
	for id, exp := range s.loaded {
		if exp < threshold {
			delete(s.loaded, id)
		}
	}
}

// load 把从存储恢复出的一条记录重新入轮，并按「id + 到期时间」去重。
func (s *Scheduler[P]) load(rec P) {
	id := rec.ID()
	exp := rec.Expire().UnixNano()

	// 去重：已加载且到期时间未变的记录不再重复入轮。
	if prev, ok := s.loaded[id]; ok && prev == exp {
		return
	}

	s.loaded[id] = exp
	s.add(&Task[P]{id: id,
		expire:  exp,
		param:   rec,
		persist: true,
		name:    rec.Name(),
	}, time.Now().UnixNano())
}

// report 上报存储层/处理器错误；未设置 onErr 时静默忽略。
func (s *Scheduler[P]) report(err error) {
	if err == nil || s.onErr == nil {
		return
	}
	s.onErr(err)
}
