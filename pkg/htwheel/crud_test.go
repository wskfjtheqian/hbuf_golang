package htwheel_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
)

// Schedule 新增：ID 不存在时等价于新增。
func TestScheduleInsert(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	fired := make(chan string, 1)
	p.Register("job", func(_ uint64, _ time.Time, rec *htwheel.TaskRecord) error {
		fired <- string(rec.GetPayload())
		return nil
	})

	at := time.Now().Add(20 * time.Millisecond)
	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 1, Expire: at, Name: "job", Payload: []byte("v1"),
		Retries: 3, RetryDelay: time.Second,
	}); err != nil {
		t.Fatal(err)
	}

	rec, ok, _ := store.Get(context.Background(), 1)
	if !ok || string(rec.GetPayload()) != "v1" || rec.GetRetries() != 3 {
		t.Fatalf("Schedule 新增记录异常: %+v", rec)
	}

	select {
	case p := <-fired:
		if p != "v1" {
			t.Fatalf("payload 异常: %q", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("任务未执行")
	}
}

// Schedule 修改：ID 已存在时整体替换（含 Name/Payload/Expire/重试字段）。
func TestScheduleReplace(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	var aCalls, bCalls int32
	p.Register("a", func(_ uint64, _ time.Time, _ *htwheel.TaskRecord) error {
		atomic.AddInt32(&aCalls, 1)
		return nil
	})
	p.Register("b", func(_ uint64, _ time.Time, _ *htwheel.TaskRecord) error {
		atomic.AddInt32(&bCalls, 1)
		return nil
	})

	// 先新增一个远期任务 a。
	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{ID: 9, Expire: time.Now().Add(time.Hour), Name: "a"}); err != nil {
		t.Fatal(err)
	}

	// 整体替换为 b，并改到近期执行。
	at := time.Now().Add(30 * time.Millisecond)
	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 9, Expire: at, Name: "b", Payload: []byte("x"),
		Retries: 0, RetryDelay: 0,
	}); err != nil {
		t.Fatal(err)
	}

	rec, ok, _ := store.Get(context.Background(), 9)
	if !ok || rec.GetName() != "b" || string(rec.GetPayload()) != "x" || !rec.GetExpire().Equal(at) {
		t.Fatalf("Schedule 替换后记录异常: %+v", rec)
	}

	// 只有 b 执行，a 不应执行。
	time.Sleep(200 * time.Millisecond)
	if n := atomic.LoadInt32(&aCalls); n != 0 {
		t.Fatalf("被替换的处理器 a 不应执行，实际 %d 次", n)
	}
	if n := atomic.LoadInt32(&bCalls); n != 1 {
		t.Fatalf("替换后的处理器 b 应执行 1 次，实际 %d 次", n)
	}
}

// Schedule 名称非法应报错，且不写入存储。
func TestScheduleEmptyName(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{ID: 1, Expire: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("空 Name 应返回错误")
	}
	if _, ok, _ := store.Get(context.Background(), 1); ok {
		t.Fatal("空 Name 的 Schedule 不应写入存储")
	}
}

// UpdateWith 改处理器名与业务参数。
func TestUpdateNameAndPayload(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	var aCalls, bCalls int32
	gotPayload := make(chan string, 1)
	p.Register("a", func(_ uint64, _ time.Time, _ *htwheel.TaskRecord) error {
		atomic.AddInt32(&aCalls, 1)
		return nil
	})
	p.Register("b", func(_ uint64, _ time.Time, rec *htwheel.TaskRecord) error {
		atomic.AddInt32(&bCalls, 1)
		gotPayload <- string(rec.GetPayload())
		return nil
	})

	// 先调度一个远期任务 a。
	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 5, Expire: time.Now().Add(time.Hour), Name: "a", Payload: []byte("old"),
		Retries: 2, RetryDelay: time.Second,
	}); err != nil {
		t.Fatal(err)
	}

	// 改期 + 改处理器 + 改参数 + 改重试。
	at := time.Now().Add(30 * time.Millisecond)
	if err := p.UpdateWith(context.Background(), 5, at,
		htwheel.SetName[*htwheel.TaskRecord]("b"),
		htwheel.SetRetries[*htwheel.TaskRecord](4),
		htwheel.SetRetryDelay[*htwheel.TaskRecord](500*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}

	rec, ok, _ := store.Get(context.Background(), 5)
	if !ok || rec.GetName() != "b" || string(rec.GetPayload()) != "new" || rec.GetRetries() != 4 || rec.GetRetryDelay() != 500*time.Millisecond {
		t.Fatalf("UpdateWith 修改后记录异常: %+v", rec)
	}

	// 等待 b 执行。
	select {
	case p := <-gotPayload:
		if p != "new" {
			t.Fatalf("payload 应为 new，实际 %q", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("改期后的任务 b 应执行")
	}
	if n := atomic.LoadInt32(&aCalls); n != 0 {
		t.Fatalf("原处理器 a 不应执行，实际 %d 次", n)
	}
}

// SetPayload(nil) 清空参数。
func TestSetPayloadNil(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	p.Register("job", func(_ uint64, _ time.Time, rec *htwheel.TaskRecord) error {
		if rec.GetPayload() != nil {
			t.Errorf("payload 应为 nil，实际 %q", rec.GetPayload())
		}
		return nil
	})

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 3, Expire: time.Now().Add(20 * time.Millisecond), Name: "job", Payload: []byte("data"),
	}); err != nil {
		t.Fatal(err)
	}
	// 清空参数并改期。
	if err := p.UpdateWith(context.Background(), 3, time.Now().Add(40*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
}

// Remove 删除任务（Cancel 别名），幂等。
func TestRemoveTask(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)

	var fired int32
	p.Register("job", func(_ uint64, _ time.Time, _ *htwheel.TaskRecord) error {
		atomic.AddInt32(&fired, 1)
		return nil
	})

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{
		ID: 1, Expire: time.Now().Add(80 * time.Millisecond), Name: "job",
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Remove(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get(context.Background(), 1); ok {
		t.Fatal("Remove 后存储层仍有记录")
	}

	// 重复删除不报错。
	if err := p.Remove(context.Background(), 1); err != nil {
		t.Fatalf("重复删除不应报错: %v", err)
	}
	if err := p.Remove(context.Background(), 999); err != nil {
		t.Fatalf("删除不存在的任务不应报错: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Fatalf("删除后不应执行，实际 %d 次", n)
	}
}

// Schedule 后停止调度器，确认不 panic。
func TestScheduleAfterStop(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newRetryScheduler(t, store)
	p.Stop()

	if err := p.Schedule(context.Background(), &htwheel.TaskRecord{ID: 1, Expire: time.Now(), Name: "x"}); err != htwheel.ErrClosed {
		t.Fatalf("停止后 Schedule 应返回 ErrClosed，实际 %v", err)
	}
	if err := p.Remove(context.Background(), 1); err != htwheel.ErrClosed {
		t.Fatalf("停止后 Remove 应返回 ErrClosed，实际 %v", err)
	}
	if err := p.UpdateWith(context.Background(), 1, time.Now()); err != htwheel.ErrClosed {
		t.Fatalf("停止后 UpdateWith 应返回 ErrClosed，实际 %v", err)
	}
}
