package hcache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
)

func TestTinyLfuCache_SetGet(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()

	_, _, ok := c.Set("a", ptr(1))
	if ok {
		t.Fatal("no eviction expected")
	}

	v, ok := c.Get("a")
	if !ok || *v != 1 {
		t.Fatalf("expected (*1, true), got (*%v, %v)", v, ok)
	}
}

func TestTinyLfuCache_Miss(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()

	v, ok := c.Get("nonexistent")
	if ok || v != nil {
		t.Fatalf("expected (nil, false), got (%v, %v)", v, ok)
	}
}

func TestTinyLfuCache_Admission(t *testing.T) {
	c := hcache.NewTinyLfuCache[int, string](
		hcache.WithTinyLfuCacheCap[int, string](3),
		hcache.WithTinyLfuCacheShards[int, string](1),
	)

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Set(3, ptr("c"))
	c.Get(1)
	c.Get(2)
	c.Get(3)
	c.Get(1)
	c.Get(2)
	c.Get(3)

	// 冷 key 被拒
	_, _, ok := c.Set(4, ptr("cold"))
	if ok {
		t.Fatal("cold key should be rejected")
	}

	// 热 key 存活
	for i := 1; i <= 3; i++ {
		if _, ok := c.Get(i); !ok {
			t.Fatalf("hot key %d should survive", i)
		}
	}
}

func TestTinyLfuCache_Peek(t *testing.T) {
	c := hcache.NewTinyLfuCache[int, string](
		hcache.WithTinyLfuCacheCap[int, string](2),
		hcache.WithTinyLfuCacheShards[int, string](1),
	)

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))

	v, ok := c.Peek(1)
	if !ok || *v != "a" {
		t.Fatalf("expected 'a', got %v", v)
	}

	ek, _, _ := c.Set(3, ptr("c"))
	if ek != 1 {
		t.Fatalf("Peek should not promote, expected evict 1, got %v", ek)
	}
}

func TestTinyLfuCache_TTL(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int](
		hcache.WithTinyLfuCacheTtl[string, int](time.Millisecond*50),
		hcache.WithTinyLfuCacheShards[string, int](1),
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

func TestTinyLfuCache_Del(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Del("a")

	if c.Contains("a") {
		t.Fatal("key 'a' should be deleted")
	}
	if !c.Contains("b") {
		t.Fatal("key 'b' should still exist")
	}

	c.Del("nonexistent")
}

func TestTinyLfuCache_Concurrent(t *testing.T) {
	c := hcache.NewTinyLfuCache[int, int](hcache.WithTinyLfuCacheCap[int, int](1000))

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

func TestTinyLfuCache_Concurrent_Eviction(t *testing.T) {
	c := hcache.NewTinyLfuCache[int, int](
		hcache.WithTinyLfuCacheCap[int, int](50),
		hcache.WithTinyLfuCacheShards[int, int](4),
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

	if c.Len() > 50 {
		t.Fatalf("len %d exceeds capacity 50", c.Len())
	}
}

func TestTinyLfuCache_ScanResistance(t *testing.T) {
	c := hcache.NewTinyLfuCache[int, int](
		hcache.WithTinyLfuCacheCap[int, int](50),
		hcache.WithTinyLfuCacheShards[int, int](4),
	)

	// 建立热点
	for i := 0; i < 10; i++ {
		c.Set(i, ptr(i*100))
	}
	for round := 0; round < 20; round++ {
		for i := 0; i < 10; i++ {
			c.Get(i)
		}
	}

	// 并发扫描
	var wg sync.WaitGroup
	for i := 100; i < 300; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.Set(n, ptr(n))
		}(i)
	}
	wg.Wait()

	for i := 0; i < 10; i++ {
		if _, ok := c.Get(i); !ok {
			t.Fatalf("hot key %d should survive scan", i)
		}
	}
}

func TestTinyLfuCache_Purge(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Purge()

	if c.Len() != 0 {
		t.Fatalf("expected len 0, got %d", c.Len())
	}
}

func TestTinyLfuCache_Cap(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int](hcache.WithTinyLfuCacheCap[string, int](256))
	if c.Cap() != 256 {
		t.Fatalf("expected cap 256, got %d", c.Cap())
	}
}
