package cache

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

//内存KV缓存管理器
//读写自动续期
//过期自动清除
//修改时自动加锁
//修改时简单事务

type Option[K comparable, V any] func(v *MemoryCache[K, V])

// NewMinExpire 参数最小有效时间
func NewMinExpire[K comparable, V any](duration time.Duration) Option[K, V] {
	return func(v *MemoryCache[K, V]) {
		v.minExpire = int64(duration / time.Millisecond)
	}
}

// NewMaxExpire 参数最大有效时间
func NewMaxExpire[K comparable, V any](duration time.Duration) Option[K, V] {
	return func(v *MemoryCache[K, V]) {
		v.maxExpire = int64(duration / time.Millisecond)
	}
}

// expireClear 过期清理接口
type expireClear interface {
	clearExpire(now int64)
}

// 解决高频调用time.Now()带来的性能问题
var timestamp atomic.Int64 //当前时间戳
var cacheList = make([]expireClear, 0)
var lock sync.Mutex

func init() {
	timestamp.Store(time.Now().UnixMilli())
	//每秒更新一次时间戳
	go func() {
		ticker := time.NewTicker(time.Second)
		for {
			now := <-ticker.C
			timestamp.Store(now.UnixMilli())
		}
	}()

	//每1分钟清理一次过期的缓存
	go func() {
		ticker := time.NewTicker(time.Minute * 1)
		for {
			now := <-ticker.C
			lock.Lock()
			list := make([]expireClear, len(cacheList))
			for i, item := range cacheList {
				list[i] = item
			}
			lock.Unlock()

			for _, item := range list {
				item.clearExpire(now.UnixMilli())
			}
		}
	}()
}

type readData[K comparable, V any] func(ctx context.Context, key K) (*V, error)

// item 缓存Item
type item[K comparable, V any] struct {
	lock      sync.RWMutex
	expire    atomic.Int64 //有效期
	val       *V
	call      readData[K, V]
	minExpire int64 //最小有效期（毫秒），默认 5分钟
	maxExpire int64 //最大有效期（毫秒），默认10分钟
}

// Get 获得值
func (i *item[K, V]) get(ctx context.Context, key K) (*V, error) {
	expire := i.expire.Load()
	if expire > -1 && expire < timestamp.Load() {
		i.lock.Lock()
		if expire < timestamp.Load() {
			i.randExpire()
			val, err := i.call(ctx, key)
			if err != nil {
				i.lock.Unlock()
				return nil, err
			}
			i.val = val
		}
		i.lock.Unlock()
	}

	i.lock.RLock()
	defer i.lock.RUnlock()
	i.randExpire()

	if nil == i.val {
		return nil, nil
	}
	value := *i.val
	return &value, nil
}

// Modify 修改设置值
func (i *item[K, V]) modify(ctx context.Context, key K, call func(ctx context.Context, key K, value V) (*V, error)) error {
	expire := i.expire.Load()
	if expire > -1 && expire < timestamp.Load() {
		i.lock.Lock()
		if expire < timestamp.Load() {
			i.randExpire()
			val, err := i.call(ctx, key)
			if err != nil {
				i.lock.Unlock()
				return err
			}
			i.val = val
		}
		i.lock.Unlock()
	}

	i.lock.RLock()
	defer i.lock.RUnlock()
	i.randExpire()

	var val V
	if nil == i.val {
		val = *new(V)
	} else {
		val = *i.val
	}
	var err error
	newVal, err := call(ctx, key, val)
	if err != nil {
		return err
	}
	i.val = newVal
	return nil
}

func (i *item[K, V]) set(val *V) {
	i.lock.Lock()
	i.val = val
	i.lock.Unlock()
}

func (i *item[K, V]) load(ctx context.Context, key K) (*V, error) {
	i.lock.Lock()
	defer i.lock.Unlock()
	val, err := i.call(ctx, key)
	if err != nil {
		return nil, err
	}
	i.randExpire()
	i.val = val
	return val, nil
}

// 设置一个随机的有效期
func (i *item[K, V]) randExpire() {
	expire := i.maxExpire - i.minExpire
	if expire > -1 {
		i.expire.Store(timestamp.Load() + rand.Int63n(expire+1) + i.minExpire)
	}
}

// NewMemoryCache 新建一个内存缓存
func NewMemoryCache[K comparable, V any](options ...Option[K, V]) *MemoryCache[K, V] {
	ret := &MemoryCache[K, V]{
		maps:      make(map[K]*item[K, V]),
		minExpire: int64(time.Minute * 5 / time.Millisecond),
		maxExpire: int64(time.Minute * 10 / time.Millisecond),
		call:      defaultReadCall[K, V],
	}

	for _, option := range options {
		option(ret)
	}

	lock.Lock()
	cacheList = append(cacheList, ret)
	lock.Unlock()
	return ret
}

// MemoryCache 内存缓存
type MemoryCache[K comparable, V any] struct {
	lock      sync.RWMutex
	maps      map[K]*item[K, V]
	call      readData[K, V]
	minExpire int64 //最小有效期（毫秒），默认 5分钟
	maxExpire int64 //最大有效期（毫秒），默认10分钟
}

// ReadCall 清理过期的缓存
func (c *MemoryCache[K, V]) clearExpire(now int64) {
	keys := make([]K, 0, len(c.maps))
	c.lock.Lock()
	defer c.lock.Unlock()

	for key, val := range c.maps {
		if val.expire.Load() < now {
			keys = append(keys, key)
		}
	}

	for _, key := range keys {
		delete(c.maps, key)
	}
}

// ReadCall 没有缓存时，读取原始数据
func (c *MemoryCache[K, V]) ReadCall(call readData[K, V]) {
	if call == nil {
		c.call = defaultReadCall[K, V]
	} else {
		c.call = call
	}
}

// Get 获得指定 Key的缓存
func (c *MemoryCache[K, V]) Get(ctx context.Context, key K) (*V, error) {
	c.lock.RLock()
	val, ok := c.maps[key]
	c.lock.RUnlock()
	if ok {
		return val.get(ctx, key)
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	val, ok = c.maps[key]
	if ok {
		return val.get(ctx, key)
	}

	temp := &item[K, V]{
		minExpire: c.minExpire,
		maxExpire: c.maxExpire,
		call:      c.call,
	}
	value, err := temp.load(ctx, key)
	if err != nil {
		return nil, err
	}

	c.maps[key] = temp
	return value, nil
}

// Del 删除指定 Key的缓存
func (c *MemoryCache[K, V]) Del(ctx context.Context, key K) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.maps, key)
	return nil
}

// Modify 修改指定KEY的内容
func (c *MemoryCache[K, V]) Modify(ctx context.Context, key K, call func(ctx context.Context, key K, value V) (*V, error)) error {
	c.lock.RLock()
	val, ok := c.maps[key]
	c.lock.RUnlock()
	if ok {
		return val.modify(ctx, key, call)
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	val, ok = c.maps[key]
	if ok {
		return val.modify(ctx, key, call)
	}

	temp := &item[K, V]{
		minExpire: c.minExpire,
		maxExpire: c.maxExpire,
		call:      c.call,
	}
	_, err := temp.load(ctx, key)
	if err != nil {
		return err
	}

	err = val.modify(ctx, key, call)
	if err != nil {
		return err
	}

	c.maps[key] = temp
	return nil
}

// Expire 设置有效期
func (c *MemoryCache[K, V]) Expire(key K, duration time.Duration) {
	c.lock.RLock()
	if val, ok := c.maps[key]; ok {
		if duration > 0 {
			val.expire.Store(int64(duration / time.Millisecond))
		} else {
			val.expire.Store(int64(-1))
		}
	}
	c.lock.RUnlock()
}

// Hash 判断KEY 是否存在
func (c *MemoryCache[K, V]) Hash(key K) bool {
	c.lock.RLock()
	_, ok := c.maps[key]
	c.lock.RUnlock()
	return ok
}

func defaultReadCall[K comparable, V any](ctx context.Context, key K) (*V, error) {
	return nil, nil
}
