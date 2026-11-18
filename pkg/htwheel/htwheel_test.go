package htwheel

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// recoverable 是实现了 Recoverable 的参数类型，用于测试存储恢复。
type recoverable struct {
	id     uint64
	expire time.Time
	fired  *atomic.Int32
	name   string
}

func (r recoverable) ID() uint64        { return r.id }
func (r recoverable) Expire() time.Time { return r.expire }
func (r recoverable) Name() string      { return r.name }

// handler 根据 param 触发计数，用于测试。
func countingHandler(id uint64, name string, execTime time.Time, p recoverable) error {
	if p.fired != nil {
		p.fired.Add(1)
	}
	return nil
}

func TestScheduleAndFire(t *testing.T) {
	var n atomic.Int32
	s := NewScheduler[recoverable](countingHandler, WithTick[recoverable](10*time.Millisecond))
	defer s.Stop()

	if err := s.Schedule(1, "task", time.Now().Add(50*time.Millisecond), recoverable{fired: &n}); err != nil {
		t.Fatalf("schedule should succeed, got %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && n.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if n.Load() != 1 {
		t.Fatalf("expected 1 execution, got %d", n.Load())
	}
}

func TestScheduleOverwrite(t *testing.T) {
	var a, b atomic.Int32
	s := NewScheduler[recoverable](
		func(id uint64, name string, execTime time.Time, p recoverable) error {
			// 依据 name 区分新旧任务。
			if name == "old" {
				a.Add(1)
			} else {
				b.Add(1)
			}
			return nil
		},
		WithTick[recoverable](10*time.Millisecond),
	)
	defer s.Stop()

	s.Schedule(1, "old", time.Now().Add(50*time.Millisecond), recoverable{})
	s.Schedule(1, "new", time.Now().Add(50*time.Millisecond), recoverable{})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && b.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if a.Load() != 0 {
		t.Fatalf("old task should not fire, fired %d times", a.Load())
	}
	if b.Load() != 1 {
		t.Fatalf("new task should fire once, fired %d times", b.Load())
	}
}

func TestCancel(t *testing.T) {
	var n atomic.Int32
	s := NewScheduler[recoverable](countingHandler, WithTick[recoverable](10*time.Millisecond))
	defer s.Stop()

	s.Schedule(1, "task", time.Now().Add(30*time.Millisecond), recoverable{fired: &n})

	// Schedule 是异步的，先等任务入轮（可查询）再取消，避免竞态。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Get(1); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := s.Cancel(1); err != nil {
		t.Fatalf("cancel should hit, got %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatalf("canceled task should not fire, fired %d times", n.Load())
	}
}

func TestGetTTL(t *testing.T) {
	s := NewScheduler[recoverable](countingHandler, WithTick[recoverable](10*time.Millisecond))
	defer s.Stop()

	s.Schedule(1, "task", time.Now().Add(time.Second), recoverable{})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Get(1); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	ttl, err := s.TTL(1)
	if err != nil || ttl <= 0 || ttl > time.Second {
		t.Fatalf("unexpected ttl: %v err=%v", ttl, err)
	}
}

func TestRetryOnError(t *testing.T) {
	var errs atomic.Int32
	var n atomic.Int32
	s := NewScheduler[recoverable](
		func(id uint64, name string, execTime time.Time, p recoverable) error {
			if n.Add(1) < 2 {
				return errors.New("boom")
			}
			return nil
		},
		WithTick[recoverable](10*time.Millisecond),
		WithErrorHandler[recoverable](func(err error) { errs.Add(1) }),
	)
	defer s.Stop()

	task := &Task[recoverable]{
		id:     1,
		expire: time.Now().Add(20 * time.Millisecond).UnixNano(),
	}
	task.WithTaskRetry(3, 10*time.Millisecond)
	if err := s.ScheduleTask(task); err != nil {
		t.Fatalf("schedule task should succeed, got %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && n.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if n.Load() != 2 {
		t.Fatalf("expected 2 invocations (1 error + 1 retry), got %d", n.Load())
	}
}

func TestRetryExhausted(t *testing.T) {
	var gotErr error
	s := NewScheduler[recoverable](
		func(id uint64, name string, execTime time.Time, p recoverable) error {
			return errors.New("always")
		},
		WithTick[recoverable](10*time.Millisecond),
		WithErrorHandler[recoverable](func(err error) { gotErr = err }),
	)
	defer s.Stop()

	task := &Task[recoverable]{
		id:     1,
		expire: time.Now().Add(20 * time.Millisecond).UnixNano(),
	}
	task.WithTaskRetry(1, 10*time.Millisecond)
	s.ScheduleTask(task)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && gotErr == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if !errors.Is(gotErr, ErrRetriesExhausted) {
		t.Fatalf("expected ErrRetriesExhausted, got %v", gotErr)
	}
}

func TestScheduleTaskNil(t *testing.T) {
	s := NewScheduler[recoverable](countingHandler, WithTick[recoverable](10*time.Millisecond))
	defer s.Stop()

	if err := s.ScheduleTask(nil); !errors.Is(err, ErrNilTask) {
		t.Fatalf("expected ErrNilTask, got %v", err)
	}
}

// memStore 是内存实现的 Store（值类型 P），用于测试。
type memStore struct {
	items map[uint64]recoverable
}

// errStoreNotFound 表示存储中不存在该记录。
var errStoreNotFound = errors.New("store: not found")

func newMemStore() *memStore { return &memStore{items: map[uint64]recoverable{}} }
func (m *memStore) Add(id uint64, t time.Time, task recoverable) error {
	task.expire = t
	m.items[id] = task
	return nil
}
func (m *memStore) Remove(id uint64) error {
	if _, ok := m.items[id]; !ok {
		return errStoreNotFound
	}
	delete(m.items, id)
	return nil
}
func (m *memStore) Get(id uint64) (recoverable, time.Time, error) {
	r, ok := m.items[id]
	if !ok {
		return recoverable{}, time.Time{}, errStoreNotFound
	}
	return r, r.expire, nil
}
func (m *memStore) Update(id uint64, t time.Time, task recoverable) error {
	if _, ok := m.items[id]; !ok {
		return errStoreNotFound
	}
	task.expire = t
	m.items[id] = task
	return nil
}
func (m *memStore) Load(before time.Time, limit int) ([]recoverable, error) {
	var out []recoverable
	for _, r := range m.items {
		if r.expire.Before(before) {
			out = append(out, r)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func TestStoreRecovery(t *testing.T) {
	var fired atomic.Int32
	st := newMemStore()
	st.Add(1, time.Now().Add(30*time.Millisecond), recoverable{id: 1, fired: &fired})

	s := NewScheduler[recoverable](
		countingHandler,
		WithTick[recoverable](10*time.Millisecond),
		WithStore[recoverable](st),
		WithPollInterval[recoverable](20*time.Millisecond),
	)
	defer s.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("expected recovered task to fire once, fired %d times", fired.Load())
	}
}

func TestSchedulePersists(t *testing.T) {
	var fired atomic.Int32
	st := newMemStore()

	s := NewScheduler[recoverable](
		countingHandler,
		WithTick[recoverable](10*time.Millisecond),
		WithStore[recoverable](st),
	)
	defer s.Stop()

	p := recoverable{id: 1, fired: &fired}
	if err := s.Schedule(1, "task", time.Now().Add(40*time.Millisecond), p); err != nil {
		t.Fatalf("schedule should succeed, got %v", err)
	}

	// 等任务入轮（持久化 Add 被调用）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := st.Get(1); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, _, err := st.Get(1); err != nil {
		t.Fatal("expected store.Add to be called after schedule")
	}

	// 等任务执行完成（持久化 Remove 被调用）。
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		if _, _, err := st.Get(1); err != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, _, err := st.Get(1); err == nil {
		t.Fatal("expected store.Remove to be called after execution completes")
	}
}

func TestCancelPersistedTaskRemoves(t *testing.T) {
	st := newMemStore()
	s := NewScheduler[recoverable](
		countingHandler,
		WithTick[recoverable](10*time.Millisecond),
		WithStore[recoverable](st),
	)
	defer s.Stop()

	s.Schedule(1, "task", time.Now().Add(time.Second), recoverable{id: 1})

	// 等入轮（Add 生效）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := st.Get(1); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := s.Cancel(1); err != nil {
		t.Fatalf("cancel should hit, got %v", err)
	}
	if _, _, err := st.Get(1); err == nil {
		t.Fatal("expected store.Remove to be called after cancel")
	}
}

// TestFarTaskSinksToStore 验证：设置了存储层时，远任务（expire-now > ahead）
// 只写存储、不入内存时间轮，临近到期时由 loadTasks 拉回并执行。
func TestFarTaskSinksToStore(t *testing.T) {
	var fired atomic.Int32
	st := newMemStore()

	s := NewScheduler[recoverable](
		countingHandler,
		WithTick[recoverable](10*time.Millisecond),
		WithStore[recoverable](st),
		WithLookahead[recoverable](50*time.Millisecond),    // ahead=50ms
		WithPollInterval[recoverable](20*time.Millisecond), // 轮询 20ms
	)
	defer s.Stop()

	// 到期时间 250ms 后，远超 ahead(50ms) → 应下沉到存储。
	p := recoverable{id: 1, fired: &fired}
	if err := s.Schedule(1, "task", time.Now().Add(250*time.Millisecond), p); err != nil {
		t.Fatalf("schedule should succeed, got %v", err)
	}

	// 等 add 处理完。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := st.Get(1); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 1) 存储里有（Add 被调用）。
	if _, _, err := st.Get(1); err != nil {
		t.Fatal("far task should be persisted to store")
	}
	// 2) 任务可查询（已下沉到 sunk 索引）。
	if _, err := s.Get(1); err != nil {
		t.Fatalf("far task should be queryable via sunk index, got %v", err)
	}

	// 3) 临近到期时被 loadTasks 拉回并执行。
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("expected far task to be pulled back and fired once, fired %d times", fired.Load())
	}
}

// TestUpdateSunkTask 验证：对已下沉到存储的远任务，Update 仍能命中并更新。
func TestUpdateSunkTask(t *testing.T) {
	var fired atomic.Int32
	st := newMemStore()

	s := NewScheduler[recoverable](
		countingHandler,
		WithTick[recoverable](10*time.Millisecond),
		WithStore[recoverable](st),
		WithLookahead[recoverable](50*time.Millisecond),
		WithPollInterval[recoverable](20*time.Millisecond),
	)
	defer s.Stop()

	// 先下沉一个远任务（300ms 后到期）。
	p := recoverable{id: 1, fired: &fired}
	s.Schedule(1, "task", time.Now().Add(300*time.Millisecond), p)

	// 等 add 处理完、任务下沉。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Get(1); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := s.Get(1); err != nil {
		t.Fatalf("sunk task should be queryable, got %v", err)
	}

	// Update 到很近的未来（30ms，≤ ahead）→ 应拉回内存轮并尽快执行。
	if err := s.Update(1, time.Now().Add(30*time.Millisecond)); err != nil {
		t.Fatalf("update on sunk task should succeed, got %v", err)
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("expected updated task to fire once, fired %d times", fired.Load())
	}
}

// TestUpdateCanceledSunkTask 验证：已取消的下沉任务不应被 Update 复活。
func TestUpdateCanceledSunkTask(t *testing.T) {
	var fired atomic.Int32
	st := newMemStore()

	s := NewScheduler[recoverable](
		countingHandler,
		WithTick[recoverable](10*time.Millisecond),
		WithStore[recoverable](st),
		WithLookahead[recoverable](50*time.Millisecond),
		WithPollInterval[recoverable](20*time.Millisecond),
	)
	defer s.Stop()

	// 下沉一个远任务。
	s.Schedule(1, "task", time.Now().Add(300*time.Millisecond), recoverable{id: 1, fired: &fired})

	// 等下沉完成。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Get(1); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 取消它。
	if err := s.Cancel(1); err != nil {
		t.Fatalf("cancel should hit sunk task, got %v", err)
	}

	// 再 Update：应被拒绝（不复活已取消任务）。
	if err := s.Update(1, time.Now().Add(30*time.Millisecond)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update on canceled sunk task should fail with ErrNotFound, got %v", err)
	}

	// 确认任务确实没有执行。
	time.Sleep(150 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("canceled task should not fire, fired %d times", fired.Load())
	}
}

// TestGetFallsBackToStore 验证：内存中没有的任务（既不在时间轮、也不在下沉索引），
// Scheduler.Get 会回退到 Store.Get 查询（例如重启前遗留、尚未被 Load 拉回的任务）。
func TestGetFallsBackToStore(t *testing.T) {
	var fired atomic.Int32
	st := newMemStore()

	s := NewScheduler[recoverable](
		countingHandler,
		WithTick[recoverable](10*time.Millisecond),
		WithStore[recoverable](st),
		// ahead 设得很小，让远任务永远不会被 Load 拉回内存。
		WithLookahead[recoverable](1*time.Millisecond),
		WithPollInterval[recoverable](50*time.Millisecond),
	)
	defer s.Stop()

	// 直接写入存储（绕过调度器），模拟「重启前遗留、尚未加载」的任务。
	far := time.Now().Add(time.Hour)
	st.Add(7, far, recoverable{id: 7, expire: far, fired: &fired})

	// 等待超过一个 poll 周期，确认任务确实没被拉回内存（ahead 极小）。
	time.Sleep(120 * time.Millisecond)

	// Get 应通过 Store.Get 回退查到它。
	task, err := s.Get(7)
	if err != nil {
		t.Fatalf("Get should fall back to Store and find the task, got %v", err)
	}
	if task.Expire() != far.UnixNano() {
		t.Fatalf("unexpected expire: got %d want %d", task.Expire(), far.UnixNano())
	}
}

// TestGetNotFound 验证：Get 未命中时返回 ErrNotFound。
func TestGetNotFound(t *testing.T) {
	s := NewScheduler[recoverable](countingHandler, WithTick[recoverable](10*time.Millisecond))
	defer s.Stop()

	if _, err := s.Get(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
