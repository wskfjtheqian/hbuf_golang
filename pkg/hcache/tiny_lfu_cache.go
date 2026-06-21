package hcache

import (
	"fmt"
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
// 每个分片内部是一个单线程 TinyLfu 实例，分片间使用 sync.RWMutex 保护。
// 支持 onLoader（miss 时自动回源）、onEvict（淘汰回调）、Modify（原子事务）。
type TinyLfuCache[K comparable, V any] struct {
	shards    []*tlfShard[K, V]
	shardMask uint64
	cap       int
	ttl       time.Duration

	onEvict  func(K, V)
	onLoader func(K) (*V, error)
}

type tlfShard[K comparable, V any] struct {
	mu  sync.RWMutex
	tlf *TinyLfu[K, V]
}

// TinyLfuCacheOption 是 TinyLfuCache 的函数式配置项。
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

// WithTinyLfuCacheOnEvict 设置淘汰回调。条目因容量满被驱逐时调用。
func WithTinyLfuCacheOnEvict[K comparable, V any](fn func(K, V)) TinyLfuCacheOption[K, V] {
	return func(c *TinyLfuCache[K, V]) { c.onEvict = fn }
}

// WithTinyLfuCacheOnLoader 设置回源函数。Get/Modify miss 时自动调用加载数据。
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
// 若配置了 onLoader 且 miss，自动回源加载并缓存。
func (c *TinyLfuCache[K, V]) Get(key K) (*V, bool) {
	s := c.shard(key)

	// fast path: read lock
	s.mu.RLock()
	v, ok := s.tlf.Get(key)
	s.mu.RUnlock()
	if ok {
		return v, true
	}

	// miss: try loader under write lock
	if c.onLoader == nil {
		return nil, false
	}

	loaded, err := c.onLoader(key)
	if err != nil {
		return nil, false
	}

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
func (c *TinyLfuCache[K, V]) Cap() int { return c.cap }

// Purge 清空所有分片。
func (c *TinyLfuCache[K, V]) Purge() {
	for _, s := range c.shards {
		s.mu.Lock()
		s.tlf.Purge()
		s.mu.Unlock()
	}
}

// Modify 提供原子读-改-写事务。
//
// 1. 获取当前值（更新频率，提升 LRU）
// 2. 若 miss 且配置了 onLoader，解锁回源后重新加锁写入
// 3. 调用 fn(key, old) 得到新值
// 4. 写回缓存并刷新 TTL
//
// 注意：fn 在分片写锁下执行，应尽量快速。
func (c *TinyLfuCache[K, V]) Modify(key K, fn func(key K, old V) (*V, error)) (*V, error) {
	s := c.shard(key)

	// 第一阶段：确保值存在
	s.mu.Lock()
	tlf := s.tlf
	v, ok := tlf.Get(key)
	if ok {
		// 命中：直接执行修改
		newVal, err := fn(key, *v)
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		tlf.Set(key, newVal)
		s.mu.Unlock()
		return newVal, nil
	}

	// 未命中：释放锁去加载
	s.mu.Unlock()
	if c.onLoader == nil {
		return nil, fmt.Errorf("key not found and no loader configured")
	}

	loaded, err := c.onLoader(key)
	if err != nil {
		return nil, err
	}

	// 重新加锁，写入并执行修改
	s.mu.Lock()
	defer s.mu.Unlock()

	tlf.Set(key, loaded)

	newVal, err := fn(key, *loaded)
	if err != nil {
		return nil, err
	}
	tlf.Set(key, newVal)
	return newVal, nil
}
