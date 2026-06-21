package hcache

import (
	"fmt"
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hsketch"
	"golang.org/x/sync/singleflight"
)

const (
	tlfCacheDefaultShards = 32
	tlfCacheDefaultCap    = 2048
)

// TinyLfuCache 是一个分片、并发安全的 TinyLFU 缓存。
//
// 每个分片内部是一个单线程 TinyLfu 实例，分片间使用 sync.RWMutex 保护。
// 内置 singleflight 防止缓存击穿。
// 支持 onLoader（miss 时自动回源）、onEvict（淘汰回调）、Modify（原子事务）。
type TinyLfuCache[K comparable, V any] struct {
	shards    []*tlfShard[K, V]
	shardMask uint64
	cap       int
	ttl       time.Duration

	onEvict  func(K, V)
	onLoader func(K) (*V, error)
	sf       singleflight.Group
}

type tlfShard[K comparable, V any] struct {
	mu  sync.RWMutex
	tlf *TinyLfu[K, V]
}

type TinyLfuCacheOption[K comparable, V any] func(c *TinyLfuCache[K, V])

func WithTinyLfuCacheCap[K comparable, V any](cap int) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) { c.cap = cap }
}
func WithTinyLfuCacheTtl[K comparable, V any](ttl time.Duration) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) { c.ttl = ttl }
}
func WithTinyLfuCacheShards[K comparable, V any](n int) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) { c.shardMask = uint64(n) - 1 }
}
func WithTinyLfuCacheOnEvict[K comparable, V any](fn func(K, V)) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) { c.onEvict = fn }
}
func WithTinyLfuCacheOnLoader[K comparable, V any](fn func(K) (*V, error)) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) { c.onLoader = fn }
}

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
			tlf: NewTinyLfu[K, V](WithTinyLfuCap[K, V](perShard), WithTinyLfuTtl[K, V](c.ttl)),
		}
	}
	return c
}

func (c *TinyLfuCache[K, V]) shard(key K) *tlfShard[K, V] {
	return c.shards[hsketch.Hash(key)&c.shardMask]
}

// Get 获取 key 对应的值，累加频率计数，命中时提升到 LRU 头部。
// 若配置了 onLoader 且 miss，通过 singleflight 回源加载并缓存。
func (c *TinyLfuCache[K, V]) Get(key K) (*V, bool) {
	s := c.shard(key)
	s.mu.RLock()
	v, ok := s.tlf.Get(key)
	s.mu.RUnlock()
	if ok {
		return v, true
	}
	if c.onLoader == nil {
		return nil, false
	}

	val, err, _ := c.sf.Do(sfKey(key), func() (any, error) {
		return c.onLoader(key)
	})
	if err != nil {
		return nil, false
	}
	loaded := val.(*V)

	s.mu.Lock()
	ek, ev, evicted := s.tlf.Set(key, loaded)
	s.mu.Unlock()
	if evicted && c.onEvict != nil {
		c.onEvict(ek, ev)
	}
	return loaded, true
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
// 若淘汰旧条目且配置了 onEvict，回调通知。
func (c *TinyLfuCache[K, V]) Set(key K, val *V) (evictedKey K, evictedVal V, evicted bool) {
	s := c.shard(key)
	s.mu.Lock()
	ek, ev, ok := s.tlf.Set(key, val)
	s.mu.Unlock()
	if ok && c.onEvict != nil {
		c.onEvict(ek, ev)
	}
	return ek, ev, ok
}

func (c *TinyLfuCache[K, V]) Del(key K) {
	s := c.shard(key)
	s.mu.Lock()
	s.tlf.Del(key)
	s.mu.Unlock()
}

func (c *TinyLfuCache[K, V]) Contains(key K) bool {
	s := c.shard(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tlf.Contains(key)
}

func (c *TinyLfuCache[K, V]) Len() int {
	total := 0
	for _, s := range c.shards {
		s.mu.RLock()
		total += s.tlf.Len()
		s.mu.RUnlock()
	}
	return total
}

func (c *TinyLfuCache[K, V]) Keys() []K {
	var keys []K
	for _, s := range c.shards {
		s.mu.RLock()
		keys = append(keys, s.tlf.Keys()...)
		s.mu.RUnlock()
	}
	return keys
}

func (c *TinyLfuCache[K, V]) Cap() int { return c.cap }

func (c *TinyLfuCache[K, V]) Purge() {
	for _, s := range c.shards {
		s.mu.Lock()
		s.tlf.Purge()
		s.mu.Unlock()
	}
}

// Modify 提供原子读-改-写事务。
// miss 时通过 singleflight 回源，fn 在分片写锁下执行。
func (c *TinyLfuCache[K, V]) Modify(key K, fn func(key K, old V) (*V, error)) (*V, error) {
	s := c.shard(key)

	s.mu.Lock()
	tlf := s.tlf
	v, ok := tlf.Get(key)
	if ok {
		newVal, err := fn(key, *v)
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		tlf.Set(key, newVal)
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
	v, ok = tlf.Get(key)
	if !ok {
		tlf.Set(key, loaded)
		v = loaded
	}

	newVal, err := fn(key, *v)
	if err != nil {
		return nil, err
	}
	tlf.Set(key, newVal)
	return newVal, nil
}
