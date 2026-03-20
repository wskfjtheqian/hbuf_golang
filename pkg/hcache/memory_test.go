package hcache_test

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcache"
)

func TestMemoryCache_GetBasic(t *testing.T) {
	var loadCount int32

	loader := func(ctx context.Context, key string) (*int, error) {
		atomic.AddInt32(&loadCount, 1)
		v := 42
		return &v, nil
	}

	cache := hcache.NewMemoryCache[string, int](loader)

	ctx := context.Background()

	v1, err := cache.Get(ctx, "k1")
	if err != nil || *v1 != 42 {
		t.Fatalf("unexpected result: %v %v", v1, err)
	}

	v2, _ := cache.Get(ctx, "k1")

	if *v2 != 42 {
		t.Fatalf("cache miss on second get")
	}

	if atomic.LoadInt32(&loadCount) != 1 {
		t.Fatalf("loader should be called once, got %d", loadCount)
	}
}

func TestMemoryCache_TTLExpire(t *testing.T) {
	var loadCount int32

	loader := func(ctx context.Context, key string) (*int, error) {
		atomic.AddInt32(&loadCount, 1)
		v := int(loadCount)
		return &v, nil
	}

	cache := hcache.NewMemoryCache[string, int](
		loader,
		hcache.WithMemoryCacheTtl[string, int](time.Millisecond*50),
	)

	ctx := context.Background()

	v1, _ := cache.Get(ctx, "k1")

	time.Sleep(time.Millisecond * 80)

	v2, _ := cache.Get(ctx, "k1")

	if *v1 == *v2 {
		t.Fatalf("value should expire but didn't")
	}

	if loadCount < 2 {
		t.Fatalf("loader should be called twice")
	}
}

func TestMemoryCache_Singleflight(t *testing.T) {
	var loadCount int32

	loader := func(ctx context.Context, key string) (*int, error) {
		time.Sleep(time.Millisecond * 50)
		atomic.AddInt32(&loadCount, 1)
		v := 1
		return &v, nil
	}

	cache := hcache.NewMemoryCache[string, int](loader)

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Get(context.Background(), "k1")
		}()
	}

	wg.Wait()

	if atomic.LoadInt32(&loadCount) != 1 {
		t.Fatalf("singleflight failed, loadCount=%d", loadCount)
	}
}

func TestMemoryCache_Modify(t *testing.T) {
	loader := func(ctx context.Context, key string) (*int, error) {
		v := 10
		return &v, nil
	}

	cache := hcache.NewMemoryCache[string, int](loader)

	ctx := context.Background()

	v, err := cache.Modify(ctx, "k1", func(ctx context.Context, key string, old int) (*int, error) {
		newVal := old + 5
		return &newVal, nil
	})

	if err != nil {
		t.Fatal(err)
	}

	if *v != 15 {
		t.Fatalf("modify failed, got %d", *v)
	}
}

func TestMemoryCache_Modify_Concurrent(t *testing.T) {
	var value atomic.Int64

	loader := func(ctx context.Context, key string) (*int, error) {
		v := int(value.Load())
		return &v, nil
	}

	cache := hcache.NewMemoryCache[string, int](loader)

	var wg sync.WaitGroup

	for i := 0; i < 10000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Modify(context.Background(), "k1"+strconv.Itoa(i/10),
				func(ctx context.Context, key string, old int) (*int, error) {
					newVal := old + 1
					value.Store(int64(newVal))
					<-time.After(time.Millisecond)
					return &newVal, nil
				})
		}()
	}

	wg.Wait()

	v, _ := cache.Get(context.Background(), "k1")

	if *v != 500 {
		t.Fatalf("concurrent modify failed, got %d", *v)
	}
}

func TestMemoryCache_TinyLFUAdmission(t *testing.T) {
	loader := func(ctx context.Context, key string) (*int, error) {
		v := 1
		return &v, nil
	}

	cache := hcache.NewMemoryCache[string, int](
		loader,
		hcache.WithMemoryCacheCap[string, int](100),
	)

	ctx := context.Background()

	// 热点 key
	for i := 0; i < 100; i++ {
		cache.Get(ctx, "hot")
	}

	// 冷 key
	cache.Get(ctx, "cold")

	// 再访问 hot
	v, _ := cache.Get(ctx, "hot")

	if v == nil {
		t.Fatalf("hot key should survive")
	}
}
