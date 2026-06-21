package hcache

import (
	"fmt"
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hsketch"
	"golang.org/x/sync/singleflight"
)

const (
	lruCacheDefaultShards = 32
	lruCacheDefaultCap    = 2048
)

// LruCache 是一个分片、并发安全的 LRU 缓存。
//
// 每个分片内部是一个单线程 Lru 实例，分片间使用 sync.RWMutex 保护。
// 内置 singleflight 防止缓存击穿。
// 支持 onLoader（miss 时自动回源）、onEvict（淘汰回调）、Modify（原子事务）。
type LruCache[K comparable, V any] struct {
	shards    []*lruShard[K, V]
	shardMask uint64
	cap       int
	ttl       time.Duration

	onEvict  func(K, *V)
	onLoader func(K) (*V, error)
	sf       singleflight.Group
}

type lruShard[K comparable, V any] struct {
	mu  sync.RWMutex
	lru *Lru[K, V]
}

type LruCacheOption[K comparable, V any] func(c *LruCache[K, V])

func WithLruCacheCap[K comparable, V any](cap int) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) { c.cap = cap }
}
func WithLruCacheTtl[K comparable, V any](ttl time.Duration) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) { c.ttl = ttl }
}
func WithLruCacheShards[K comparable, V any](n int) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) { c.shardMask = uint64(n) - 1 }
}
func WithLruCacheOnEvict[K comparable, V any](fn func(K, *V)) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) { c.onEvict = fn }
}
func WithLruCacheOnLoader[K comparable, V any](fn func(K) (*V, error)) LruCacheOption[K, V] {
	return func(c *LruCache[K, V]) { c.onLoader = fn }
}

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
			lru: NewLru[K, V](WithLruCap[K, V](perShard), WithLruTtl[K, V](c.ttl)),
		}
	}
	return c
}

func (c *LruCache[K, V]) shard(key K) *lruShard[K, V] {
	return c.shards[hsketch.Hash(key)&c.shardMask]
}

// Get 获取 key 对应的值，命中时提升到 LRU 头部。
// 若配置了 onLoader 且 miss，通过 singleflight 回源加载并缓存。
func (c *LruCache[K, V]) Get(key K) (*V, bool) {
	s := c.shard(key)
	s.mu.RLock()
	v, ok := s.lru.Get(key)
	s.mu.RUnlock()
	if ok {
		return v, true
	}
	if c.onLoader == nil {
		return nil, false
	}

	// singleflight 防击穿
	val, err, _ := c.sf.Do(sfKey(key), func() (any, error) {
		return c.onLoader(key)
	})
	if err != nil {
		return nil, false
	}
	loaded := val.(*V)

	s.mu.Lock()
	ek, ev, evicted := s.lru.Set(key, loaded)
	s.mu.Unlock()
	if evicted && c.onEvict != nil {
		c.onEvict(ek, ev)
	}
	return loaded, true
}

// Peek 返回 key 对应的值，但不改变 LRU 位置。
func (c *LruCache[K, V]) Peek(key K) (*V, bool) {
	s := c.shard(key)
	s.mu.RLock()
	v, ok := s.lru.Peek(key)
	s.mu.RUnlock()
	return v, ok
}

// Set 插入或更新一个键值对。若淘汰旧条目且配置了 onEvict，回调通知。
func (c *LruCache[K, V]) Set(key K, val *V) (evictedKey K, evictedVal *V, evicted bool) {
	s := c.shard(key)
	s.mu.Lock()
	ek, ev, ok := s.lru.Set(key, val)
	s.mu.Unlock()
	if ok && c.onEvict != nil {
		c.onEvict(ek, ev)
	}
	return ek, ev, ok
}

func (c *LruCache[K, V]) Del(key K) {
	s := c.shard(key)
	s.mu.Lock()
	s.lru.Del(key)
	s.mu.Unlock()
}

func (c *LruCache[K, V]) Contains(key K) bool {
	s := c.shard(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lru.Contains(key)
}

func (c *LruCache[K, V]) Len() int {
	total := 0
	for _, s := range c.shards {
		s.mu.RLock()
		total += s.lru.Len()
		s.mu.RUnlock()
	}
	return total
}

// Keys 返回缓存中所有 key（遍历各分片，非原子快照）。
func (c *LruCache[K, V]) Keys() []K {
	var keys []K
	for _, s := range c.shards {
		s.mu.RLock()
		keys = append(keys, s.lru.Keys()...)
		s.mu.RUnlock()
	}
	return keys
}

func (c *LruCache[K, V]) Cap() int { return c.cap }

func (c *LruCache[K, V]) Purge() {
	for _, s := range c.shards {
		s.mu.Lock()
		s.lru.Purge()
		s.mu.Unlock()
	}
}

// Modify 提供原子读-改-写事务。
// miss 时通过 singleflight 回源，fn 在分片写锁下执行。
func (c *LruCache[K, V]) Modify(key K, fn func(key K, old V) (*V, error)) (*V, error) {
	s := c.shard(key)

	s.mu.Lock()
	l := s.lru
	v, ok := l.Get(key)
	if ok {
		newVal, err := fn(key, *v)
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		l.Set(key, newVal)
		s.mu.Unlock()
		return newVal, nil
	}

	s.mu.Unlock()
	if c.onLoader == nil {
		return nil, fmt.Errorf("key not found and no loader configured")
	}

	val, err, _ := c.sf.Do(sfKey(key), func() (any, error) {
		return c.onLoader(key)
	})
	if err != nil {
		return nil, err
	}
	loaded := val.(*V)

	s.mu.Lock()
	defer s.mu.Unlock()

	// 双重检查：锁外加载期间，其他 goroutine 可能已写入
	v, ok = l.Get(key)
	if !ok {
		l.Set(key, loaded)
		v = loaded
	}

	newVal, err := fn(key, *v)
	if err != nil {
		return nil, err
	}
	l.Set(key, newVal)
	return newVal, nil
}

// sfKey 将 key 转为 singleflight 去重键。
func sfKey[K comparable](key K) string {
	return fmt.Sprintf("%v", key)
}
