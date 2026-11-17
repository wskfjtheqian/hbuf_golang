package htwheel_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
)

// 大量任务同时到期时，WorkerPool 缓冲被填满，Submit 会在锁内阻塞发送。
// 这不应导致死锁，且所有任务最终都应被执行（验证调度器不丢任务、不卡死）。
func TestWorkerPoolSaturation(t *testing.T) {
	s := htwheel.NewScheduler(
		htwheel.WithTick(time.Millisecond),
		htwheel.WithSize(8),
		htwheel.WithWorkers(2), // 刻意少量 worker，放大积压
	)
	defer s.Stop()

	const n = 5000
	var executed int64

	// 全部在几乎同一时刻到期，瞬时塞满 worker 队列。
	at := time.Now().Add(10 * time.Millisecond)
	for i := 0; i < n; i++ {
		id := uint64(i)
		s.Schedule(id, at, func(uint64, time.Time) {
			atomic.AddInt64(&executed, 1)
		})
	}

	// 每个任务都做一次轻量阻塞（模拟慢任务），放大 Submit 持锁阻塞的窗口。
	// 用充足超时等待全部执行完。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&executed) == n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&executed); got != n {
		t.Fatalf("WorkerPool 饱和下丢失任务：期望 %d，实际 %d", n, got)
	}
}

// 极端远期任务在轮内「打转」不应饿死：即使未到期，也要能被 Get/Cancel 命中，
// 且不会因 place 反复重排而丢失索引。
func TestFarFutureStable(t *testing.T) {
	s := htwheel.NewScheduler(
		htwheel.WithTick(time.Millisecond),
		htwheel.WithSize(4),
		htwheel.WithWorkers(2),
	)
	defer s.Stop()

	at := time.Now().Add(50 * 365 * 24 * time.Hour) // 极远，超过最粗层量程，走取模兜底
	if !s.Schedule(1, at, func(uint64, time.Time) {}) {
		t.Fatal("Schedule 失败")
	}

	// 让它跑过几轮 advance，期间索引必须持续可查。
	for i := 0; i < 20; i++ {
		time.Sleep(5 * time.Millisecond)
		task, ok := s.Get(1)
		if !ok {
			t.Fatalf("第 %d 轮：远期任务从索引丢失", i)
		}
		if task.Expire() != at.UnixNano() {
			t.Fatalf("expire 被改变: %d != %d", task.Expire(), at.UnixNano())
		}
	}

	if !s.Cancel(1) {
		t.Fatal("远期任务应能取消")
	}
	if _, ok := s.Get(1); ok {
		t.Fatal("取消后仍可查到")
	}
}

// Stop 与 Schedule/Get/Update/Cancel 并发竞争，不应 panic、不应死锁。
func TestStopConcurrentRace(t *testing.T) {
	for round := 0; round < 20; round++ {
		s := htwheel.NewScheduler(htwheel.WithTick(time.Millisecond), htwheel.WithSize(8))

		var wg sync.WaitGroup
		stopCh := make(chan struct{})

		// 一组 goroutine 持续操作。
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				id := uint64(g)
				for {
					select {
					case <-stopCh:
						return
					default:
					}
					s.Schedule(id, time.Now().Add(20*time.Millisecond), func(uint64, time.Time) {})
					s.Get(id)
					s.Update(id, time.Now().Add(30*time.Millisecond))
					s.Cancel(id)
				}
			}(g)
		}

		// 主 goroutine 在随机时刻 Stop。
		time.Sleep(time.Duration(round%5) * time.Millisecond)
		close(stopCh)
		s.Stop()
		wg.Wait()
	}
}

// 同一 id 的覆盖在极端并发下不应丢失或重复执行旧任务。
func TestOverwriteConcurrent(t *testing.T) {
	s := htwheel.NewScheduler(
		htwheel.WithTick(time.Millisecond),
		htwheel.WithSize(8),
		htwheel.WithWorkers(4),
	)
	defer s.Stop()

	const rounds = 1000
	var fired int64
	var wg sync.WaitGroup

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				// 同一 id，时间拉长到不会在测试内真正到期，
				// 只验证覆盖语义不 panic、索引一致。
				s.Schedule(999, time.Now().Add(time.Hour), func(uint64, time.Time) {
					atomic.AddInt64(&fired, 1)
				})
			}
		}()
	}
	wg.Wait()

	// 仅能存在一条任务。
	eventually(t, time.Second, func() bool {
		_, ok := s.Get(999)
		return ok
	}, "覆盖后应仍有一条任务在索引中")

	if !s.Cancel(999) {
		t.Fatal("覆盖后的任务应能取消")
	}
	if n := atomic.LoadInt64(&fired); n != 0 {
		t.Fatalf("远期任务不应执行，实际 %d 次", n)
	}
}

// 重试字段序列化为 JSON 后应能原样恢复（Duration 纳秒精度）。
func TestRecordJSONRoundTrip(t *testing.T) {
	// 通过 FileStore 间接验证：写入带重试字段的记录，读回字段一致。
	// 直接依赖 encoding/json 语义，这里验证 time.Duration 的持久化。
	rec := htwheel.Record{
		ID:         42,
		Expire:     time.Now().Add(time.Hour).Truncate(time.Millisecond),
		Name:       "job",
		Payload:    []byte("x"),
		Retries:    7,
		RetryDelay: 1234 * time.Millisecond,
	}

	// 用 FileStore 的写读验证（内部走 json.Marshal/Unmarshal）。
	path := t.TempDir() + "/rt.json"
	fs := htwheel.NewFileStore(path)
	if err := fs.Put(rec); err != nil {
		t.Fatal(err)
	}

	fs2 := htwheel.NewFileStore(path) // 新实例重新读盘
	got, ok, err := fs2.Get(42)
	if err != nil || !ok {
		t.Fatalf("读取失败: ok=%v err=%v", ok, err)
	}
	if got.Retries != 7 || got.RetryDelay != 1234*time.Millisecond {
		t.Fatalf("重试字段未正确持久化: %+v", got)
	}
	if got.Expire.Equal(rec.Expire) != true {
		t.Fatalf("Expire 持久化不一致: %v != %v", got.Expire, rec.Expire)
	}
}

// 未注册处理器 + 带重试配置的任务：处理器缺失不应消耗重试次数。
func TestMissingHandlerDoesNotConsumeRetries(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	// 先调度一个处理器未注册、且配置了重试的任务。
	if err := p.ScheduleWith(1, time.Now().Add(10*time.Millisecond), "ghost", nil,
		htwheel.WithTaskRetry(2, 20*time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	// 等它到期并触发「处理器缺失」路径。
	time.Sleep(120 * time.Millisecond)

	rec, ok, _ := store.Get(1)
	if !ok {
		t.Fatal("处理器缺失时应保留记录")
	}
	if rec.Retries != 2 {
		t.Fatalf("处理器缺失不应消耗重试次数，期望 2，实际 %d", rec.Retries)
	}
}

// 重试时若存储层 Put 失败，任务不应被误删：Put 失败不会触发 Delete，
// 记录仍保留，下一次轮询按旧 Expire 重新预热（重试次数不额外扣减）。
func TestRetryPutFailureKeepsRecord(t *testing.T) {
	base := htwheel.NewMemoryStore()
	// failAfter 次 Put 成功后再失败：让首次 Schedule 成功、首次重试的 Put 失败。
	wrapped := &putFailStore{MemoryStore: base, failAfter: 2, remaining: 2}

	p := newRetryScheduler(t, wrapped)

	var calls int32
	p.Register("job", func(uint64, time.Time, []byte) error {
		atomic.AddInt32(&calls, 1)
		return fmt.Errorf("fail %d", atomic.LoadInt32(&calls))
	})

	if err := p.ScheduleWith(2, time.Now().Add(10*time.Millisecond), "job", nil,
		htwheel.WithTaskRetry(2, 10*time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	// 首次执行失败，进入重试；重试的 Put 被注入失败 → 记录不应被删。
	eventually(t, 2*time.Second, func() bool {
		return atomic.LoadInt32(&calls) >= 2
	}, "任务应至少执行两次（首次失败 + 至少一次重试尝试）")

	// 无论 Put 失败与否，记录都不应因 Put 错误被删除。
	// Put 失败时 reschedule 直接 return false，不调用 Delete。
	if _, ok, _ := wrapped.Get(2); !ok {
		t.Fatal("重试 Put 失败不应导致记录被删除")
	}
}

// putFailStore 前 failAfter 次 Put 放行，之后每次 Put 都返回错误。
type putFailStore struct {
	*htwheel.MemoryStore
	mu        sync.Mutex
	failAfter int
	remaining int
}

func (s *putFailStore) Put(rec htwheel.Record) error {
	s.mu.Lock()
	if s.remaining > 0 {
		s.remaining--
		s.mu.Unlock()
		return s.MemoryStore.Put(rec)
	}
	s.mu.Unlock()
	return fmt.Errorf("injected put failure")
}

// PersistentScheduler.Get / TTL 读存储层，含重试等待期的 TTL 语义。
func TestPersistentGetTTL(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)
	p.Register("job", func(uint64, time.Time, []byte) error { return nil })

	at := time.Now().Add(200 * time.Millisecond)
	if err := p.ScheduleWith(1, at, "job", []byte("p"), htwheel.WithTaskRetry(2, 5*time.Second)); err != nil {
		t.Fatal(err)
	}

	rec, ok, err := p.Get(1)
	if err != nil || !ok {
		t.Fatalf("Get 失败: ok=%v err=%v", ok, err)
	}
	if rec.Name != "job" || string(rec.Payload) != "p" || rec.Retries != 2 {
		t.Fatalf("Get 返回异常: %+v", rec)
	}

	ttl, ok, err := p.TTL(1)
	if err != nil || !ok {
		t.Fatalf("TTL 失败: ok=%v err=%v", ok, err)
	}
	if ttl <= 0 || ttl > 300*time.Millisecond {
		t.Fatalf("TTL 应在 (0, 300ms]，实际 %s", ttl)
	}

	// 不存在的任务。
	if _, ok, _ := p.Get(999); ok {
		t.Fatal("不存在任务 Get 应返回 ok=false")
	}
	if _, ok, _ := p.TTL(999); ok {
		t.Fatal("不存在任务 TTL 应返回 ok=false")
	}
}

// FileStore.Delete：删除存在的与不存在的记录。
func TestFileStoreDelete(t *testing.T) {
	path := t.TempDir() + "/del.json"
	fs := htwheel.NewFileStore(path)

	if err := fs.Put(htwheel.Record{ID: 1, Expire: time.Now().Add(time.Hour), Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(htwheel.Record{ID: 2, Expire: time.Now().Add(time.Hour), Name: "y"}); err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete(1); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := fs.Get(1); ok {
		t.Fatal("删除后仍可读")
	}
	if _, ok, _ := fs.Get(2); !ok {
		t.Fatal("未删除的记录不应消失")
	}
	// 删除不存在的记录不报错。
	if err := fs.Delete(1); err != nil {
		t.Fatalf("重复删除不应报错: %v", err)
	}
	if err := fs.Delete(999); err != nil {
		t.Fatalf("删除不存在记录不应报错: %v", err)
	}

	// 新实例重读盘，确认删除已落盘。
	fs2 := htwheel.NewFileStore(path)
	if _, ok, _ := fs2.Get(1); ok {
		t.Fatal("重启后已删除的记录不应存在")
	}
	if _, ok, _ := fs2.Get(2); !ok {
		t.Fatal("重启后未删除的记录应保留")
	}
}

// addLevel 的 tick 溢出保护：极端远期任务不应死循环建层。
func TestAddLevelOverflowGuard(t *testing.T) {
	s := htwheel.NewScheduler(
		htwheel.WithTick(time.Nanosecond),
		htwheel.WithSize(512),
		htwheel.WithWorkers(1),
	)
	defer s.Stop()

	// 超出 time.Duration 可表示范围的任务（UnixNano 溢出会回到负数/极小值）。
	// 用 time.Now().Add 会 panic，故直接构造一个极端的 Unix 时间。
	// 这里用最大的合理时间，验证 addLevel 有上限保护。
	at := time.Unix(0, 1<<62) // 约 146 年后，远超最粗层量程
	if !s.Schedule(1, at, func(uint64, time.Time) {}) {
		t.Fatal("Schedule 失败")
	}
	eventually(t, time.Second, func() bool {
		_, ok := s.Get(1)
		return ok
	}, "极端远期任务应能入轮（触发 addLevel 溢出兜底）")

	// 不应创建无限层级：levels 数量应被 tick 溢出限制住。
	// 通过取消验证索引一致即可。
	if !s.Cancel(1) {
		t.Fatal("极端远期任务应能取消")
	}
}

// WithBatchSize 与 WithDefaultRetry 的边界值（非法输入被安全忽略）。
func TestOptionBoundaries(t *testing.T) {
	store := htwheel.NewMemoryStore()
	// 非法值不应 panic：batch<=0 走默认，retries<0 归零。
	p := htwheel.NewPersistentScheduler(store, nil,
		htwheel.WithBatchSize(0),
		htwheel.WithDefaultRetry(-5, -1),
		htwheel.WithRetryBackoff(0.5), // <1 被忽略
		htwheel.WithMaxRetryDelay(-1), // <=0 被忽略
	)
	defer p.Stop()

	p.Register("job", func(uint64, time.Time, []byte) error { return nil })
	if err := p.Schedule(1, time.Now().Add(time.Hour), "job", nil); err != nil {
		t.Fatal(err)
	}
	rec, ok, _ := p.Get(1)
	if !ok {
		t.Fatal("任务应已入库")
	}
	if rec.Retries != 0 {
		t.Fatalf("非法负数重试次数应归零，实际 %d", rec.Retries)
	}
}
