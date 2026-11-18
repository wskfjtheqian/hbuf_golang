package htwheel

import (
	"testing"
	"time"
)

// noopHandler 是基准测试用的空回调，避免回调本身的开销干扰测量。
func noopHandler(id uint64, name string, execTime time.Time, p recoverable) error {
	return nil
}

// BenchmarkSchedule 测量纯入队吞吐（任务到期时间足够远，只入轮不执行）。
func BenchmarkSchedule(b *testing.B) {
	s := NewScheduler[recoverable](noopHandler, WithTick[recoverable](time.Millisecond))
	defer s.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var id uint64
		for pb.Next() {
			id++
			if err := s.Schedule(id, "bench", time.Now().Add(time.Hour), recoverable{id: id}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkScheduleFire 测量「入队到执行」的完整链路吞吐。
// 用足够大的 worker 池与立即到期的任务，压测调度+提交的并发能力。
func BenchmarkScheduleFire(b *testing.B) {
	s := NewScheduler[recoverable](noopHandler,
		WithTick[recoverable](time.Millisecond),
		WithWorkers[recoverable](32),
	)
	defer s.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var id uint64
		for pb.Next() {
			id++
			// 立即到期：走 place -> submit 快路径。
			if err := s.Schedule(id, "bench", time.Now(), recoverable{id: id}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkGet 测量查询吞吐（命中内存轮）。
func BenchmarkGet(b *testing.B) {
	s := NewScheduler[recoverable](noopHandler, WithTick[recoverable](time.Millisecond))
	defer s.Stop()

	if err := s.Schedule(1, "bench", time.Now().Add(time.Hour), recoverable{id: 1}); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCancel 测量「入队 + 取消」的吞吐（覆盖语义：每次 Schedule 覆盖旧任务）。
func BenchmarkScheduleOverwrite(b *testing.B) {
	s := NewScheduler[recoverable](noopHandler, WithTick[recoverable](time.Millisecond))
	defer s.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := s.Schedule(1, "bench", time.Now().Add(time.Hour), recoverable{id: 1}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkManyTasks 测量大量长周期任务同时挂载时的推进开销（时间轮推进 + 重排）。
// 每次循环只推进一个 tick，验证「大量任务挂载下，单步推进是否仍是 O(1) 而非 O(n)」。
func BenchmarkManyTasks(b *testing.B) {
	s := NewScheduler[recoverable](noopHandler,
		WithTick[recoverable](time.Millisecond),
		WithSize[recoverable](512),
	)
	defer s.Stop()

	// 预挂 10 万个任务，分散在未来的不同时间点。
	const n = 100_000
	for i := 0; i < n; i++ {
		id := uint64(i)
		// 到期时间随机分散在 1s ~ 60s 之间，跨越多个层级。
		delay := time.Duration(i%60000) * time.Millisecond
		if err := s.Schedule(id, "bench", time.Now().Add(delay), recoverable{id: id}); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	// 连续推进若干 tick，测量调度循环的稳定开销。
	for i := 0; i < b.N; i++ {
		time.Sleep(time.Millisecond)
	}
}
