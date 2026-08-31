package hcache

import (
	"container/list"
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hsketch"
	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
	"golang.org/x/sync/singleflight"
)

/***
🎯 主要功能
  泛型内存缓存
	支持任意类型的键值对存储（使用 Go 泛型）
	提供线程安全的并发访问
  智能缓存淘汰策略
	分片 LRU：将缓存分成 32 个分片，减少锁竞争
	TinyLFU 准入机制：使用频率过滤，只允许高频访问的数据进入缓存
    4路 Count-Min Sketch：精确估算键的访问频率
  高效的频率统计
	4位计数器打包存储：每个字节存2个计数器，节省内存
	O(1) 衰减算法：通过随机采样实现计数器衰减，无需全量扫描
	饱和计数：计数器最大值为15，防止溢出
  自动数据加载
	支持传入 loader 函数，缓存未命中时自动加载数据
	使用 singleflight 防止缓存击穿（同一时刻只加载一次相同key）
  TTL 过期机制
	支持自定义过期时间（默认5分钟）
	每次访问时检查是否过期
📦 核心组件
  cmSketch：Count-Min Sketch 频率统计结构
  shard：缓存分片，每个分片有独立的锁和 LRU 列表
  item：缓存项，包含键、值、过期时间和 LRU 链表节点
  MemoryCache：主缓存结构，管理所有分片和全局配置
🔧 主要方法
  Get()：获取缓存值，未命中时自动加载
  Modify()：原子性的读-改-写操作，适合需要修改缓存值的场景
  setWithLock()：内部方法，带 TinyLFU 准入控制的写入
💡 设计亮点
  高并发性能：32个分片减少锁竞争
  内存优化：4位计数器打包，约0.5MB存储1M个计数器
  防缓存污染：TinyLFU 确保只有热点数据才能进入缓存
  确定性行为：无异步写入，所有操作同步完成
  自适应衰减：每1024次操作触发一次计数器衰减
  这是一个生产级别的高性能缓存实现，适合需要高吞吐、低延迟的场景。
*/

// =====================================
// 近 Ristretto MemoryCache（加锁 + 真正的 TinyLFU）
// - 4位计数器（打包存储）
// - 4路 Count-Min Sketch
// - O(1) 衰减（采样，无全量扫描）
// - 分片 LRU + TinyLFU 准入机制
// - 确定性（无异步写入）
// =====================================

const (
	defaultShard = 32
	decaySample
)

// ===================== Item / Shard =====================
type item[K comparable, V any] struct {
	key K
	val atomic.Pointer[V]

	expireAt atomic.Int64
	ele      *list.Element
}

type shard[K comparable, V any] struct {
	mu   sync.RWMutex
	data map[K]*item[K, V]
	lru  *list.List
	cap  int
}

// ===================== MemoryCache =====================

type MemoryCache[K comparable, V any] struct {
	shards []shard[K, V]

	cap int
	ttl time.Duration

	loader func(context.Context, K) (*V, error)
	sf     singleflight.Group

	sketch *hsketch.CMSketch

	// 衰减控制
	decayEvery uint32 // 每N次操作触发一次（近似）
	opCount    atomic.Uint32
}

// MemoryCacheOption ===================== 构造函数 =====================
type MemoryCacheOption[K comparable, V any] func(c *MemoryCache[K, V])

func WithMemoryCacheCap[K comparable, V any](cap int) MemoryCacheOption[K, V] {
	return func(c *MemoryCache[K, V]) {
		c.cap = cap
	}
}

func WithMemoryCacheTtl[K comparable, V any](ttl time.Duration) MemoryCacheOption[K, V] {
	return func(c *MemoryCache[K, V]) {
		c.ttl = ttl
	}
}

// ===================== 构造函数 =====================

func NewMemoryCache[K comparable, V any](loader func(context.Context, K) (*V, error), options ...MemoryCacheOption[K, V]) *MemoryCache[K, V] {
	c := &MemoryCache[K, V]{
		shards:     make([]shard[K, V], defaultShard),
		ttl:        time.Minute * 5,
		loader:     loader,
		sketch:     hsketch.NewCMSketch(),
		decayEvery: 1024, // 可调：512~4096
		cap:        10000,
	}

	for _, option := range options {
		option(c)
	}

	perShard := c.cap / defaultShard
	if perShard <= 0 {
		perShard = 1
	}

	for i := range c.shards {
		c.shards[i] = shard[K, V]{
			data: make(map[K]*item[K, V]),
			lru:  list.New(),
			cap:  perShard,
		}
	}

	return c
}

func (c *MemoryCache[K, V]) getShard(key K) *shard[K, V] {
	idx := hsketch.Hash(key) % uint64(len(c.shards))
	return &c.shards[idx]
}

// ===================== 获取 =====================

func (c *MemoryCache[K, V]) Get(ctx context.Context, key K) (*V, error) {
	h := hsketch.Hash(key)
	c.sketch.Add(h)

	// 概率性衰减触发器
	if c.opCount.Add(1)%c.decayEvery == 0 {
		// 采样少量字节（可调）
		c.sketch.DecaySample(decaySample)
	}

	now := htime.NowTime().UnixMilli()
	s := c.getShard(key)

	// 快速路径
	s.mu.RLock()
	it, ok := s.data[key]
	if ok && it.expireAt.Load() > now {
		val := it.val.Load()
		s.mu.RUnlock()
		if val != nil {
			// 在LRU中提升（短暂升级锁）
			s.mu.Lock()
			s.lru.MoveToFront(it.ele)
			s.mu.Unlock()
			return val, nil
		}
	} else {
		s.mu.RUnlock()
	}

	// 使用singleflight加载
	v, err, _ := c.sf.Do(toString(key), func() (any, error) {
		return c.loader(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	val := v.(*V)
	// 同步写入
	c.setWithLock(key, val, h)
	return val, nil
}

// ===================== 替换 =====================

// Replace 如果存在就替换，不存在就返回
func (c *MemoryCache[K, V]) Replace(ctx context.Context, key K, val *V) error {
	s := c.getShard(key)
	s.mu.Lock()
	it, ok := s.data[key]
	if ok {
		h := hsketch.Hash(key)
		c.sketch.Add(h)

		// 衰减触发器
		if c.opCount.Add(1)%c.decayEvery == 0 {
			c.sketch.DecaySample(decaySample)
		}

		it.val.Store(val)
		it.expireAt.Store(htime.NowTime().UnixMilli() + c.ttl.Milliseconds())
		s.lru.MoveToFront(it.ele)
	}
	s.mu.Unlock()
	return nil
}

// ===================== 替换 =====================

// Insert 插入
func (c *MemoryCache[K, V]) Insert(ctx context.Context, key K, val *V) error {
	h := hsketch.Hash(key)
	c.sketch.Add(h)

	// 衰减触发器
	if c.opCount.Add(1)%c.decayEvery == 0 {
		c.sketch.DecaySample(decaySample)
	}

	s := c.getShard(key)
	s.mu.Lock()
	it, ok := s.data[key]
	if !ok {
		it = &item[K, V]{key: key}
		it.ele = s.lru.PushFront(it)
		s.data[key] = it
	}
	it.val.Store(val)
	it.expireAt.Store(htime.NowTime().UnixMilli() + c.ttl.Milliseconds())
	s.mu.Unlock()
	return nil
}

// ===================== 事务 =====================

// Modify 提供每键原子读-改-写操作。
// 它锁定分片，确保值存在（如果需要则加载），然后应用fn。
// 注意：保持fn快速执行；它在分片锁下运行。
func (c *MemoryCache[K, V]) Modify(ctx context.Context, key K, fn func(ctx context.Context, key K, old V) (*V, error)) (*V, error) {
	h := hsketch.Hash(key)
	c.sketch.Add(h)

	// 衰减触发器
	if c.opCount.Add(1)%c.decayEvery == 0 {
		c.sketch.DecaySample(decaySample)
	}

	s := c.getShard(key)
	now := htime.NowTime().UnixMilli()

	// 第一阶段：确保缓存中存在有效数据
	s.mu.Lock()
	it, ok := s.data[key]

	// 如果不存在或已过期，则释放锁去加载数据
	if !ok || it.expireAt.Load() <= now {
		s.mu.Unlock()

		// 使用 singleflight 防止并发回源
		v, err, _ := c.sf.Do(toString(key), func() (any, error) {
			return c.loader(ctx, key)
		})
		if err != nil {
			return nil, err
		}
		val := v.(*V)

		// 重新加锁并写入刚加载的数据（双重检查）
		s.mu.Lock()
		it, ok = s.data[key]
		if !ok || it.expireAt.Load() <= now {
			// 创建新项或更新旧项
			if !ok {
				it = &item[K, V]{key: key}
				it.ele = s.lru.PushFront(it)
				s.data[key] = it
			}
			it.val.Store(val)
			it.expireAt.Store(now + c.ttl.Milliseconds())
		} else {
			// 如果在解锁期间有其他 goroutine 已经更新了，则刷新 LRU
			s.lru.MoveToFront(it.ele)
		}
	}

	// 第二阶段：执行原子修改（此时一定持有锁且数据有效）
	defer s.mu.Unlock()

	oldVal := it.val.Load()
	newVal, err := fn(ctx, key, *oldVal)
	if err != nil {
		return nil, err
	}

	// 写回并重置 TTL
	it.val.Store(newVal)
	it.expireAt.Store(now + c.ttl.Milliseconds())
	s.lru.MoveToFront(it.ele)

	return newVal, nil
}

// ===================== 设置（带TinyLFU准入机制）=====================

func (c *MemoryCache[K, V]) setWithLock(key K, val *V, h uint64) {
	freq := c.sketch.Estimate(h)

	s := c.getShard(key)
	now := htime.NowTime().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果存在则更新
	if it, ok := s.data[key]; ok {
		it.val.Store(val)
		it.expireAt.Store(now + c.ttl.Milliseconds())
		s.lru.MoveToFront(it.ele)
		return
	}

	// 准入 + 驱逐
	if len(s.data) >= s.cap {
		victimEle := s.lru.Back()
		if victimEle != nil {
			victim := victimEle.Value.(*item[K, V])
			victimFreq := c.sketch.Estimate(hsketch.Hash(victim.key))

			// 如果比被淘汰项差则拒绝
			if freq < victimFreq {
				return
			}

			delete(s.data, victim.key)
			s.lru.Remove(victimEle)
		}
	}

	it := &item[K, V]{key: key}
	it.val.Store(val)
	it.expireAt.Store(now + c.ttl.Milliseconds())

	it.ele = s.lru.PushFront(it)
	s.data[key] = it
}

func toString[K comparable](k K) string {
	switch v := any(k).(type) {
	case string:
		return v
	default:
		// fallback (not perfect but stable)
		var buf [16]byte
		binary.LittleEndian.PutUint64(buf[:], hsketch.Hash(k))
		return string(buf[:])
	}
}
