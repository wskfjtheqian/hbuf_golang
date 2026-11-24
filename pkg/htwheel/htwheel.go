package htwheel

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

type Handler[P any] func(ctx context.Context, id uint64, name string, execTime time.Time, retries int, param P) error

type LoaderTask[P any] struct {
	id     uint64    // 任务 ID
	name   string    // 任务名称
	expire time.Time // 计划执行时间
	param  P         // 参数
}

func NewLoaderTask[P any](id uint64, name string, expire time.Time, param P) *LoaderTask[P] {
	return &LoaderTask[P]{id: id, name: name, expire: expire, param: param}
}

type Loader[P any] func(ctx context.Context, before time.Time, limit int) ([]*LoaderTask[P], error)

/*
========================
   Task
========================
*/

// Task 是一条调度条目。除 canceled 外，字段在发布给调度器后不再被修改，
// 因此外部通过 Expire 读取是安全的。
type Task[P any] struct {
	id       uint64
	expire   int64 // 计划执行时间（UnixMilli）
	canceled atomic.Bool
	retries  int           // 剩余可重试次数（默认 0，即不重试）。
	delay    time.Duration // 重试间隔。
	param    P             // 参数
	name     string        // 任务名称
}

// Expire 返回计划执行时间（毫秒）。
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

type bucket[P any] struct {
	tasks map[uint64]*Task[P]
}

func newBucket[P any]() *bucket[P] {
	return &bucket[P]{tasks: make(map[uint64]*Task[P])}
}

// wheel 是单层时间轮。它不持有 goroutine、也不做任何加锁：
// 所有读写都只发生在 Scheduler 的驱动协程内，由结构保证串行。
type wheel[P any] struct {
	tick    time.Duration
	size    int
	cursor  int
	buckets []*bucket[P]
}

func newWheel[P any](tick time.Duration, size int) *wheel[P] {
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

type config[P any] struct {
	tick    time.Duration // 设置最小时间刻度（默认 100ms）。
	size    int           // 设置每层时间轮的槽位数（默认 512，最小 2）。
	workers int           // 设置执行任务的 worker 数量（默认 8）。
	loader  Loader[P]
	delay   time.Duration // 设置默认重试间隔（默认 0，即不重试）。
	retries int           // 设置默认重试次数（默认 0，即不重试）。
	batch   int           // 设置单次扫描的最大条数（默认 256）。
	ahead   time.Duration // 设置扫描窗口（默认 30s）。
	poll    time.Duration // 设置扫描存储层的间隔（默认 1s）。
}

func defaultConfig[P any]() config[P] {
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
type Option[P any] func(*config[P])

// WithTick 设置最小时间刻度（默认 100ms）。
func WithTick[P any](d time.Duration) Option[P] {
	return func(c *config[P]) {
		if d > 0 {
			c.tick = d
		}
	}
}

// WithSize 设置每层时间轮的槽位数（默认 512，最小 2）。
func WithSize[P any](n int) Option[P] {
	return func(c *config[P]) {
		if n >= 2 {
			c.size = n
		}
	}
}

// WithWorkers 设置执行任务的 worker 数量（默认 8）。
func WithWorkers[P any](n int) Option[P] {
	return func(c *config[P]) {
		if n > 0 {
			c.workers = n
		}
	}
}

// WithLoader 设置任务加载器。
func WithLoader[P any](s Loader[P]) Option[P] {
	return func(c *config[P]) {
		c.loader = s
	}
}

// WithPollInterval 设置扫描存储层的间隔（默认 1s）。
func WithPollInterval[P any](d time.Duration) Option[P] {
	return func(c *config[P]) {
		if d > 0 {
			c.poll = d
		}
	}
}

// WithLookahead 设置扫描窗口：把 Expire 在 now+lookahead 之前的任务提前读入内存（默认 30s）。
func WithLookahead[P any](d time.Duration) Option[P] {
	return func(c *config[P]) {
		if d > 0 {
			c.ahead = d
		}
	}
}

// WithBatchSize 设置单次扫描的最大条数（默认 256）。
func WithBatchSize[P any](n int) Option[P] {
	return func(c *config[P]) {
		if n > 0 {
			c.batch = n
		}
	}
}

// WithDefaultRetry 设置新建任务的默认重试策略：失败后最多重试 retries 次，
// 每次间隔 delay（默认 0，即不重试）。单条任务可用 WithTaskRetry 覆盖。
// 仅影响之后 Schedule 的任务；直接写入存储层的记录以 Record 自身字段为准。
func WithDefaultRetry[P any](retries int, delay time.Duration) Option[P] {
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
type Scheduler[P any] struct {
	tick time.Duration
	wp   *WorkerPool

	levels []*wheel[P]           // levels[0] 最细，tick 逐层放大 size 倍
	index  map[uint64]*bucket[P] // 全局索引：id -> 所在桶，跨层可见

	addCh    chan addReq[P]
	cancelCh chan cancelReq
	getCh    chan getReq[P]
	updCh    chan updReq[P]
	finishCh chan uint64
	stopCh   chan struct{}
	doneCh   chan struct{}

	stopped  atomic.Bool
	stopOnce sync.Once

	loader Loader[P]
	poll   time.Duration //设置扫描存储层的间隔（默认 1s）。
	ahead  time.Duration //设置扫描窗口（默认 30s）。
	batch  int           //设置单次扫描的最大条数（默认 256）。

	// 默认重试策略（新建任务时生效，单条任务可用 WithTaskRetry 覆盖）。
	retries int
	delay   time.Duration

	// loaded 记录已从存储加载过、且到期时间未变的任务 id -> expire，
	// 用于在轮询加载时去重，避免重复入轮。
	loaded  map[uint64]int64
	handler Handler[P]
}

type addReq[P any] struct{ task *Task[P] }

type getReq[P any] struct {
	id   uint64
	resp chan getResp[P]
}

type getResp[P any] struct {
	task *Task[P]
	ok   bool
}

type updReq[P any] struct {
	id     uint64
	name   string
	expire int64
	param  P
}

type cancelReq struct {
	id uint64
}

// NewScheduler 创建并启动一个调度器。
func NewScheduler[P any](handler Handler[P], opts ...Option[P]) *Scheduler[P] {
	cfg := defaultConfig[P]()
	for _, o := range opts {
		o(&cfg)
	}

	s := &Scheduler[P]{
		handler:  handler,
		loader:   cfg.loader,
		tick:     cfg.tick,
		poll:     cfg.poll,
		ahead:    cfg.ahead,
		batch:    cfg.batch,
		retries:  cfg.retries,
		delay:    cfg.delay,
		wp:       NewWorkerPool(cfg.workers),
		levels:   []*wheel[P]{newWheel[P](cfg.tick, cfg.size)},
		index:    make(map[uint64]*bucket[P]),
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
			s.advance(0, htime.NowTime())
		case req := <-s.addCh:
			s.add(req.task, htime.NowTime())
		case req := <-s.cancelCh:
			s.cancel(req.id)
		case req := <-s.getCh:
			t, ok := s.get(req.id)
			req.resp <- getResp[P]{task: t, ok: ok}
		case req := <-s.updCh:
			s.update(req.id, req.name, req.expire, req.param)
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
// 配置了 Store 时任务会自动持久化；p 需实现 any，重启后可经 Store.Load 恢复。
func (s *Scheduler[P]) Schedule(id uint64, name string, t time.Time, p P) {
	if s.stopped.Load() {
		return
	}
	task := &Task[P]{
		id:      id,
		expire:  t.UnixMilli(),
		name:    name,
		param:   p,
		retries: s.retries,
		delay:   s.delay,
	}
	s.ScheduleTask(task)
}

// ScheduleTask 提交一条已构造好的任务（可携带单条任务的重试策略）。
// task 为 nil 时返回 ErrNilTask，调度器已停止时返回 ErrStopped。
func (s *Scheduler[P]) ScheduleTask(task *Task[P]) {
	if task == nil {
		return
	}
	if s.stopped.Load() {
		return
	}
	// 未显式指定重试时，套用默认重试策略。
	if task.retries == 0 && s.retries > 0 {
		task.retries = s.retries
		task.delay = s.delay
	}
	select {
	case s.addCh <- addReq[P]{task: task}:
	case <-s.stopCh:
		return
	}
}

// Update 修改已有任务的执行时间（保持原回调）。任务不存在返回 ErrNotFound，
// 调度器已停止返回 ErrStopped。对已下沉到存储的远任务同样生效。
func (s *Scheduler[P]) Update(id uint64, name string, t time.Time, p P) {
	s.sendUpd(updReq[P]{id: id, name: name, expire: t.UnixMilli(), param: p})
}

// Get 查询任务快照（只读，返回的 *Task 不可改）。任务不存在返回 ErrNotFound，
// 调度器已停止返回 ErrStopped。已下沉到存储的远任务同样可查询。
func (s *Scheduler[P]) Get(id uint64) (*Task[P], bool) {
	resp := make(chan getResp[P], 1)
	if !s.sendGet(getReq[P]{id: id, resp: resp}) {
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

// TTL 返回剩余时间；任务不存在返回 ErrNotFound，已到期或已停止返回相应错误。
func (s *Scheduler[P]) TTL(id uint64) (time.Duration, bool) {
	task, ok := s.Get(id)
	if !ok {
		return 0, ok
	}
	ttl := time.Until(time.Unix(0, task.expire))
	if ttl <= 0 {
		return 0, false
	}
	return ttl, true
}

// Cancel 取消任务。任务不存在返回 ErrNotFound，调度器已停止返回 ErrStopped。
// 已进入执行队列的任务会在真正运行前再次检查取消标记（尽力而为）。
func (s *Scheduler[P]) Cancel(id uint64) {
	s.sendCancel(cancelReq{id: id})
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

func (s *Scheduler[P]) get(id uint64) (*Task[P], bool) {
	if b, ok := s.index[id]; ok {
		if t, ok := b.tasks[id]; ok {
			return t, true
		}
	}
	return nil, false
}

func (s *Scheduler[P]) update(id uint64, name string, expire int64, param P) {
	now := htime.NowTime()

	t, ok := s.get(id)
	if s.loader == nil {
		if !ok {
			return
		}
	} else {
		if expire > now.Add(s.ahead).UnixMilli() {
			if ok {
				s.cancel(id)
			}
			return
		}
	}
	task := &Task[P]{
		id:     id,
		expire: expire,
		param:  param,
	}

	if ok {
		task.retries = t.retries
		task.delay = t.delay
		task.name = t.name
	} else {
		task.retries = s.retries
		task.delay = s.delay
		task.name = name
	}

	s.add(task, now)
}

// add 负责覆盖旧任务并把新任务放到合适的层级。
func (s *Scheduler[P]) add(task *Task[P], now time.Time) {
	if s.loader != nil && task.expire > now.Add(s.ahead).UnixMilli() {
		return
	}
	if b, ok := s.index[task.id]; ok {
		if old, ok := b.tasks[task.id]; ok {
			old.canceled.Store(true)
			delete(b.tasks, task.id)
		}
		delete(s.index, task.id)
	}
	s.place(task, now)
}

// cancel 取消任务
func (s *Scheduler[P]) cancel(id uint64) {
	b, ok := s.index[id]
	if !ok {
		return
	}
	t, hit := b.tasks[id]
	if hit {
		t.canceled.Store(true)
		delete(b.tasks, id)
	}
	delete(s.index, id)
	return
}

// place 把任务放进「能装下它的最细一层」；已经到期则立即提交执行。
func (s *Scheduler[P]) place(task *Task[P], now time.Time) {
	delay := task.expire - now.UnixMilli()
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
func (s *Scheduler[P]) advance(i int, now time.Time) {
	lv := s.levels[i]
	b := lv.buckets[lv.cursor]
	// 先换上新桶，保证重新入桶（place）不会写回正在遍历的 map。
	lv.buckets[lv.cursor] = newBucket[P]()

	for id, task := range b.tasks {
		delete(s.index, id)

		if task.canceled.Load() {
			continue
		}
		if task.expire <= now.UnixMilli() {
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
		if task.canceled.Load() {
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

		ctx := hlog.NewContext()
		// handler 返回 error 等价于失败，触发重试。
		if err := s.handler(ctx, id, task.name, execTime, task.retries, task.param); err != nil {
			hlog.Error(ctx, "task handler error: %v", err)
			failed = true
			s.onTaskPanic(task, id)
		}
	}()
	// 执行成功（未失败）时，通知驱动协程清理持久化记录。
	if !failed {
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
	s.deleteLoaded(id)
}

// deleteLoaded 移除 loaded 去重记录，避免重启后误判为「已加载」。
func (s *Scheduler[P]) deleteLoaded(id uint64) {
	delete(s.loaded, id)
}

// onTaskPanic 在回调 panic 后决定重试或放弃。
func (s *Scheduler[P]) onTaskPanic(task *Task[P], id uint64) {
	if task.canceled.Load() {
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
			expire:  htime.NowTime().Add(delay).UnixMilli(),
			retries: task.retries,
			delay:   task.delay,
			param:   task.param,
			name:    task.name,
		}
		// 重新入轮；调度器停止时直接放弃。
		select {
		case s.addCh <- addReq[P]{task: next}:
		default:
			s.notifyFinish(id)
		}
		return
	}
	s.notifyFinish(id)
}

// loadTasks 从存储层加载待执行任务（重启恢复 + 周期性轮询）。
func (s *Scheduler[P]) loadTasks() {
	if s.loader == nil {
		return
	}
	now := htime.NowTime()
	s.pruneLoaded(now.UnixMilli())

	ctx := hlog.NewContext()
	recs, err := s.loader(ctx, now.Add(s.ahead), s.batch)
	if err != nil {
		hlog.Exit(ctx, "load tasks error: %v", err)
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
func (s *Scheduler[P]) load(rec *LoaderTask[P]) {
	expire := rec.expire.UnixMilli()
	// 去重：已加载且到期时间未变的记录不再重复入轮。
	if prev, ok := s.loaded[rec.id]; ok && prev == expire {
		return
	}

	s.loaded[rec.id] = expire
	s.add(&Task[P]{id: rec.id,
		expire: expire,
		param:  rec.param,
		name:   rec.name,
	}, htime.NowTime())
}
