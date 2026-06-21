package hcache_test

import (
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
)

func TestTinyLfu_SetGet(t *testing.T) {
	c := hcache.NewTinyLfu[string, int]()

	_, _, ok := c.Set("a", ptr(1))
	if ok {
		t.Fatal("no eviction expected")
	}

	v, ok := c.Get("a")
	if !ok || *v != 1 {
		t.Fatalf("expected (*1, true), got (*%v, %v)", v, ok)
	}
}

func TestTinyLfu_Miss(t *testing.T) {
	c := hcache.NewTinyLfu[string, int]()

	v, ok := c.Get("nonexistent")
	if ok || v != nil {
		t.Fatalf("expected (nil, false), got (%v, %v)", v, ok)
	}
}

func TestTinyLfu_Admission_RejectCold(t *testing.T) {
	c := hcache.NewTinyLfu[int, string](hcache.WithTinyLfuCap[int, string](3))

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
		t.Fatal("cold key should be rejected by TinyLFU")
	}
	if c.Contains(4) {
		t.Fatal("cold key should not be in cache")
	}

	for i := 1; i <= 3; i++ {
		if _, ok := c.Get(i); !ok {
			t.Fatalf("hot key %d should survive", i)
		}
	}
}

func TestTinyLfu_Admission_AcceptHot(t *testing.T) {
	c := hcache.NewTinyLfu[int, string](hcache.WithTinyLfuCap[int, string](2))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Get(1)
	c.Get(2)

	// 冷 key 被拒
	_, _, ok := c.Set(3, ptr("c"))
	if ok {
		t.Fatal("cold key 3 should be rejected")
	}

	// Get 建立频率
	c.Get(3)
	c.Get(3)
	c.Get(3)

	// 再次 Set → 准入
	ek, ev, ok := c.Set(3, ptr("c"))
	if !ok {
		t.Fatal("key 3 should be admitted after building frequency")
	}
	if _, ok := c.Get(3); !ok {
		t.Fatal("key 3 should be in cache")
	}
	if ek == 0 {
		t.Fatal("should have evicted some key")
	}
	_ = ev
}

func TestTinyLfu_Update(t *testing.T) {
	c := hcache.NewTinyLfu[string, string](hcache.WithTinyLfuCap[string, string](3))

	c.Set("k", ptr("old"))
	c.Set("k", ptr("new"))

	v, ok := c.Get("k")
	if !ok || *v != "new" {
		t.Fatalf("expected 'new', got %q", *v)
	}
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
}

func TestTinyLfu_TTL(t *testing.T) {
	c := hcache.NewTinyLfu[string, int](
		hcache.WithTinyLfuTtl[string, int](time.Millisecond * 50),
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

func TestTinyLfu_Del(t *testing.T) {
	c := hcache.NewTinyLfu[string, int]()

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

func TestTinyLfu_Purge(t *testing.T) {
	c := hcache.NewTinyLfu[string, int]()

	c.Set("a", ptr(1))
	c.Set("b", ptr(2))
	c.Purge()

	if c.Len() != 0 {
		t.Fatalf("expected len 0, got %d", c.Len())
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("key 'a' should be gone")
	}
}

func TestTinyLfu_Peek(t *testing.T) {
	c := hcache.NewTinyLfu[int, string](hcache.WithTinyLfuCap[int, string](2))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))

	v, ok := c.Peek(1)
	if !ok || *v != "a" {
		t.Fatalf("expected 'a', got %v", v)
	}

	// Peek 不提升 → 1 最旧
	ek, _, _ := c.Set(3, ptr("c"))
	if ek != 1 {
		t.Fatalf("Peek should not promote, expected evict 1, got %v", ek)
	}
}

func TestTinyLfu_Peek_NoSketch(t *testing.T) {
	c := hcache.NewTinyLfu[int, string](hcache.WithTinyLfuCap[int, string](2))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Get(1)
	c.Get(2)

	// 只 Peek，不 Get
	c.Peek(3)
	c.Peek(3)
	c.Peek(3)

	// Peek 不累加频率 → 被拒
	_, _, ok := c.Set(3, ptr("c"))
	if ok {
		t.Fatal("Peek should not build frequency, key 3 should be rejected")
	}
}

func TestTinyLfu_ScanResistance(t *testing.T) {
	c := hcache.NewTinyLfu[int, int](hcache.WithTinyLfuCap[int, int](50))

	for i := 0; i < 10; i++ {
		c.Set(i, ptr(i*100))
	}
	for round := 0; round < 20; round++ {
		for i := 0; i < 10; i++ {
			c.Get(i)
		}
	}

	// 扫描 200 个冷 key
	for i := 100; i < 300; i++ {
		c.Set(i, ptr(i))
	}

	for i := 0; i < 10; i++ {
		if _, ok := c.Get(i); !ok {
			t.Fatalf("hot key %d should survive scan", i)
		}
	}
}

func TestTinyLfu_Cap(t *testing.T) {
	c := hcache.NewTinyLfu[string, int](hcache.WithTinyLfuCap[string, int](256))
	if c.Cap() != 256 {
		t.Fatalf("expected cap 256, got %d", c.Cap())
	}
}

func TestTinyLfu_EvictReturn(t *testing.T) {
	c := hcache.NewTinyLfu[int, string](hcache.WithTinyLfuCap[int, string](2))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	// 只加热 key 2 → 1 频率低且在 LRU 尾部
	c.Get(2)
	c.Get(2)

	// key 3 建立频率
	c.Get(3)
	c.Get(3)

	ek, ev, ok := c.Set(3, ptr("c"))
	if !ok {
		t.Fatal("key 3 should be admitted")
	}
	if ek != 1 || *ev != "a" {
		t.Fatalf("expected evicted (1, a), got (%v, %v)", ek, ev)
	}
}

func TestTinyLfu_GetBuildsFrequency(t *testing.T) {
	c := hcache.NewTinyLfu[int, string](hcache.WithTinyLfuCap[int, string](2))

	c.Set(1, ptr("a"))
	c.Set(2, ptr("b"))
	c.Get(1)
	c.Get(2)

	// Get miss 累加频率
	for i := 0; i < 10; i++ {
		c.Get(3)
	}

	_, _, ok := c.Set(3, ptr("c"))
	if !ok {
		t.Fatal("key 3 should be admitted after Get builds frequency")
	}
	if !c.Contains(3) {
		t.Fatal("key 3 should be in cache")
	}
}
