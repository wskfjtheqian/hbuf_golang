package htwheel_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
)

func newTestScheduler(t *testing.T, opts ...htwheel.Option) *htwheel.Scheduler {
	t.Helper()
	s := htwheel.NewScheduler(opts...)
	t.Cleanup(s.Stop)
	return s
}

// eventually 轮询等待异步入轮等最终一致的状态。
func eventually(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

func waitSignal(t *testing.T, ch <-chan time.Time, timeout time.Duration, msg string) time.Time {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatal(msg)
		return time.Time{}
	}
}

// 正常触发，且回调拿到的是计划执行时间而非当前时间。
func TestScheduleExecutes(t *testing.T) {
	s := newTestScheduler(t, htwheel.WithTick(10*time.Millisecond), htwheel.WithSize(16), htwheel.WithWorkers(4))

	at := time.Now().Add(40 * time.Millisecond)
	got := make(chan time.Time, 1)
	if !s.Schedule(1, at, func(id uint64, exec time.Time) {
		got <- exec
	}) {
		t.Fatal("Schedule 返回 false")
	}

	exec := waitSignal(t, got, 2*time.Second, "任务未在超时内执行")
	if !exec.Equal(at) {
		t.Fatalf("执行时间参数应为计划时间 %v，实际 %v", at, exec)
	}
	if _, ok := s.Get(1); ok {
		t.Fatal("任务执行后仍留在索引中")
	}
}

// 长延时会落入上层时间轮，Get / TTL / Update 必须仍然可见。
func TestLongDelayCrossLevel(t *testing.T) {
	s := newTestScheduler(t, htwheel.WithTick(10*time.Millisecond), htwheel.WithSize(16), htwheel.WithWorkers(4))

	at := time.Now().Add(3 * time.Second) // level0 量程仅 160ms，必然进入上层
	if !s.Schedule(7, at, func(uint64, time.Time) {}) {
		t.Fatal("Schedule 失败")
	}

	eventually(t, 500*time.Millisecond, func() bool {
		_, ok := s.Get(7)
		return ok
	}, "长延时任务应可被 Get 查到")

	task, _ := s.Get(7)
	if task.Expire() != at.UnixNano() {
		t.Fatalf("expire 不符: %d != %d", task.Expire(), at.UnixNano())
	}

	ttl, ok := s.TTL(7)
	if !ok || ttl <= 2*time.Second {
		t.Fatalf("TTL 异常: %v, %v", ttl, ok)
	}

	// 把长任务提前到 50ms 后，应能正常执行（跨层更新）。
	if !s.Update(7, time.Now().Add(50*time.Millisecond)) {
		t.Fatal("跨层 Update 应成功")
	}
	eventually(t, time.Second, func() bool {
		_, ok := s.Get(7)
		return !ok
	}, "任务执行后应从索引移除")
}

// 位于上层时间轮的任务也必须能被取消。
func TestCancelCrossLevel(t *testing.T) {
	s := newTestScheduler(t, htwheel.WithTick(10*time.Millisecond), htwheel.WithSize(16), htwheel.WithWorkers(4))

	var fired int32
	if !s.Schedule(9, time.Now().Add(300*time.Millisecond), func(uint64, time.Time) {
		atomic.AddInt32(&fired, 1)
	}) {
		t.Fatal("Schedule 失败")
	}
	eventually(t, 500*time.Millisecond, func() bool {
		_, ok := s.Get(9)
		return ok
	}, "任务应已入轮")

	if !s.Cancel(9) {
		t.Fatal("Cancel 应命中任务")
	}
	if _, ok := s.Get(9); ok {
		t.Fatal("取消后仍可查到任务")
	}

	time.Sleep(500 * time.Millisecond)
	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Fatalf("取消后仍执行了 %d 次", n)
	}
}

// 同 id 覆盖：旧任务被丢弃，只执行最后一次。
func TestOverwrite(t *testing.T) {
	s := newTestScheduler(t, htwheel.WithTick(10*time.Millisecond), htwheel.WithSize(16), htwheel.WithWorkers(4))

	var oldFired, newFired int32
	s.Schedule(1, time.Now().Add(120*time.Millisecond), func(uint64, time.Time) {
		atomic.AddInt32(&oldFired, 1)
	})
	s.Schedule(1, time.Now().Add(120*time.Millisecond), func(uint64, time.Time) {
		atomic.AddInt32(&newFired, 1)
	})

	time.Sleep(400 * time.Millisecond)
	if o, n := atomic.LoadInt32(&oldFired), atomic.LoadInt32(&newFired); o != 0 || n != 1 {
		t.Fatalf("覆盖语义错误，期望旧任务 0 次、新任务 1 次，实际 %d / %d", o, n)
	}
}

// 取消 + 更新混用，验证索引一致。
func TestCancelThenUpdate(t *testing.T) {
	s := newTestScheduler(t, htwheel.WithTick(10*time.Millisecond), htwheel.WithSize(16))

	s.Schedule(3, time.Now().Add(100*time.Millisecond), func(uint64, time.Time) {})
	eventually(t, 500*time.Millisecond, func() bool {
		_, ok := s.Get(3)
		return ok
	}, "任务应已入轮")

	if !s.Cancel(3) {
		t.Fatal("Cancel 应命中")
	}
	if s.Update(3, time.Now().Add(50*time.Millisecond)) {
		t.Fatal("已取消的任务不应能 Update")
	}
	if _, ok := s.Get(3); ok {
		t.Fatal("已取消任务仍可查到")
	}
}

// 并发压测：配合 -race 使用，验证不存在并发 map 读写与生命周期竞态。
func TestConcurrentAccess(t *testing.T) {
	s := newTestScheduler(t,
		htwheel.WithTick(5*time.Millisecond),
		htwheel.WithSize(32),
		htwheel.WithWorkers(8),
	)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			base := uint64(g) * 100000
			for i := 0; i < 200; i++ {
				id := base + uint64(i)
				s.Schedule(id, time.Now().Add(time.Duration(i%50)*time.Millisecond), func(uint64, time.Time) {})
				s.Get(id)
				s.TTL(id)
				s.Update(id, time.Now().Add(time.Duration(i%30+1)*time.Millisecond))
				s.Cancel(id)
			}
		}(g)
	}
	wg.Wait()
}

// Stop 可重复调用，且停止后 API 返回 false。
func TestStopIdempotent(t *testing.T) {
	s := htwheel.NewScheduler(htwheel.WithTick(10*time.Millisecond), htwheel.WithSize(16))
	s.Stop()
	s.Stop()

	if s.Schedule(1, time.Now().Add(time.Millisecond), func(uint64, time.Time) {}) {
		t.Fatal("停止后 Schedule 应返回 false")
	}
	if s.Cancel(1) {
		t.Fatal("停止后 Cancel 应返回 false")
	}
	if _, ok := s.Get(1); ok {
		t.Fatal("停止后 Get 应返回 false")
	}
}

// 未调 Schedule 的 Update 返回 false。
func TestUpdateMissing(t *testing.T) {
	s := newTestScheduler(t)
	if s.Update(42, time.Now().Add(time.Second)) {
		t.Fatal("不存在的任务 Update 应返回 false")
	}
}

// 极端远期任务不应导致建层死循环；WithSize(1) 会被安全忽略。
func TestFarFutureNoHang(t *testing.T) {
	s := newTestScheduler(t,
		htwheel.WithTick(time.Millisecond),
		htwheel.WithSize(1), // 非法值，应被忽略并使用默认值
		htwheel.WithWorkers(1),
	)

	at := time.Now().Add(100 * 365 * 24 * time.Hour)
	if !s.Schedule(1, at, func(uint64, time.Time) {}) {
		t.Fatal("Schedule 失败")
	}
	eventually(t, time.Second, func() bool {
		_, ok := s.Get(1)
		return ok
	}, "远期任务应能入轮")
}

// size=2 时层级会快速倍增，验证多层安置与取消。
func TestManyLevels(t *testing.T) {
	s := newTestScheduler(t,
		htwheel.WithTick(time.Millisecond),
		htwheel.WithSize(2),
		htwheel.WithWorkers(1),
	)

	at := time.Now().Add(24 * time.Hour)
	if !s.Schedule(1, at, func(uint64, time.Time) {}) {
		t.Fatal("Schedule 失败")
	}
	eventually(t, time.Second, func() bool {
		_, ok := s.Get(1)
		return ok
	}, "24h 任务应能入轮")
	if !s.Cancel(1) {
		t.Fatal("多层任务应能取消")
	}
}
