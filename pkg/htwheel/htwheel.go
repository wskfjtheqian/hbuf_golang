package htwheel

import (
	"sync"
	"sync/atomic"
	"time"
)

/*
========================
   Task 定义
========================
*/

type Task struct {
	id       uint64
	fn       func()
	expire   int64 // 纳秒时间戳
	canceled int32
}

func (t Task) Expire() int64 {
	return t.expire
}

/*
========================
   Worker Pool
========================
*/

type WorkerPool struct {
	ch chan func()
	wg sync.WaitGroup
}

func NewWorkerPool(size int) *WorkerPool {
	wp := &WorkerPool{
		ch: make(chan func(), 1024),
	}
	for i := 0; i < size; i++ {
		go func() {
			for fn := range wp.ch {
				fn()
				wp.wg.Done()
			}
		}()
	}
	return wp
}

func (wp *WorkerPool) Submit(fn func()) {
	wp.wg.Add(1)
	wp.ch <- fn
}

func (wp *WorkerPool) Stop() {
	wp.wg.Wait()
	close(wp.ch)
}

/*
========================
   bucket
========================
*/

type bucket struct {
	tasks map[uint64]*Task
}

func newBucket() *bucket {
	return &bucket{
		tasks: make(map[uint64]*Task),
	}
}

/*
========================
   TimingWheel
========================
*/

type TimingWheel struct {
	tick   time.Duration
	size   int
	cursor int

	buckets []*bucket

	addCh    chan *Task
	cancelCh chan uint64
	stopCh   chan struct{}

	overflow *TimingWheel

	taskIndex map[uint64]*bucket

	wp *WorkerPool
}

/*
========================
   创建
========================
*/

func NewTimingWheel(tick time.Duration, size int, wp *WorkerPool) *TimingWheel {
	tw := &TimingWheel{
		tick:      tick,
		size:      size,
		addCh:     make(chan *Task, 1024),
		cancelCh:  make(chan uint64, 1024),
		stopCh:    make(chan struct{}),
		taskIndex: make(map[uint64]*bucket),
		wp:        wp,
	}

	tw.buckets = make([]*bucket, size)
	for i := 0; i < size; i++ {
		tw.buckets[i] = newBucket()
	}

	return tw
}

/*
========================
   启动
========================
*/

func (tw *TimingWheel) Start() {
	ticker := time.NewTicker(tw.tick)

	go func() {
		for {
			select {
			case now := <-ticker.C:
				tw.onTick(now.UnixNano())

			case task := <-tw.addCh:
				tw.add(task)

			case id := <-tw.cancelCh:
				tw.cancel(id)

			case <-tw.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

func (tw *TimingWheel) Stop() {
	close(tw.stopCh)
}

/*
========================
   添加（支持覆盖）
========================
*/

func (tw *TimingWheel) add(task *Task) {
	now := time.Now().UnixNano()

	// 覆盖旧任务
	if oldBucket, ok := tw.taskIndex[task.id]; ok {
		if oldTask, ok := oldBucket.tasks[task.id]; ok {
			atomic.StoreInt32(&oldTask.canceled, 1)
			delete(oldBucket.tasks, task.id)
		}
		delete(tw.taskIndex, task.id)
	}

	delay := task.expire - now

	if delay < int64(tw.tick) {
		tw.wp.Submit(task.fn)
		return
	}

	ticks := delay / int64(tw.tick)
	pos := (tw.cursor + int(ticks)) % tw.size

	if ticks >= int64(tw.size) {
		if tw.overflow == nil {
			tw.overflow = NewTimingWheel(
				time.Duration(int64(tw.tick)*int64(tw.size)),
				tw.size,
				tw.wp,
			)
			tw.overflow.Start()
		}
		tw.overflow.add(task)
		return
	}

	b := tw.buckets[pos]
	b.tasks[task.id] = task
	tw.taskIndex[task.id] = b
}

/*
========================
   Tick（含回流）
========================
*/

func (tw *TimingWheel) onTick(now int64) {
	b := tw.buckets[tw.cursor]

	for id, task := range b.tasks {

		if atomic.LoadInt32(&task.canceled) == 1 {
			delete(b.tasks, id)
			delete(tw.taskIndex, id)
			continue
		}

		if task.expire <= now {
			tw.wp.Submit(task.fn)
		} else {
			// 未到期 → 重新分配（关键）
			tw.add(task)
		}

		delete(b.tasks, id)
		delete(tw.taskIndex, id)
	}

	tw.cursor = (tw.cursor + 1) % tw.size

	// 回流
	if tw.cursor == 0 && tw.overflow != nil {
		tw.overflow.drainTo(tw)
	}
}

func (tw *TimingWheel) drainTo(lower *TimingWheel) {
	b := tw.buckets[tw.cursor]

	for _, task := range b.tasks {
		lower.add(task)
	}

	tw.buckets[tw.cursor] = newBucket()
	tw.cursor = (tw.cursor + 1) % tw.size
}

/*
========================
   Cancel
========================
*/

func (tw *TimingWheel) cancel(id uint64) {
	if b, ok := tw.taskIndex[id]; ok {
		if t, ok := b.tasks[id]; ok {
			atomic.StoreInt32(&t.canceled, 1)
			delete(b.tasks, id)
		}
		delete(tw.taskIndex, id)
	}
}

/*
========================
   查询能力（新增核心）
========================
*/

func (tw *TimingWheel) get(id uint64) (*Task, bool) {
	if b, ok := tw.taskIndex[id]; ok {
		t, ok := b.tasks[id]
		return t, ok
	}
	return nil, false
}

/*
========================
   Scheduler
========================
*/

type Scheduler struct {
	tw *TimingWheel
	wp *WorkerPool
}

func NewScheduler() *Scheduler {
	wp := NewWorkerPool(8)
	tw := NewTimingWheel(100*time.Millisecond, 512, wp)
	tw.Start()

	return &Scheduler{tw: tw, wp: wp}
}

func (s *Scheduler) AfterFunc(id uint64, t time.Time, fn func()) {
	task := &Task{
		id:     id,
		fn:     fn,
		expire: t.UnixNano(),
	}
	s.tw.addCh <- task
}

func (s *Scheduler) Cancel(id uint64) {
	s.tw.cancelCh <- id
}

/*
========================
   新增 API
========================
*/

// Update ：只更新 delay，不改 fn
func (s *Scheduler) Update(id uint64, t time.Time) bool {
	task, ok := s.tw.get(id)
	if !ok {
		return false
	}

	newTask := &Task{
		id:     id,
		fn:     task.fn,
		expire: t.UnixNano(),
	}

	s.tw.addCh <- newTask
	return true
}

// Get ：查询任务
func (s *Scheduler) Get(id uint64) (*Task, bool) {
	return s.tw.get(id)
}

// TTL ：剩余时间
func (s *Scheduler) TTL(id uint64) (time.Duration, bool) {
	task, ok := s.tw.get(id)
	if !ok {
		return 0, false
	}

	ttl := time.Until(time.Unix(0, task.expire))
	if ttl < 0 {
		return 0, false
	}
	return ttl, true
}

func (s *Scheduler) Stop() {
	s.tw.Stop()
	s.wp.Stop()
}
