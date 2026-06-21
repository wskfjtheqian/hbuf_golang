package hcache

import (
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hsketch"
)

const (
	tlfCacheDefaultShards = 32
	tlfCacheDefaultCap    = 2048
)

// TinyLfuCache 是一个分片、并发安全的 TinyLFU 缓存。
//
// 每个分片内部是一个单线程 TinyLfu 实例（含独立的 CMSketch），
// 分片间使用 sync.RWMutex 保护，减少锁竞争。
// 同一 key 总是路由到同一分片，频率统计正确累加。
//
// 典型用法:
//
//	c := NewTinyLfuCache[string, int](WithTinyLfuCacheCap[string, int](10000))
//	c.Get("key")             // 累加频率
//	c.Set("key", &val)       // TinyLFU 准入
//	v, ok := c.Get("key")
type TinyLfuCache[K comparable, V any] struct {
	shards    []*tlfShard[K, V]
	shardMask uint64
	cap       int
	ttl       time.Duration
}

// tlfShard 是单个分片，包含锁和 TinyLfu 实例。
type tlfShard[K comparable, V any] struct {
	mu  sync.RWMutex
	tlf *TinyLfu[K, V]
}

// TinyLfuCacheOption 是 TinyLfuCache 的函数式配置项。
type TinyLfuCacheOption[K comparable, V any] func(c *TinyLfuCache[K, V])

// WithTinyLfuCacheCap 设置缓存总容量，会均分到各分片。默认 2048。
func WithTinyLfuCacheCap[K comparable, V any](cap int) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) {
		c.cap = cap
	}
}

// WithTinyLfuCacheTtl 设置每个条目的过期时间，默认 5 分钟。
func WithTinyLfuCacheTtl[K comparable, V any](ttl time.Duration) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) {
		c.ttl = ttl
	}
}

// WithTinyLfuCacheShards 设置分片数，必须是 2 的幂。默认 32。
func WithTinyLfuCacheShards[K comparable, V any](n int) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) {
		c.shardMask = uint64(n) - 1
	}
}

// NewTinyLfuCache 创建一个分片并发安全的 TinyLFU 缓存。
func NewTinyLfuCache[K comparable, V any](options ...TinyLfuCacheOption[K, V]) *TinyLfuCache[K, V] {
	c := &TinyLfuCache[K, V]{
		shardMask: tlfCacheDefaultShards - 1,
		cap:       tlfCacheDefaultCap,
		ttl:       time.Minute * 5,
	}

	for _, option := range options {
		option(c)
	}

	numShards := int(c.shardMask) + 1
	perShard := c.cap / numShards
	if perShard <= 0 {
		perShard = 1
	}

	c.shards = make([]*tlfShard[K, V], numShards)
	for i := range c.shards {
		c.shards[i] = &tlfShard[K, V]{
			tlf: NewTinyLfu[K, V](
				WithTinyLfuCap[K, V](perShard),
				WithTinyLfuTtl[K, V](c.ttl),
			),
		}
	}

	return c
}

func (c *TinyLfuCache[K, V]) shard(key K) *tlfShard[K, V] {
	h := hsketch.Hash(key)
	return c.shards[h&c.shardMask]
}

// Get 获取 key 对应的值，累加频率计数，命中时提升到 LRU 头部。
func (c *TinyLfuCache[K, V]) Get(key K) (*V, bool) {
	s := c.shard(key)
	s.mu.RLock()
	v, ok := s.tlf.Get(key)
	s.mu.RUnlock()
	return v, ok
}

// Peek 返回 key 对应的值，不改变 LRU 位置，不累加频率。
func (c *TinyLfuCache[K, V]) Peek(key K) (*V, bool) {
	s := c.shard(key)
	s.mu.RLock()
	v, ok := s.tlf.Peek(key)
	s.mu.RUnlock()
	return v, ok
}

// Set 插入或更新一个键值对，带 TinyLFU 准入控制。
// 若淘汰旧条目，返回被淘汰的 key、value 和 true。
func (c *TinyLfuCache[K, V]) Set(key K, val *V) (evictedKey K, evictedVal V, evicted bool) {
	s := c.shard(key)
	s.mu.Lock()
	ek, ev, ok := s.tlf.Set(key, val)
	s.mu.Unlock()
	return ek, ev, ok
}

// Del 删除指定 key。
func (c *TinyLfuCache[K, V]) Del(key K) {
	s := c.shard(key)
	s.mu.Lock()
	s.tlf.Del(key)
	s.mu.Unlock()
}

// Contains 判断 key 是否存在且未过期。
func (c *TinyLfuCache[K, V]) Contains(key K) bool {
	s := c.shard(key)
	s.mu.RLock()
	v := s.tlf.Contains(key)
	s.mu.RUnlock()
	return v
}

// Len 返回缓存中所有分片的条目总数（近似值，非原子快照）。
func (c *TinyLfuCache[K, V]) Len() int {
	total := 0
	for _, s := range c.shards {
		s.mu.RLock()
		total += s.tlf.Len()
		s.mu.RUnlock()
	}
	return total
}

// Cap 返回缓存总容量。
func (c *TinyLfuCache[K, V]) Cap() int {
	return c.cap
}

// Purge 清空所有分片。
func (c *TinyLfuCache[K, V]) Purge() {
	for _, s := range c.shards {
		s.mu.Lock()
		s.tlf.Purge()
		s.mu.Unlock()
	}
}
