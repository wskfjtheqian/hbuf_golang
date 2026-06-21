package hcache_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
)

func TestTlfCache_SetGet(t *testing.T) {
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

func TestTlfCache_Miss(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()
	v, ok := c.Get("nonexistent")
	if ok || v != nil {
		t.Fatalf("expected (nil, false), got (%v, %v)", v, ok)
	}
}

func TestTlfCache_Admission(t *testing.T) {
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

	_, _, ok := c.Set(4, ptr("cold"))
	if ok {
		t.Fatal("cold key should be rejected")
	}
	for i := 1; i <= 3; i++ {
		if _, ok := c.Get(i); !ok {
			t.Fatalf("hot key %d should survive", i)
		}
	}
}

// ===================== onLoader =====================

func TestTlfCache_OnLoader(t *testing.T) {
	var calls int32
	c := hcache.NewTinyLfuCache[string, int](
		hcache.WithTinyLfuCacheOnLoader[string, int](func(key string) (*int, error) {
			atomic.AddInt32(&calls, 1)
			return ptr(42), nil
		}),
	)

	v, ok := c.Get("k")
	if !ok || *v != 42 {
		t.Fatalf("expected 42, got %v, ok=%v", v, ok)
	}

	// second call should hit cache
	v, ok = c.Get("k")
	if !ok || *v != 42 {
		t.Fatalf("expected 42 on second get, got %v, ok=%v", *v, ok)
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("loader should be called once, got %d", calls)
	}
}

func TestTlfCache_OnLoader_Error(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int](
		hcache.WithTinyLfuCacheOnLoader[string, int](func(key string) (*int, error) {
			return nil, fmt.Errorf("load error")
		}),
	)

	v, ok := c.Get("k")
	if ok || v != nil {
		t.Fatalf("expected (nil, false) on loader error, got (%v, %v)", v, ok)
	}
}

// ===================== onEvict =====================

func TestTlfCache_OnEvict(t *testing.T) {
	var evictedKey int
	var evictedVal *string

	c := hcache.NewTinyLfuCache[int, string](
		hcache.WithTinyLfuCacheCap[int, string](2),
		hcache.WithTinyLfuCacheShards[int, string](1),
		hcache.WithTinyLfuCacheOnEvict[int, string](func(k int, v *string) {
			evictedKey = k
			evictedVal = v
		}),
	)

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Get(1)
	c.Get(1) // heat key 1
	c.Get(2) // heat key 2

	// key 3 builds frequency
	c.Get(3)
	c.Get(3)
	c.Get(3)

	ek, ev, ok := c.Set(3, ptr("c"))
	if !ok {
		t.Fatal("key 3 should be admitted")
	}
	if evictedKey != ek || evictedVal != ev {
		t.Fatalf("onEvict mismatch: callback got (%v, %v), Set returned (%v, %v)",
			evictedKey, evictedVal, ek, ev)
	}
}

func TestTlfCache_OnEvict_NotCalledOnUpdate(t *testing.T) {
	var evictCalls int32
	c := hcache.NewTinyLfuCache[string, int](
		hcache.WithTinyLfuCacheCap[string, int](10),
		hcache.WithTinyLfuCacheOnEvict[string, int](func(k string, v *int) {
			atomic.AddInt32(&evictCalls, 1)
		}),
	)

	c.Set("k", ptr(1)) // insert
	c.Set("k", ptr(2)) // update — no eviction

	if atomic.LoadInt32(&evictCalls) != 0 {
		t.Fatal("onEvict should not be called on update")
	}
}

// ===================== Modify =====================

func TestTlfCache_Modify_Hit(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()

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

	// verify cache updated
	cur, ok := c.Get("counter")
	if !ok || *cur != 11 {
		t.Fatalf("expected 11 in cache, got %v", cur)
	}
}

func TestTlfCache_Modify_Miss_WithLoader(t *testing.T) {
	var loadCalls int32
	c := hcache.NewTinyLfuCache[string, int](
		hcache.WithTinyLfuCacheOnLoader[string, int](func(key string) (*int, error) {
			atomic.AddInt32(&loadCalls, 1)
			return ptr(100), nil
		}),
	)

	// Modify on a key not in cache → loader called → fn applied
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

func TestTlfCache_Modify_Miss_NoLoader(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()

	_, err := c.Modify("k", func(key string, old int) (*int, error) {
		return ptr(old + 1), nil
	})
	if err == nil {
		t.Fatal("expected error when no loader")
	}
}

func TestTlfCache_Modify_FnError(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()
	c.Set("k", ptr(10))

	_, err := c.Modify("k", func(key string, old int) (*int, error) {
		return nil, fmt.Errorf("fn error")
	})
	if err == nil {
		t.Fatal("expected error from fn")
	}

	// cache should be unchanged
	v, ok := c.Get("k")
	if !ok || *v != 10 {
		t.Fatalf("cache should be unchanged after fn error, got %v", v)
	}
}

// ===================== concurrent =====================

func TestTlfCache_Concurrent_Modify(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int](
		hcache.WithTinyLfuCacheCap[string, int](100),
		hcache.WithTinyLfuCacheOnLoader[string, int](func(key string) (*int, error) {
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
		t.Fatalf("expected 100 after 100 concurrent increments, got %d", *v)
	}
}

func TestTlfCache_Concurrent_Eviction(t *testing.T) {
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

func TestTlfCache_ScanResistance(t *testing.T) {
	c := hcache.NewTinyLfuCache[int, int](
		hcache.WithTinyLfuCacheCap[int, int](50),
		hcache.WithTinyLfuCacheShards[int, int](4),
	)
	for i := 0; i < 10; i++ {
		c.Set(i, ptr(i*100))
	}
	for round := 0; round < 20; round++ {
		for i := 0; i < 10; i++ {
			c.Get(i)
		}
	}
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

func TestTlfCache_Purge(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int]()
	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Purge()
	if c.Len() != 0 {
		t.Fatalf("expected len 0, got %d", c.Len())
	}
}

func TestTlfCache_Cap(t *testing.T) {
	c := hcache.NewTinyLfuCache[string, int](hcache.WithTinyLfuCacheCap[string, int](256))
	if c.Cap() != 256 {
		t.Fatalf("expected cap 256, got %d", c.Cap())
	}
}
