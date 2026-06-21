package hcache

import (
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hsketch"
)

const (
	lruCacheDefaultShards = 32
	lruCacheDefaultCap    = 2048
)

// LruCache 是一个分片、并发安全的 LRU 缓存。
//
// 将 key 哈希到多个分片，每个分片内部是一个单线程 Lru 实例，
// 分片间使用 sync.RWMutex 保护，减少锁竞争。
//
// 典型用法:
//
//	c := NewLruCache[string, int](WithLruCacheCap[string, int](10000))
//	c.Set("key", &val)
//	v, ok := c.Get("key")
type LruCache[K comparable, V any] struct {
	shards    []*lruShard[K, V]
	shardMask uint64
	cap       int
	ttl       time.Duration
}

// lruShard 是单个分片，包含锁和 Lru 实例。
type lruShard[K comparable, V any] struct {
	mu  sync.RWMutex
	lru *Lru[K, V]
}

// LruCacheOption 是 LruCache 的函数式配置项。
type LruCacheOption[K comparable, V any] func(c *LruCache[K, V])

// WithLruCacheCap 设置缓存总容量，会均分到各分片。默认 2048。
func WithLruCacheCap[K comparable, V any](cap int) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) {
		c.cap = cap
	}
}

// WithLruCacheTtl 设置每个条目的过期时间，默认 5 分钟。
func WithLruCacheTtl[K comparable, V any](ttl time.Duration) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) {
		c.ttl = ttl
	}
}

// WithLruCacheShards 设置分片数，必须是 2 的幂。默认 32。
func WithLruCacheShards[K comparable, V any](n int) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) {
		c.shardMask = uint64(n) - 1
	}
}

// NewLruCache 创建一个分片并发安全的 LRU 缓存。
func NewLruCache[K comparable, V any](options ...LruCacheOption[K, V]) *LruCache[K, V] {
	c := &LruCache[K, V]{
		shardMask: lruCacheDefaultShards - 1,
		cap:       lruCacheDefaultCap,
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

	c.shards = make([]*lruShard[K, V], numShards)
	for i := range c.shards {
		c.shards[i] = &lruShard[K, V]{
			lru: NewLru[K, V](
				WithLruCap[K, V](perShard),
				WithLruTtl[K, V](c.ttl),
			),
		}
	}

	return c
}

// shard 根据 key 的哈希值定位分片。
func (c *LruCache[K, V]) shard(key K) *lruShard[K, V] {
	h := hsketch.Hash(key)
	return c.shards[h&c.shardMask]
}

// Get 获取 key 对应的值，命中时将其提升到 LRU 头部。
func (c *LruCache[K, V]) Get(key K) (*V, bool) {
	s := c.shard(key)
	s.mu.RLock()
	v, ok := s.lru.Get(key)
	s.mu.RUnlock()
	return v, ok
}

// Peek 返回 key 对应的值，但不改变 LRU 位置。
func (c *LruCache[K, V]) Peek(key K) (*V, bool) {
	s := c.shard(key)
	s.mu.RLock()
	v, ok := s.lru.Peek(key)
	s.mu.RUnlock()
	return v, ok
}

// Set 插入或更新一个键值对。
// 若淘汰旧条目，返回被淘汰的 key、value 和 true。
func (c *LruCache[K, V]) Set(key K, val *V) (evictedKey K, evictedVal V, evicted bool) {
	s := c.shard(key)
	s.mu.Lock()
	ek, ev, ok := s.lru.Set(key, val)
	s.mu.Unlock()
	return ek, ev, ok
}

// Del 删除指定 key。
func (c *LruCache[K, V]) Del(key K) {
	s := c.shard(key)
	s.mu.Lock()
	s.lru.Del(key)
	s.mu.Unlock()
}

// Contains 判断 key 是否存在且未过期。
func (c *LruCache[K, V]) Contains(key K) bool {
	s := c.shard(key)
	s.mu.RLock()
	v := s.lru.Contains(key)
	s.mu.RUnlock()
	return v
}

// Len 返回缓存中所有分片的条目总数（近似值，非原子快照）。
func (c *LruCache[K, V]) Len() int {
	total := 0
	for _, s := range c.shards {
		s.mu.RLock()
		total += s.lru.Len()
		s.mu.RUnlock()
	}
	return total
}

// Cap 返回缓存总容量。
func (c *LruCache[K, V]) Cap() int {
	return c.cap
}

// Purge 清空所有分片。
func (c *LruCache[K, V]) Purge() {
	for _, s := range c.shards {
		s.mu.Lock()
		s.lru.Purge()
		s.mu.Unlock()
	}
}
