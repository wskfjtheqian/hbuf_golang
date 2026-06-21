package hcache

import (
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

/***
🎯 单线程 LRU 缓存

经典 LRU 淘汰策略：每次访问将元素移到链表头部，缓存满时淘汰最久未访问的尾部元素。
O(1) 查找 + O(1) 插入/淘汰。

TTL 过期机制：每次访问时检查，过期自动移除。

无锁设计，仅限单 goroutine 使用。如需并发安全，请使用 MemoryCache。
*/

// lruItem 是 LRU 链表中的缓存条目。
// prev/next 构成内嵌双向链表，无接口装箱和类型断言开销。
type lruItem[K comparable, V any] struct {
	key      K
	val      V
	expireAt int64
	prev     *lruItem[K, V]
	next     *lruItem[K, V]
}

// Lru 是一个泛型、单线程的 LRU 缓存。
//
// 内部使用 map 实现 O(1) 查找，使用内嵌泛型双向链表维护访问顺序。
// 无 container/list 的 any 接口装箱开销。
//
// 典型用法:
//
//	c := NewLru[string, int](WithLruCap[string, int](100))
//	c.Set("key", &val)
//	v, ok := c.Get("key")
type Lru[K comparable, V any] struct {
	data map[K]*lruItem[K, V]

	// head 和 tail 是哨兵节点，head 指向最新，tail 指向最旧
	head *lruItem[K, V]
	tail *lruItem[K, V]

	cap int
	ttl time.Duration
}

// LruOption 是 Lru 的函数式配置项。
type LruOption[K comparable, V any] func(c *Lru[K, V])

// WithLruCap 设置缓存最大容量，默认 2048。
func WithLruCap[K comparable, V any](cap int) LruOption[K, V] {
	return func(c *Lru[K, V]) {
		c.cap = cap
	}
}

// WithLruTtl 设置每个条目的过期时间，默认 5 分钟。
func WithLruTtl[K comparable, V any](ttl time.Duration) LruOption[K, V] {
	return func(c *Lru[K, V]) {
		c.ttl = ttl
	}
}

// NewLru 创建一个新的 LRU 缓存。
func NewLru[K comparable, V any](options ...LruOption[K, V]) *Lru[K, V] {
	c := &Lru[K, V]{
		data: make(map[K]*lruItem[K, V]),
		ttl:  time.Minute * 5,
		cap:  2048,
	}

	for _, option := range options {
		option(c)
	}

	if c.cap <= 0 {
		c.cap = 1
	}

	// 初始化哨兵节点
	c.head = new(lruItem[K, V])
	c.tail = new(lruItem[K, V])
	c.head.next = c.tail
	c.tail.prev = c.head

	return c
}

// Get 获取 key 对应的值。
// 命中未过期条目时将其移到 LRU 头部，返回 (*V, true)。
// 未命中或已过期时返回 (nil, false)，不自动回源。
func (c *Lru[K, V]) Get(key K) (*V, bool) {
	now := htime.NowTime().UnixMilli()

	if it, ok := c.data[key]; ok && it.expireAt > now {
		c.moveToFront(it)
		return &it.val, true
	}

	return nil, false
}

// Peek 返回 key 对应的值，但不改变其在 LRU 中的位置。
// 适用于只读检查而不影响淘汰顺序的场景。
// 命中未过期条目时返回 (*V, true)，否则返回 (nil, false)。
func (c *Lru[K, V]) Peek(key K) (*V, bool) {
	now := htime.NowTime().UnixMilli()

	if it, ok := c.data[key]; ok && it.expireAt > now {
		return &it.val, true
	}

	return nil, false
}

// Set 插入或更新一个键值对。
// 若因缓存满而淘汰旧条目，返回被淘汰的 key、value 和 true；
// 若更新已存在的 key 或无需淘汰，返回零值和 false。
func (c *Lru[K, V]) Set(key K, val *V) (evictedKey K, evictedVal V, evicted bool) {
	now := htime.NowTime().UnixMilli()

	if it, ok := c.data[key]; ok {
		it.val = *val
		it.expireAt = now + c.ttl.Milliseconds()
		c.moveToFront(it)
		return
	}

	evictedKey, evictedVal, evicted = c.evict()

	it := &lruItem[K, V]{
		key:      key,
		val:      *val,
		expireAt: now + c.ttl.Milliseconds(),
	}
	c.pushFront(it)
	c.data[key] = it
	return
}

// Del 删除指定 key。如果 key 不存在则无操作。
func (c *Lru[K, V]) Del(key K) {
	if it, ok := c.data[key]; ok {
		delete(c.data, key)
		c.remove(it)
	}
}

// Contains 判断 key 是否存在且未过期。
func (c *Lru[K, V]) Contains(key K) bool {
	now := htime.NowTime().UnixMilli()
	it, ok := c.data[key]
	return ok && it.expireAt > now
}

// Len 返回当前缓存中的条目数。
func (c *Lru[K, V]) Len() int {
	return len(c.data)
}

// Cap 返回缓存的最大容量。
func (c *Lru[K, V]) Cap() int {
	return c.cap
}

// Purge 清空所有缓存条目。
func (c *Lru[K, V]) Purge() {
	c.data = make(map[K]*lruItem[K, V])
	c.head.next = c.tail
	c.tail.prev = c.head
}

// ===================== 内嵌双向链表操作 =====================

// pushFront 将 item 插入到链表头部（哨兵 head 之后）。
func (c *Lru[K, V]) pushFront(it *lruItem[K, V]) {
	it.prev = c.head
	it.next = c.head.next
	c.head.next.prev = it
	c.head.next = it
}

// moveToFront 将已在链表中的 item 移到头部。
func (c *Lru[K, V]) moveToFront(it *lruItem[K, V]) {
	c.remove(it)
	c.pushFront(it)
}

// remove 从链表中移除 item（不释放 map 引用）。
func (c *Lru[K, V]) remove(it *lruItem[K, V]) {
	it.prev.next = it.next
	it.next.prev = it.prev
	it.prev = nil
	it.next = nil
}

// evict 淘汰链表尾部元素（最久未访问）。
// 返回被淘汰的 key、value 和是否发生淘汰。
func (c *Lru[K, V]) evict() (key K, val V, ok bool) {
	if len(c.data) < c.cap {
		return
	}

	it := c.tail.prev
	if it == c.head {
		return
	}
	key, val = it.key, it.val
	delete(c.data, it.key)
	c.remove(it)
	return key, val, true
}
