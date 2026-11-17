package htwheel_test

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
)

type firedCall struct {
	id      uint64
	exec    time.Time
	payload string
}

func newPersistent(t *testing.T, store htwheel.Store) *htwheel.PersistentScheduler {
	t.Helper()
	s := htwheel.NewScheduler(
		htwheel.WithTick(5*time.Millisecond),
		htwheel.WithSize(16),
		htwheel.WithWorkers(2),
	)
	p := htwheel.NewPersistentScheduler(store, s,
		htwheel.WithPollInterval(10*time.Millisecond),
		htwheel.WithLookahead(100*time.Millisecond),
	)
	t.Cleanup(p.Stop)
	return p
}

// 通过 PersistentScheduler 调度的任务：执行后应从存储层删除。
func TestPersistentScheduleFires(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newPersistent(t, store)

	got := make(chan firedCall, 1)
	p.Register("greet", func(id uint64, exec time.Time, payload []byte) {
		got <- firedCall{id: id, exec: exec, payload: string(payload)}
	})

	at := time.Now().Add(30 * time.Millisecond)
	if err := p.Schedule(1, at, "greet", []byte("hello")); err != nil {
		t.Fatal(err)
	}

	select {
	case c := <-got:
		if c.id != 1 || c.payload != "hello" {
			t.Fatalf("回调参数异常: %+v", c)
		}
		if !c.exec.Equal(at) {
			t.Fatalf("exec 应为计划时间 %v，实际 %v", at, c.exec)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("任务未执行")
	}

	eventually(t, time.Second, func() bool {
		_, ok, _ := store.Get(1)
		return !ok
	}, "执行后存储层应删除记录")
}

// 外部直接写入存储层的到期任务，应被轮询读入并执行。
func TestPersistentExternalInsert(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newPersistent(t, store)

	fired := make(chan uint64, 1)
	p.Register("job", func(id uint64, _ time.Time, _ []byte) { fired <- id })

	if err := store.Put(htwheel.Record{ID: 6, Expire: time.Now().Add(30 * time.Millisecond), Name: "job"}); err != nil {
		t.Fatal(err)
	}

	select {
	case id := <-fired:
		if id != 6 {
			t.Fatalf("id 异常: %d", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("外部插入的任务未被执行")
	}
}

// 取消后不应执行（即使内存任务已提交，fire 也会校验存储层）。
func TestPersistentCancel(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newPersistent(t, store)

	var fired int32
	p.Register("job", func(uint64, time.Time, []byte) { atomic.AddInt32(&fired, 1) })

	if err := p.Schedule(3, time.Now().Add(80*time.Millisecond), "job", nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Cancel(3); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)
	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Fatalf("取消后仍执行 %d 次", n)
	}
	if _, ok, _ := store.Get(3); ok {
		t.Fatal("取消后存储层仍有记录")
	}
}

// 改期：包括尚未预热进内存的远期任务。
func TestPersistentUpdate(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newPersistent(t, store)

	fired := make(chan uint64, 2)
	p.Register("job", func(id uint64, _ time.Time, _ []byte) { fired <- id })

	// 远期任务：超出 lookahead，不会被预热进内存。
	if err := store.Put(htwheel.Record{ID: 5, Expire: time.Now().Add(time.Hour), Name: "job"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok := p.Scheduler().Get(5); ok {
		t.Fatal("远期任务不应被提前预热进内存")
	}

	// 改到 50ms 后，应能执行。
	if err := p.Update(5, time.Now().Add(50*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-fired:
		if id != 5 {
			t.Fatalf("id 异常: %d", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("改期后的任务未执行")
	}
}

// 处理器缺失时保留记录，注册后应能执行。
func TestPersistentMissingHandlerRetry(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newPersistent(t, store)

	if err := store.Put(htwheel.Record{ID: 7, Expire: time.Now().Add(10 * time.Millisecond), Name: "later"}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(120 * time.Millisecond)
	if _, ok, _ := store.Get(7); !ok {
		t.Fatal("处理器缺失时应保留记录")
	}

	fired := make(chan struct{}, 1)
	p.Register("later", func(uint64, time.Time, []byte) { fired <- struct{}{} })

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("注册处理器后应重试成功")
	}

	eventually(t, time.Second, func() bool {
		_, ok, _ := store.Get(7)
		return !ok
	}, "成功后应删除记录")
}

// 文件存储：重启后记录仍在。
func TestFileStoreRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")

	store1 := htwheel.NewFileStore(path)
	future := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := store1.Put(htwheel.Record{ID: 1, Expire: future, Name: "job"}); err != nil {
		t.Fatal(err)
	}

	p := newPersistent(t, store1)
	p.Register("job", func(uint64, time.Time, []byte) {})

	// 等待一次轮询，确认远期记录未被删除。
	time.Sleep(60 * time.Millisecond)
	if _, ok, _ := store1.Get(1); !ok {
		t.Fatal("远期记录不应被删除")
	}
	p.Stop()

	// 新的存储实例从同一文件恢复。
	store2 := htwheel.NewFileStore(path)
	rec, ok, err := store2.Get(1)
	if err != nil || !ok {
		t.Fatalf("重启后应能读到记录: ok=%v err=%v", ok, err)
	}
	if !rec.Expire.Equal(future) {
		t.Fatalf("expire 不符: %v != %v", rec.Expire, future)
	}
}

// 慢处理器执行期间，轮询不应把同一任务重复读入导致并发重复执行。
func TestPersistentSlowHandlerNoDuplicate(t *testing.T) {
	store := htwheel.NewMemoryStore()
	p := newPersistent(t, store)

	var calls int32
	block := make(chan struct{})
	p.Register("slow", func(uint64, time.Time, []byte) {
		atomic.AddInt32(&calls, 1)
		<-block
	})

	if err := p.Schedule(8, time.Now().Add(10*time.Millisecond), "slow", nil); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond) // 期间轮询会执行约 20 次
	close(block)
	time.Sleep(100 * time.Millisecond)

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("慢任务被执行了 %d 次，期望 1 次", n)
	}
}

// Store 基础语义：筛选、排序、limit、幂等删除。
func TestMemoryStoreSemantics(t *testing.T) {
	store := htwheel.NewMemoryStore()
	now := time.Now()

	mustPut := func(id uint64, d time.Duration) {
		t.Helper()
		if err := store.Put(htwheel.Record{ID: id, Expire: now.Add(d), Name: "n"}); err != nil {
			t.Fatal(err)
		}
	}
	mustPut(1, 30*time.Millisecond)
	mustPut(2, 10*time.Millisecond)
	mustPut(3, time.Hour)

	recs, err := store.Scan(now.Add(time.Minute), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].ID != 2 || recs[1].ID != 1 {
		t.Fatalf("Scan 结果不符: %+v", recs)
	}

	limited, err := store.Scan(now.Add(time.Minute), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].ID != 2 {
		t.Fatalf("limit 未生效: %+v", limited)
	}

	if err := store.Delete(999); err != nil {
		t.Fatalf("删除不存在的记录不应报错: %v", err)
	}
	if err := store.Delete(1); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get(1); ok {
		t.Fatal("删除后仍可读到")
	}
}
