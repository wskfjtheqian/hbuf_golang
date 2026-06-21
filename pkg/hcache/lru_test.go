package hcache_test

import (
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
)

func TestLru_SetGet(t *testing.T) {
	c := hcache.NewLru[string, int]()

	ek, ev, ok := c.Set("a", ptr(1))
	if ok {
		t.Fatalf("no eviction expected, got key=%v val=%v", ek, ev)
	}

	v, ok := c.Get("a")
	if !ok || *v != 1 {
		t.Fatalf("expected (*1, true), got (*%v, %v)", v, ok)
	}
}

func TestLru_Miss(t *testing.T) {
	c := hcache.NewLru[string, int]()

	v, ok := c.Get("nonexistent")
	if ok || v != nil {
		t.Fatalf("expected (nil, false), got (%v, %v)", v, ok)
	}
}

func TestLru_Eviction(t *testing.T) {
	c := hcache.NewLru[int, string](hcache.WithLruCap[int, string](3))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Set(3, ptr("c"))

	c.Get(1) // promote 1 → order: 1,3,2

	ek, ev, ok := c.Set(4, ptr("d")) // should evict 2
	if !ok {
		t.Fatal("expected eviction")
	}
	if ek != 2 || ev != "b" {
		t.Fatalf("expected evicted (2, b), got (%v, %v)", ek, ev)
	}
	if c.Len() != 3 {
		t.Fatalf("expected len 3, got %d", c.Len())
	}
	if _, ok := c.Get(2); ok {
		t.Fatal("key 2 should have been evicted")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("key 1 should still exist")
	}
	if _, ok := c.Get(3); !ok {
		t.Fatal("key 3 should still exist")
	}
	if _, ok := c.Get(4); !ok {
		t.Fatal("key 4 should exist")
	}
}

func TestLru_EvictOldest(t *testing.T) {
	c := hcache.NewLru[int, string](hcache.WithLruCap[int, string](2))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Get(1) // promote 1 → order: 1,2

	ek, ev, ok := c.Set(3, ptr("c")) // evict 2
	if !ok || ek != 2 || ev != "b" {
		t.Fatalf("expected evicted (2, b), got (%v, %v, %v)", ek, ev, ok)
	}
	if _, ok := c.Get(2); ok {
		t.Fatal("key 2 should be evicted")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("key 1 should survive")
	}
	if c.Len() != 2 {
		t.Fatalf("expected len 2, got %d", c.Len())
	}
}

func TestLru_Update(t *testing.T) {
	c := hcache.NewLru[string, string](hcache.WithLruCap[string, string](2))

	c.Set("k", ptr("old"))
	// 更新已存在的 key，不应发生淘汰
	_, _, ok := c.Set("k", ptr("new"))
	if ok {
		t.Fatal("update should not trigger eviction")
	}

	v, ok := c.Get("k")
	if !ok || *v != "new" {
		t.Fatalf("expected 'new', got %q, ok=%v", *v, ok)
	}
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
}

func TestLru_TTL(t *testing.T) {
	c := hcache.NewLru[string, int](
		hcache.WithLruTtl[string, int](time.Millisecond * 50),
	)

	c.Set("k", ptr(10))

	v, ok := c.Get("k")
	if !ok || *v != 10 {
		t.Fatal("should be valid immediately")
	}

	time.Sleep(time.Millisecond * 80)

	_, ok = c.Get("k")
	if ok {
		t.Fatal("should be expired")
	}

	if c.Contains("k") {
		t.Fatal("Contains should be false after expiry")
	}
}

func TestLru_Del(t *testing.T) {
	c := hcache.NewLru[string, int]()

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Del("a")

	if c.Contains("a") {
		t.Fatal("key 'a' should be deleted")
	}
	if !c.Contains("b") {
		t.Fatal("key 'b' should still exist")
	}
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}

	c.Del("nonexistent")
}

func TestLru_Purge(t *testing.T) {
	c := hcache.NewLru[string, int]()

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Purge()

	if c.Len() != 0 {
		t.Fatalf("expected len 0, got %d", c.Len())
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("key 'a' should be gone after purge")
	}
}

func TestLru_Cap(t *testing.T) {
	c := hcache.NewLru[string, int](hcache.WithLruCap[string, int](42))
	if c.Cap() != 42 {
		t.Fatalf("expected cap 42, got %d", c.Cap())
	}
}

func TestLru_CapDefault(t *testing.T) {
	c := hcache.NewLru[string, int]()
	if c.Cap() != 2048 {
		t.Fatalf("expected default cap 2048, got %d", c.Cap())
	}
}

func TestLru_Cap1(t *testing.T) {
	c := hcache.NewLru[int, string](hcache.WithLruCap[int, string](1))

	c.Set(1, ptr("a"))
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}

	ek, ev, ok := c.Set(2, ptr("b")) // 应淘汰 1
	if !ok || ek != 1 || ev != "a" {
		t.Fatalf("expected evicted (1, a), got (%v, %v, %v)", ek, ev, ok)
	}
	if _, ok := c.Get(1); ok {
		t.Fatal("key 1 should be evicted")
	}
	if _, ok := c.Get(2); !ok {
		t.Fatal("key 2 should exist")
	}
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
}

func TestLru_UpdateExisting(t *testing.T) {
	c := hcache.NewLru[string, int](hcache.WithLruCap[string, int](3))

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Set("a", ptr(10)) // 更新 a，无淘汰
	c.Set("c", ptr(3))

	if c.Len() != 3 {
		t.Fatalf("expected len 3, got %d", c.Len())
	}

	v, ok := c.Get("a")
	if !ok || *v != 10 {
		t.Fatalf("expected 10, got %v", v)
	}
}

func TestLru_Contains_Expired(t *testing.T) {
	c := hcache.NewLru[string, int](
		hcache.WithLruTtl[string, int](time.Millisecond * 30),
	)

	c.Set("k", ptr(1))
	if !c.Contains("k") {
		t.Fatal("should be present immediately")
	}

	time.Sleep(time.Millisecond * 50)

	if c.Contains("k") {
		t.Fatal("should be expired")
	}
}

func TestLru_GetExpiredRemoves(t *testing.T) {
	c := hcache.NewLru[string, int](
		hcache.WithLruTtl[string, int](time.Millisecond * 30),
	)

	c.Set("k", ptr(1))
	time.Sleep(time.Millisecond * 50)

	_, ok := c.Get("k")
	if ok {
		t.Fatal("should return false for expired key")
	}
}

func TestLru_GetPromotes(t *testing.T) {
	c := hcache.NewLru[int, int](hcache.WithLruCap[int, int](2))

	c.Set(1, ptr(10))
	c.Set(2, ptr(20))
	c.Get(1) // promote 1 → order: 1,2

	ek, _, ok := c.Set(3, ptr(30)) // evict 2
	if !ok || ek != 2 {
		t.Fatalf("expected evicted key 2, got %v", ek)
	}
	if _, ok := c.Get(2); ok {
		t.Fatal("key 2 should be evicted since 1 was promoted")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("key 1 should survive after promotion")
	}
}

func TestLru_SetNoEviction_BelowCap(t *testing.T) {
	c := hcache.NewLru[int, string](hcache.WithLruCap[int, string](5))

	_, _, ok := c.Set(1, ptr("a"))
	if ok {
		t.Fatal("should not evict when below capacity")
	}
	_, _, ok = c.Set(2, ptr("b"))
	if ok {
		t.Fatal("should not evict when below capacity")
	}
	if c.Len() != 2 {
		t.Fatalf("expected len 2, got %d", c.Len())
	}
}

func TestLru_Peek(t *testing.T) {
	c := hcache.NewLru[int, string](hcache.WithLruCap[int, string](2))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))

	// Peek 不应改变 LRU 顺序
	v, ok := c.Peek(1)
	if !ok || *v != "a" {
		t.Fatalf("expected 'a', got %v, ok=%v", v, ok)
	}

	// 插入新条目，淘汰的应是 1（Peek 未提升它）
	ek, _, _ := c.Set(3, ptr("c"))
	if ek != 1 {
		t.Fatalf("Peek should not promote, expected evict 1, got %v", ek)
	}
}

func TestLru_Peek_Miss(t *testing.T) {
	c := hcache.NewLru[string, int]()

	v, ok := c.Peek("nonexistent")
	if ok || v != nil {
		t.Fatalf("expected (nil, false), got (%v, %v)", v, ok)
	}
}

func TestLru_Peek_Expired(t *testing.T) {
	c := hcache.NewLru[string, int](
		hcache.WithLruTtl[string, int](time.Millisecond * 30),
	)

	c.Set("k", ptr(1))
	time.Sleep(time.Millisecond * 50)

	_, ok := c.Peek("k")
	if ok {
		t.Fatal("Peek should return false for expired key")
	}
}

func ptr[T any](v T) *T { return &v }
