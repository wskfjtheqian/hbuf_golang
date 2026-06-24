package hcache

import (
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

/***
🎯 单线程 LRU 缓存

经典 LRU 淘汰策略：O(1) 查找 + O(1) 插入/淘汰。
TTL 过期机制。内部 val 存储指针，避免 Set/淘汰时拷贝大对象。
无锁设计，仅限单 goroutine 使用。
*/

type lruItem[K comparable, V any] struct {
	key      K
	val      *V
	expireAt int64
	prev     *lruItem[K, V]
	next     *lruItem[K, V]
}

type Lru[K comparable, V any] struct {
	data map[K]*lruItem[K, V]
	head *lruItem[K, V]
	tail *lruItem[K, V]
	cap  int
	ttl  time.Duration
}

type LruOption[K comparable, V any] func(c *Lru[K, V])

func WithLruCap[K comparable, V any](cap int) LruOption[K, V] {
	return func(c *Lru[K, V]) { c.cap = cap }
}
func WithLruTtl[K comparable, V any](ttl time.Duration) LruOption[K, V] {
	return func(c *Lru[K, V]) { c.ttl = ttl }
}

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
	c.head = new(lruItem[K, V])
	c.tail = new(lruItem[K, V])
	c.head.next = c.tail
	c.tail.prev = c.head
	return c
}

func (c *Lru[K, V]) Get(key K) (*V, bool) {
	now := htime.NowTime().UnixMilli()
	if it, ok := c.data[key]; ok {
		if it.expireAt > now {
			c.moveToFront(it)
			return it.val, true
		}
		return it.val, false
	}
	return nil, false
}

func (c *Lru[K, V]) Peek(key K) (*V, bool) {
	now := htime.NowTime().UnixMilli()
	if it, ok := c.data[key]; ok {
		return it.val, it.expireAt > now
	}
	return nil, false
}

func (c *Lru[K, V]) Set(key K, val *V) (evictedKey K, evictedVal *V, evicted bool) {
	now := htime.NowTime().UnixMilli()
	if it, ok := c.data[key]; ok {
		it.val = val
		it.expireAt = now + c.ttl.Milliseconds()
		c.moveToFront(it)
		return
	}
	evictedKey, evictedVal, evicted = c.evict()
	it := &lruItem[K, V]{
		key:      key,
		val:      val,
		expireAt: now + c.ttl.Milliseconds(),
	}
	c.pushFront(it)
	c.data[key] = it
	return
}

func (c *Lru[K, V]) Del(key K) {
	if it, ok := c.data[key]; ok {
		delete(c.data, key)
		c.remove(it)
	}
}

func (c *Lru[K, V]) Contains(key K) bool {
	now := htime.NowTime().UnixMilli()
	it, ok := c.data[key]
	return ok && it.expireAt > now
}

func (c *Lru[K, V]) Len() int { return len(c.data) }
func (c *Lru[K, V]) Cap() int { return c.cap }

func (c *Lru[K, V]) Keys() []K {
	keys := make([]K, 0, len(c.data))
	for k := range c.data {
		keys = append(keys, k)
	}
	return keys
}

func (c *Lru[K, V]) Purge() {
	c.data = make(map[K]*lruItem[K, V])
	c.head.next = c.tail
	c.tail.prev = c.head
}

func (c *Lru[K, V]) pushFront(it *lruItem[K, V]) {
	it.prev = c.head
	it.next = c.head.next
	c.head.next.prev = it
	c.head.next = it
}
func (c *Lru[K, V]) moveToFront(it *lruItem[K, V]) {
	c.remove(it)
	c.pushFront(it)
}
func (c *Lru[K, V]) remove(it *lruItem[K, V]) {
	it.prev.next = it.next
	it.next.prev = it.prev
	it.prev = nil
	it.next = nil
}
func (c *Lru[K, V]) evict() (key K, val *V, ok bool) {
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
