package htwheel_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
)

func newRetryScheduler(t *testing.T, store htwheel.Store[*htwheel.TaskRecord], opts ...htwheel.PersistOption) *htwheel.PersistentScheduler[*htwheel.TaskRecord] {
	t.Helper()
	s := htwheel.NewScheduler(
		htwheel.WithTick(5*time.Millisecond),
		htwheel.WithSize(16),
		htwheel.WithWorkers(4),
	)
	base := []htwheel.PersistOption{
		htwheel.WithPollInterval(10 * time.Millisecond),
		htwheel.WithLookahead(100 * time.Millisecond),
	}
	p := htwheel.NewPersistentScheduler(store, s, nil, append(base, opts...)...)
	t.Cleanup(p.Stop)
	return p
}

// 失败后按间隔重试，成功后删除记录；每次失败扣减一次重试次数。
func TestRetryEventuallySucceeds(t *testing.T) {
	store := htwheel.NewMemoryStore()

	var mu sync.Mutex
	var failures []error
	p := newRetryScheduler(t, store, htwheel.WithErrorHandler(func(err error) {
		mu.Lock()
		failures = append(failures, err)
		mu.Unlock()
	}))

	var calls int32
	done := make(chan struct{}, 1)
	p.Register("job", func(id uint64, _ time.Time, _ *htwheel.TaskRecord) error {
		if atomic.AddInt32(&calls, 1) <= 2 {
			return errors.New("boom")
		}
		done <- struct{}{}
		return nil
	})

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 1, Expire: time.Now().Add(20 * time.Millisecond), Name: "job",
		Retries: 5, RetryDelay: 100 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	// 第一次失败后记录仍在，且剩余次数已减 1、下一次执行时间已改到重试时刻。
	eventually(t, 2*time.Second, func() bool {
		rec, ok, _ := store.Get(context.Background(), 1)
		return ok && rec.GetRetries() == 4
	}, "首次失败后剩余重试次数应为 4")

	rec, _, _ := store.Get(context.Background(), 1)
	if rec.GetRetryDelay() != 100*time.Millisecond {
		t.Fatalf("RetryDelay 应保持配置值 100ms，实际 %s", rec.GetRetryDelay())
	}
	if ttl := time.Until(rec.GetExpire()); ttl <= 0 || ttl > 150*time.Millisecond {
		t.Fatalf("重试时间应约在 100ms 后，实际 %s", ttl)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("任务未在重试后成功，调用次数 %d", atomic.LoadInt32(&calls))
	}

	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("期望执行 3 次（2 次失败 + 1 次成功），实际 %d", n)
	}

	eventually(t, time.Second, func() bool {
		_, ok, _ := store.Get(context.Background(), 1)
		return !ok
	}, "成功后应删除记录")

	mu.Lock()
	n := len(failures)
	for _, err := range failures {
		if errors.Is(err, htwheel.ErrRetriesExhausted) {
			t.Fatalf("未用尽重试却上报了 ErrRetriesExhausted: %v", err)
		}
	}
	mu.Unlock()
	if n != 2 {
		t.Fatalf("期望上报 2 次失败，实际 %d", n)
	}
}

// 重试次数用尽后放弃：删除记录并上报 ErrRetriesExhausted。
func TestRetryExhausted(t *testing.T) {
	store := htwheel.NewMemoryStore()

	var mu sync.Mutex
	var last error
	p := newRetryScheduler(t, store, htwheel.WithErrorHandler(func(err error) {
		mu.Lock()
		last = err
		mu.Unlock()
	}))

	var calls int32
	p.Register("bad", func(uint64, time.Time, *htwheel.TaskRecord) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("always fail")
	})

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 2, Expire: time.Now().Add(10 * time.Millisecond), Name: "bad",
		Retries: 2, RetryDelay: 20 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	// 首次 + 2 次重试 = 3 次调用。
	eventually(t, 3*time.Second, func() bool {
		return atomic.LoadInt32(&calls) == 3
	}, "应执行 3 次")

	eventually(t, time.Second, func() bool {
		_, ok, _ := store.Get(context.Background(), 2)
		return !ok
	}, "重试用尽后应删除记录")

	eventually(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return errors.Is(last, htwheel.ErrRetriesExhausted)
	}, "应上报 ErrRetriesExhausted")

	// 用尽后不应再执行。
	time.Sleep(150 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("用尽后仍在执行，总调用 %d 次", n)
	}
}

// Schedule 直接写入记录的重试字段；UpdateWith 可重置重试策略。
func TestRetryDefaultsAndOverride(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store, htwheel.WithDefaultRetry(1, 30*time.Millisecond))
	p.Register("job", func(uint64, time.Time, *htwheel.TaskRecord) error { return nil })

	at := time.Now().Add(time.Hour)

	// 记录自带重试字段，直接写入。
	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 1, Expire: at, Name: "job", Retries: 1, RetryDelay: 30 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}
	rec, ok, _ := store.Get(context.Background(), 1)
	if !ok || rec.GetRetries() != 1 || rec.GetRetryDelay() != 30*time.Millisecond {
		t.Fatalf("重试字段未写入记录: %+v", rec)
	}

	// 另一条任务用不同的重试字段。
	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 2, Expire: at, Name: "job", Retries: 3, RetryDelay: 50 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}
	rec, ok, _ = store.Get(context.Background(), 2)
	if !ok || rec.GetRetries() != 3 || rec.GetRetryDelay() != 50*time.Millisecond {
		t.Fatalf("任务级重试字段未生效: %+v", rec)
	}

	// 关闭单条任务的重试。
	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 3, Expire: at, Name: "job", Retries: 0, RetryDelay: 0,
	}); err != nil {
		t.Fatal(err)
	}
	rec, ok, _ = store.Get(context.Background(), 3)
	if !ok || rec.GetRetries() != 0 {
		t.Fatalf("应关闭该任务的重试: %+v", rec)
	}

	// UpdateWith 可重置重试次数（例如人工再给几次机会）。
	if err := p.UpdateWith(context.Background(), 1, at, htwheel.WithTaskRetry[*htwheel.TaskRecord](5, 10*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	rec, _, _ = store.Get(context.Background(), 1)
	if rec.GetRetries() != 5 || rec.GetRetryDelay() != 10*time.Millisecond {
		t.Fatalf("UpdateWith 未重置重试策略: %+v", rec)
	}
}

// 开启退避后重试间隔按倍数增长。
func TestRetryBackoff(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store,
		htwheel.WithRetryBackoff(2),
		htwheel.WithMaxRetryDelay(time.Second),
	)
	p.Register("bad", func(uint64, time.Time, *htwheel.TaskRecord) error { return errors.New("x") })

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 4, Expire: time.Now().Add(10 * time.Millisecond), Name: "bad",
		Retries: 4, RetryDelay: 20 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	// 第一次失败：本次等 20ms，下一次的间隔增长为 40ms。
	eventually(t, 2*time.Second, func() bool {
		rec, ok, _ := store.Get(context.Background(), 4)
		return ok && rec.GetRetries() == 3
	}, "首次失败后剩余重试次数应为 3")

	rec, _, _ := store.Get(context.Background(), 4)
	if rec.GetRetryDelay() != 40*time.Millisecond {
		t.Fatalf("退避后间隔应为 40ms，实际 %s", rec.GetRetryDelay())
	}
	if ttl := time.Until(rec.GetExpire()); ttl <= 0 || ttl > 60*time.Millisecond {
		t.Fatalf("本次重试仍应按 20ms 等待，实际 %s", ttl)
	}

	// 第二次失败：间隔继续增长为 80ms。
	eventually(t, 2*time.Second, func() bool {
		rec, ok, _ := store.Get(context.Background(), 4)
		return ok && rec.GetRetries() == 2 && rec.GetRetryDelay() == 80*time.Millisecond
	}, "第二次失败后间隔应增长为 80ms")

	if err := p.Cancel(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
}

// Handler panic 也应走重试流程，而不是直接吞掉任务。
func TestRetryOnPanic(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	var calls int32
	p.Register("boom", func(uint64, time.Time, *htwheel.TaskRecord) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			panic("kaboom")
		}
		return nil
	})

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 5, Expire: time.Now().Add(10 * time.Millisecond), Name: "boom",
		Retries: 1, RetryDelay: 20 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	eventually(t, 3*time.Second, func() bool {
		return atomic.LoadInt32(&calls) == 2
	}, "panic 后应重试一次")

	eventually(t, time.Second, func() bool {
		_, ok, _ := store.Get(context.Background(), 5)
		return !ok
	}, "重试成功后应删除记录")
}

// 处于重试等待期的任务可以被取消。
func TestRetryCancelDuringBackoff(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	var calls int32
	p.Register("bad", func(uint64, time.Time, *htwheel.TaskRecord) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("x")
	})

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 6, Expire: time.Now().Add(10 * time.Millisecond), Name: "bad",
		Retries: 5, RetryDelay: 300 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	eventually(t, 2*time.Second, func() bool {
		rec, ok, _ := store.Get(context.Background(), 6)
		return ok && rec.GetRetries() == 4
	}, "首次失败后应进入重试等待")

	if err := p.Cancel(context.Background(), 6); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get(context.Background(), 6); ok {
		t.Fatal("取消后存储层仍有记录")
	}

	time.Sleep(500 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("取消后不应再重试，实际执行 %d 次", n)
	}
}

// 重试字段需要随记录持久化，重启后继续按剩余次数重试。
func TestRetryFieldsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")

	store1 := htwheel.NewFileStore(path)
	if err := store1.Put(context.Background(), &htwheel.TaskRecord{
		ID:         1,
		Expire:     time.Now().Add(time.Hour).Truncate(time.Second),
		Name:       "job",
		Retries:    3,
		RetryDelay: 5 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}

	store2 := htwheel.NewFileStore(path)
	rec, ok, err := store2.Get(context.Background(), 1)
	if err != nil || !ok {
		t.Fatalf("重启后应能读到记录: ok=%v err=%v", ok, err)
	}
	if rec.GetRetries() != 3 || rec.GetRetryDelay() != 5*time.Second {
		t.Fatalf("重试字段未持久化: %+v", rec)
	}
}
