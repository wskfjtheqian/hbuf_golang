package hcache

import (
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hsketch"
	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

/***
🎯 单线程 TinyLFU 缓存

在 LRU 基础上加入 TinyLFU 准入过滤：新条目必须比 LRU 尾部更"热"才能进入缓存，
从而免疫扫描污染（一次性的批量查询不会挤掉真正的热点数据）。

频率统计：4-bit 计数器 × 4-way Count-Min Sketch（~0.5MB），
每次 Get 累加频率，每 1024 次操作触发一次 O(1) 随机采样衰减。

无锁设计，仅限单 goroutine 使用。如需并发安全，请使用 TinyLfuCache。
*/

const (
	tlfDecayEvery  = 1024
	tlfDecaySample = 32
)

// tlfItem 是 TinyLFU 缓存中的条目，内嵌双向链表指针。
type tlfItem[K comparable, V any] struct {
	key      K
	val      *V
	expireAt int64
	prev     *tlfItem[K, V]
	next     *tlfItem[K, V]
}

// TinyLfu 是一个单线程的 TinyLFU 缓存。
//
// Get 每次都会累加频率计数；Set 时通过频率比较决定是否准入新条目。
//
// 典型用法:
//
//	c := NewTinyLfu[string, int](WithTinyLfuCap[string, int](100))
//	c.Get("key")             // 累加频率
//	c.Set("key", &val)       // TinyLFU 准入
//	v, ok := c.Get("key")
type TinyLfu[K comparable, V any] struct {
	data map[K]*tlfItem[K, V]
	head *tlfItem[K, V]
	tail *tlfItem[K, V]

	cap int
	ttl time.Duration

	sketch     *hsketch.CMSketch
	decayEvery uint32
	opCount    uint32
}

// TinyLfuOption 是 TinyLfu 的函数式配置项。
type TinyLfuOption[K comparable, V any] func(c *TinyLfu[K, V])

// WithTinyLfuCap 设置缓存最大容量，默认 2048。
func WithTinyLfuCap[K comparable, V any](cap int) TinyLfuOption[K, V] {
	return func(c *TinyLfu[K, V]) {
		c.cap = cap
	}
}

// WithTinyLfuTtl 设置每个条目的过期时间，默认 5 分钟。
func WithTinyLfuTtl[K comparable, V any](ttl time.Duration) TinyLfuOption[K, V] {
	return func(c *TinyLfu[K, V]) {
		c.ttl = ttl
	}
}

// NewTinyLfu 创建一个新的 TinyLFU 缓存。
func NewTinyLfu[K comparable, V any](options ...TinyLfuOption[K, V]) *TinyLfu[K, V] {
	c := &TinyLfu[K, V]{
		data:       make(map[K]*tlfItem[K, V]),
		ttl:        time.Minute * 5,
		cap:        2048,
		sketch:     hsketch.NewCMSketch(),
		decayEvery: tlfDecayEvery,
	}

	for _, option := range options {
		option(c)
	}

	if c.cap <= 0 {
		c.cap = 1
	}

	c.head = new(tlfItem[K, V])
	c.tail = new(tlfItem[K, V])
	c.head.next = c.tail
	c.tail.prev = c.head

	return c
}

// Get 获取 key 对应的值。
// 每次调用都会累加频率计数（命中与否都算"想要"），
// 命中未过期条目时将其移到 LRU 头部。
func (c *TinyLfu[K, V]) Get(key K) (*V, bool) {
	h := hsketch.Hash(key)
	c.sketch.Add(h)

	// 概率衰减
	c.opCount++
	if c.opCount%c.decayEvery == 0 {
		c.sketch.DecaySample(tlfDecaySample)
	}

	now := htime.NowTime().UnixMilli()

	if it, ok := c.data[key]; ok && it.expireAt > now {
		c.moveToFront(it)
		return it.val, true
	}

	return nil, false
}

// Peek 返回 key 对应的值，不改变 LRU 位置，不累加频率。
func (c *TinyLfu[K, V]) Peek(key K) (*V, bool) {
	now := htime.NowTime().UnixMilli()

	if it, ok := c.data[key]; ok && it.expireAt > now {
		return it.val, true
	}

	return nil, false
}

// Set 插入或更新一个键值对，带 TinyLFU 准入控制。
//
//   - key 已存在：直接更新值，移到头部。
//   - 缓存未满：直接插入。
//   - 缓存已满：比较新条目的频率与 LRU 尾部 victim 的频率，
//     频率 ≥ victim 则淘汰 victim 并插入；否则拒绝，返回 (_, _, false)。
func (c *TinyLfu[K, V]) Set(key K, val *V) (evictedKey K, evictedVal *V, evicted bool) {
	freq := c.sketch.Estimate(hsketch.Hash(key))
	now := htime.NowTime().UnixMilli()

	// 已存在：直接更新
	if it, ok := c.data[key]; ok {
		it.val = val
		it.expireAt = now + c.ttl.Milliseconds()
		c.moveToFront(it)
		return
	}

	// 未满：直接插入
	if len(c.data) < c.cap {
		it := &tlfItem[K, V]{
			key:      key,
			val:      val,
			expireAt: now + c.ttl.Milliseconds(),
		}
		c.pushFront(it)
		c.data[key] = it
		return
	}

	// 已满：TinyLFU 准入判断
	victim := c.tail.prev
	if victim == c.head {
		return
	}
	victimFreq := c.sketch.Estimate(hsketch.Hash(victim.key))

	// 频率不如 victim → 拒绝
	if freq < victimFreq {
		return
	}

	// 淘汰 victim，插入新条目
	evictedKey, evictedVal = victim.key, victim.val
	delete(c.data, victim.key)
	c.remove(victim)

	it := &tlfItem[K, V]{
		key:      key,
		val:      val,
		expireAt: now + c.ttl.Milliseconds(),
	}
	c.pushFront(it)
	c.data[key] = it
	return evictedKey, evictedVal, true
}

// Del 删除指定 key。
func (c *TinyLfu[K, V]) Del(key K) {
	if it, ok := c.data[key]; ok {
		delete(c.data, key)
		c.remove(it)
	}
}

// Contains 判断 key 是否存在且未过期。
func (c *TinyLfu[K, V]) Contains(key K) bool {
	now := htime.NowTime().UnixMilli()
	it, ok := c.data[key]
	return ok && it.expireAt > now
}

// Len 返回当前缓存中的条目数。
func (c *TinyLfu[K, V]) Len() int {
	return len(c.data)
}

func (c *TinyLfu[K, V]) Keys() []K {
	keys := make([]K, 0, len(c.data))
	for k := range c.data {
		keys = append(keys, k)
	}
	return keys
}

func (c *TinyLfu[K, V]) Cap() int {
	return c.cap
}

// Purge 清空所有缓存条目。
func (c *TinyLfu[K, V]) Purge() {
	c.data = make(map[K]*tlfItem[K, V])
	c.head.next = c.tail
	c.tail.prev = c.head
}

// ===================== 内嵌双向链表操作 =====================

func (c *TinyLfu[K, V]) pushFront(it *tlfItem[K, V]) {
	it.prev = c.head
	it.next = c.head.next
	c.head.next.prev = it
	c.head.next = it
}

func (c *TinyLfu[K, V]) moveToFront(it *tlfItem[K, V]) {
	c.remove(it)
	c.pushFront(it)
}

func (c *TinyLfu[K, V]) remove(it *tlfItem[K, V]) {
	it.prev.next = it.next
	it.next.prev = it.prev
	it.prev = nil
	it.next = nil
}
