package hcache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
)

func TestLruCache_SetGet(t *testing.T) {
	c := hcache.NewLruCache[string, int]()

	_, _, ok := c.Set("a", ptr(1))
	if ok {
		t.Fatal("no eviction expected")
	}

	v, ok := c.Get("a")
	if !ok || *v != 1 {
		t.Fatalf("expected (*1, true), got (*%v, %v)", v, ok)
	}
}

func TestLruCache_Miss(t *testing.T) {
	c := hcache.NewLruCache[string, int]()

	v, ok := c.Get("nonexistent")
	if ok || v != nil {
		t.Fatalf("expected (nil, false), got (%v, %v)", v, ok)
	}
}

func TestLruCache_Eviction(t *testing.T) {
	c := hcache.NewLruCache[int, string](
		hcache.WithLruCacheCap[int, string](3),
		hcache.WithLruCacheShards[int, string](1), // 单分片便于验证
	)

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Set(3, ptr("c"))
	c.Get(1) // promote 1

	ek, ev, ok := c.Set(4, ptr("d")) // evict 2
	if !ok || ek != 2 || ev != "b" {
		t.Fatalf("expected evicted (2, b), got (%v, %v, %v)", ek, ev, ok)
	}
	if _, ok := c.Get(2); ok {
		t.Fatal("key 2 should be evicted")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("key 1 should survive")
	}
}

func TestLruCache_Peek(t *testing.T) {
	c := hcache.NewLruCache[int, string](
		hcache.WithLruCacheCap[int, string](2),
		hcache.WithLruCacheShards[int, string](1),
	)

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))

	v, ok := c.Peek(1)
	if !ok || *v != "a" {
		t.Fatalf("expected 'a', got %v", v)
	}

	// Peek 不提升，1 应被淘汰
	ek, _, _ := c.Set(3, ptr("c"))
	if ek != 1 {
		t.Fatalf("Peek should not promote, expected evict 1, got %v", ek)
	}
}

func TestLruCache_TTL(t *testing.T) {
	c := hcache.NewLruCache[string, int](
		hcache.WithLruCacheTtl[string, int](time.Millisecond*50),
		hcache.WithLruCacheShards[string, int](1),
	)

	c.Set("k", ptr(10))
	v, ok := c.Get("k")
	if !ok || *v != 10 {
		t.Fatal("should be valid")
	}

	time.Sleep(time.Millisecond * 80)

	_, ok = c.Get("k")
	if ok {
		t.Fatal("should be expired")
	}
}

func TestLruCache_Del(t *testing.T) {
	c := hcache.NewLruCache[string, int]()

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Del("a")

	if c.Contains("a") {
		t.Fatal("key 'a' should be deleted")
	}
	if !c.Contains("b") {
		t.Fatal("key 'b' should still exist")
	}

	// 删除不存在的 key 不应 panic
	c.Del("nonexistent")
}

func TestLruCache_Concurrent(t *testing.T) {
	c := hcache.NewLruCache[int, int](hcache.WithLruCacheCap[int, int](1000))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Set(n, ptr(n*10))
		}(i)
	}
	wg.Wait()

	if c.Len() != 100 {
		t.Fatalf("expected len 100, got %d", c.Len())
	}

	// 并发 Get
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			v, ok := c.Get(n)
			if !ok || *v != n*10 {
				t.Errorf("key %d: expected %d, got %v, ok=%v", n, n*10, v, ok)
			}
		}(i)
	}
	wg.Wait()
}

func TestLruCache_Concurrent_Eviction(t *testing.T) {
	c := hcache.NewLruCache[int, int](
		hcache.WithLruCacheCap[int, int](50),
		hcache.WithLruCacheShards[int, int](4),
	)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Set(n, ptr(n))
		}(i)
	}
	wg.Wait()

	// 容量 50，不应超过
	if c.Len() > 50 {
		t.Fatalf("len %d exceeds capacity 50", c.Len())
	}
}

func TestLruCache_Purge(t *testing.T) {
	c := hcache.NewLruCache[string, int]()

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Purge()

	if c.Len() != 0 {
		t.Fatalf("expected len 0, got %d", c.Len())
	}
}

func TestLruCache_Cap(t *testing.T) {
	c := hcache.NewLruCache[string, int](hcache.WithLruCacheCap[string, int](128))
	if c.Cap() != 128 {
		t.Fatalf("expected cap 128, got %d", c.Cap())
	}
}
