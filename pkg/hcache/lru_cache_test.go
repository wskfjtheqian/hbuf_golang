package hcache_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
)

func TestLc_SetGet(t *testing.T) {
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

func TestLc_Miss(t *testing.T) {
	c := hcache.NewLruCache[string, int]()
	v, ok := c.Get("nonexistent")
	if ok || v != nil {
		t.Fatalf("expected (nil, false), got (%v, %v)", v, ok)
	}
}

func TestLc_Eviction(t *testing.T) {
	c := hcache.NewLruCache[int, string](
		hcache.WithLruCacheCap[int, string](3),
		hcache.WithLruCacheShards[int, string](1),
	)
	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Set(3, ptr("c"))
	c.Get(1)

	ek, ev, ok := c.Set(4, ptr("d"))
	if !ok || ek != 2 || ev != "b" {
		t.Fatalf("expected evicted (2, b), got (%v, %v, %v)", ek, ev, ok)
	}
}

// ===================== onLoader =====================

func TestLc_OnLoader(t *testing.T) {
	var calls int32
	c := hcache.NewLruCache[string, int](
		hcache.WithLruCacheOnLoader[string, int](func(key string) (*int, error) {
			atomic.AddInt32(&calls, 1)
			return ptr(42), nil
		}),
	)

	v, ok := c.Get("k")
	if !ok || *v != 42 {
		t.Fatalf("expected 42, got %v, ok=%v", v, ok)
	}

	v, ok = c.Get("k")
	if !ok || *v != 42 {
		t.Fatalf("expected 42 on second get, got %v, ok=%v", *v, ok)
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("loader should be called once, got %d", calls)
	}
}

func TestLc_OnLoader_Error(t *testing.T) {
	c := hcache.NewLruCache[string, int](
		hcache.WithLruCacheOnLoader[string, int](func(key string) (*int, error) {
			return nil, fmt.Errorf("load error")
		}),
	)

	v, ok := c.Get("k")
	if ok || v != nil {
		t.Fatalf("expected (nil, false) on loader error, got (%v, %v)", v, ok)
	}
}

// ===================== onEvict =====================

func TestLc_OnEvict(t *testing.T) {
	var evictedKey int
	var evictedVal string

	c := hcache.NewLruCache[int, string](
		hcache.WithLruCacheCap[int, string](2),
		hcache.WithLruCacheShards[int, string](1),
		hcache.WithLruCacheOnEvict[int, string](func(k int, v string) {
			evictedKey = k
			evictedVal = v
		}),
	)

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))

	ek, ev, ok := c.Set(3, ptr("c"))
	if !ok {
		t.Fatal("should evict")
	}
	if evictedKey != ek || evictedVal != ev {
		t.Fatalf("onEvict mismatch: callback (%v, %v), Set (%v, %v)",
			evictedKey, evictedVal, ek, ev)
	}
}

func TestLc_OnEvict_NotCalledOnUpdate(t *testing.T) {
	var evictCalls int32
	c := hcache.NewLruCache[string, int](
		hcache.WithLruCacheCap[string, int](10),
		hcache.WithLruCacheOnEvict[string, int](func(k string, v int) {
			atomic.AddInt32(&evictCalls, 1)
		}),
	)

	c.Set("k", ptr(1))
	c.Set("k", ptr(2))

	if atomic.LoadInt32(&evictCalls) != 0 {
		t.Fatal("onEvict should not be called on update")
	}
}

// ===================== Modify =====================

func TestLc_Modify_Hit(t *testing.T) {
	c := hcache.NewLruCache[string, int]()
	c.Set("counter", ptr(10))

	v, err := c.Modify("counter", func(key string, old int) (*int, error) {
		return ptr(old + 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if *v != 11 {
		t.Fatalf("expected 11, got %d", *v)
	}

	cur, ok := c.Get("counter")
	if !ok || *cur != 11 {
		t.Fatalf("expected 11 in cache, got %v", cur)
	}
}

func TestLc_Modify_Miss_WithLoader(t *testing.T) {
	var loadCalls int32
	c := hcache.NewLruCache[string, int](
		hcache.WithLruCacheOnLoader[string, int](func(key string) (*int, error) {
			atomic.AddInt32(&loadCalls, 1)
			return ptr(100), nil
		}),
	)

	v, err := c.Modify("k", func(key string, old int) (*int, error) {
		return ptr(old + 5), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if *v != 105 {
		t.Fatalf("expected 105, got %d", *v)
	}
	if atomic.LoadInt32(&loadCalls) != 1 {
		t.Fatalf("loader should be called once, got %d", loadCalls)
	}
}

func TestLc_Modify_Miss_NoLoader(t *testing.T) {
	c := hcache.NewLruCache[string, int]()

	_, err := c.Modify("k", func(key string, old int) (*int, error) {
		return ptr(old + 1), nil
	})
	if err == nil {
		t.Fatal("expected error when no loader")
	}
}

func TestLc_Modify_FnError(t *testing.T) {
	c := hcache.NewLruCache[string, int]()
	c.Set("k", ptr(10))

	_, err := c.Modify("k", func(key string, old int) (*int, error) {
		return nil, fmt.Errorf("fn error")
	})
	if err == nil {
		t.Fatal("expected error from fn")
	}

	v, ok := c.Get("k")
	if !ok || *v != 10 {
		t.Fatalf("cache should be unchanged, got %v", v)
	}
}

// ===================== concurrent =====================

func TestLc_Concurrent_Modify(t *testing.T) {
	c := hcache.NewLruCache[string, int](
		hcache.WithLruCacheCap[string, int](100),
		hcache.WithLruCacheOnLoader[string, int](func(key string) (*int, error) {
			return ptr(0), nil
		}),
	)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Modify("counter", func(key string, old int) (*int, error) {
				return ptr(old + 1), nil
			})
		}()
	}
	wg.Wait()

	v, ok := c.Get("counter")
	if !ok {
		t.Fatal("counter should exist")
	}
	if *v != 100 {
		t.Fatalf("expected 100, got %d", *v)
	}
}

func TestLc_Concurrent_Eviction(t *testing.T) {
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
	if c.Len() > 50 {
		t.Fatalf("len %d exceeds capacity 50", c.Len())
	}
}

func TestLc_Purge(t *testing.T) {
	c := hcache.NewLruCache[string, int]()
	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Purge()
	if c.Len() != 0 {
		t.Fatalf("expected len 0, got %d", c.Len())
	}
}

func TestLc_Cap(t *testing.T) {
	c := hcache.NewLruCache[string, int](hcache.WithLruCacheCap[string, int](256))
	if c.Cap() != 256 {
		t.Fatalf("expected cap 256, got %d", c.Cap())
	}
}
