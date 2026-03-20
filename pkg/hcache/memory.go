package hcache

import (
	"container/list"
	"context"
	"encoding/binary"
	"hash/fnv"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
	"golang.org/x/sync/singleflight"
)

// =====================================
// Near-Ristretto MemoryCache (Locked + True TinyLFU)
// - 4-bit counter (packed)
// - 4-way Count-Min Sketch
// - O(1) decay (sampling, no full scan)
// - Sharded LRU + TinyLFU admission
// - Deterministic (no async write)
// =====================================

const (
	defaultShard = 32
	// number of counters (must be power of 2)
	sketchCounters = 1 << 20 // ~1M counters (packed: 0.5MB)
)

// ===================== Hash =====================

func hash32(b []byte) uint32 {
	h := fnv.New32a()
	h.Write(b)
	return h.Sum32()
}

func hashKey[K comparable](key K) uint32 {
	return hash32([]byte(toString(key)))
}

func toString[K comparable](k K) string {
	switch v := any(k).(type) {
	case string:
		return v
	default:
		// fallback (not perfect but stable)
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(hash32([]byte("k"))))
		return string(buf[:])
	}
}

// ===================== TinyLFU (4-bit + 4-way CMS) =====================

// Each byte stores 2 counters (low 4 bits, high 4 bits)
type cmSketch struct {
	table []byte
	mask  uint32 // counters-1
}

func newCMSketch() *cmSketch {
	n := uint32(sketchCounters)
	// 2 counters per byte
	return &cmSketch{
		table: make([]byte, n/2),
		mask:  n - 1,
	}
}

// get 4-bit counter at index
func (c *cmSketch) get(idx uint32) uint8 {
	i := (idx & c.mask) >> 1
	b := c.table[i]
	if (idx & 1) == 0 {
		return b & 0x0F
	}
	return (b >> 4) & 0x0F
}

// increment with saturation (max 15)
func (c *cmSketch) inc(idx uint32) {
	i := (idx & c.mask) >> 1
	shift := (idx & 1) * 4
	b := c.table[i]
	v := (b >> shift) & 0x0F
	if v < 15 {
		v++
	}
	// clear 4 bits then set
	b &^= (0x0F << shift)
	b |= (v << shift)
	c.table[i] = b
}

// 4-way estimate: min of 4 hashes
func (c *cmSketch) estimate(h uint32) uint8 {
	// derive 4 indices from one hash (fast mix)
	h1 := h
	h2 := h ^ (h >> 17)
	h3 := h ^ (h >> 11)
	h4 := h ^ (h >> 5)

	v1 := c.get(h1)
	v2 := c.get(h2)
	v3 := c.get(h3)
	v4 := c.get(h4)

	// min
	m := v1
	if v2 < m {
		m = v2
	}
	if v3 < m {
		m = v3
	}
	if v4 < m {
		m = v4
	}
	return m
}

// increment all 4 counters
func (c *cmSketch) add(h uint32) {
	h1 := h
	h2 := h ^ (h >> 17)
	h3 := h ^ (h >> 11)
	h4 := h ^ (h >> 5)

	c.inc(h1)
	c.inc(h2)
	c.inc(h3)
	c.inc(h4)
}

// O(1) decay via random sampling (no full scan)
func (c *cmSketch) decaySample(k int) {
	// sample k bytes and halve both 4-bit counters
	for i := 0; i < k; i++ {
		idx := rand.Intn(len(c.table))
		b := c.table[idx]
		// halve low and high nibbles separately
		low := (b & 0x0F) >> 1
		high := ((b >> 4) & 0x0F) >> 1
		c.table[idx] = (high << 4) | low
	}
}

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

	sketch *cmSketch

	// decay control
	decayEvery uint32 // trigger every N ops (approx)
	opCount    atomic.Uint32
}

// ===================== Constructor =====================
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

// ===================== Constructor =====================

func NewMemoryCache[K comparable, V any](loader func(context.Context, K) (*V, error), options ...MemoryCacheOption[K, V]) *MemoryCache[K, V] {
	c := &MemoryCache[K, V]{
		shards:     make([]shard[K, V], defaultShard),
		ttl:        time.Minute * 5,
		loader:     loader,
		sketch:     newCMSketch(),
		decayEvery: 1024, // tune: 512~4096
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
	idx := hashKey(key) % uint32(len(c.shards))
	return &c.shards[idx]
}

// ===================== Get =====================

func (c *MemoryCache[K, V]) Get(ctx context.Context, key K) (*V, error) {
	h := hashKey(key)
	c.sketch.add(h)

	// probabilistic decay trigger
	if c.opCount.Add(1)%c.decayEvery == 0 {
		// sample a small number of bytes (tunable)
		c.sketch.decaySample(32)
	}

	now := hutl.NowTime().UnixMilli()
	s := c.getShard(key)

	// fast path
	s.mu.RLock()
	it, ok := s.data[key]
	if ok && it.expireAt.Load() > now {
		val := it.val.Load()
		s.mu.RUnlock()
		if val != nil {
			// promote in LRU (upgrade lock briefly)
			s.mu.Lock()
			s.lru.MoveToFront(it.ele)
			s.mu.Unlock()
			return val, nil
		}
	} else {
		s.mu.RUnlock()
	}

	// load with singleflight
	v, err, _ := c.sf.Do(toString(key), func() (any, error) {
		return c.loader(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	val := v.(*V)

	// sync write
	c.setWithLock(key, val, h)

	return val, nil
}

// ===================== Transaction =====================

// Txn provides per-key atomic read-modify-write.
// It locks the shard, ensures value exists (load if needed), then applies fn.
// Note: keep fn fast; it runs under shard lock.
func (c *MemoryCache[K, V]) Modify(ctx context.Context, key K, fn func(ctx context.Context, key K, old V) (*V, error)) (*V, error) {
	h := hashKey(key)
	c.sketch.add(h)

	// decay trigger
	if c.opCount.Add(1)%c.decayEvery == 0 {
		c.sketch.decaySample(32)
	}

	s := c.getShard(key)

	now := hutl.NowTime().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.data[key]
	if !ok || it.expireAt.Load() <= now {
		// load under singleflight outside lock to avoid blocking others
		s.mu.Unlock()
		v, err, _ := c.sf.Do(toString(key), func() (any, error) {
			return c.loader(ctx, key)
		})
		if err != nil {
			// re-lock before return to keep lock state consistent
			s.mu.Lock()
			return nil, err
		}
		val := v.(*V)

		// re-lock and re-check (double-check)
		s.mu.Lock()
		it, ok = s.data[key]
		if !ok {
			it = &item[K, V]{key: key}
			it.val.Store(val)
			it.expireAt.Store(now + c.ttl.Milliseconds())
			it.ele = s.lru.PushFront(it)
			s.data[key] = it
		} else {
			it.val.Store(val)
			it.expireAt.Store(now + c.ttl.Milliseconds())
			s.lru.MoveToFront(it.ele)
		}
	}

	old := it.val.Load()
	newVal, err := fn(ctx, key, *old)
	if err != nil {
		return nil, err
	}

	// write back
	it.val.Store(newVal)
	it.expireAt.Store(now + c.ttl.Milliseconds())
	s.lru.MoveToFront(it.ele)

	return newVal, nil
}

// ===================== Set (with TinyLFU admission) =====================

func (c *MemoryCache[K, V]) setWithLock(key K, val *V, h uint32) {
	freq := c.sketch.estimate(h)

	s := c.getShard(key)
	now := hutl.NowTime().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()

	// update if exists
	if it, ok := s.data[key]; ok {
		it.val.Store(val)
		it.expireAt.Store(now + c.ttl.Milliseconds())
		s.lru.MoveToFront(it.ele)
		return
	}

	// admission + eviction
	if len(s.data) >= s.cap {
		victimEle := s.lru.Back()
		if victimEle != nil {
			victim := victimEle.Value.(*item[K, V])
			victimFreq := c.sketch.estimate(hashKey(victim.key))

			// reject if worse than victim
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
