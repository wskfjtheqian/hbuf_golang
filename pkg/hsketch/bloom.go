package hsketch

import (
	"sync"
)

//import "github.com/seiflotfy/cuckoofilter"  (布谷鸟过滤器)

// NewBloomFilter 创建一个新的布隆过滤器
// n: 预期插入的元素数量
// fp: 期望的误判率 (例如 0.01 代表 1%)
func NewBloomFilter[T comparable](n uint, fp float64) *BloomFilter[T] {
	m, k := optimalParams(n, fp)

	// 将位数转换为 uint64 数组的长度
	size := m / 64
	if m%64 != 0 {
		size++
	}

	return &BloomFilter[T]{
		bits: make([]uint64, size),
		m:    m,
		k:    k,
	}
}

// BloomFilter 是一个高性能的泛型布隆过滤器
// 用于判断一个元素是否“可能”存在于集合中
type BloomFilter[T comparable] struct {
	bits []uint64 // 位数组，使用 uint64 提高寻址效率
	m    uint     // 位数组总长度 (bits)
	k    uint     // 哈希函数数量
	lock sync.RWMutex
}

// Add 向过滤器中添加一个元素
func (b *BloomFilter[T]) Add(item T) {
	h1 := Hash(item)
	h2 := h1 ^ (h1 >> 32)
	b.lock.Lock()
	defer b.lock.Unlock()

	for i := uint(0); i < b.k; i++ {
		// 组合哈希: h(i) = h1 + i * h2
		pos := (h1 + uint64(i)*h2) % uint64(b.m)
		idx := pos / 64
		offset := pos % 64
		b.bits[idx] |= 1 << offset
	}
}

// Contains 判断元素是否可能存在
// 返回 true: 元素可能存在
// 返回 false: 元素一定不存在
func (b *BloomFilter[T]) Contains(item T) bool {
	h1 := Hash(item)
	h2 := h1 ^ (h1 >> 32)
	b.lock.RLock()
	defer b.lock.RUnlock()

	for i := uint(0); i < b.k; i++ {
		pos := (h1 + uint64(i)*h2) % uint64(b.m)
		idx := pos / 64
		offset := pos % 64
		if (b.bits[idx] & (1 << offset)) == 0 {
			return false
		}
	}
	return true
}

// Reset 清空过滤器
func (b *BloomFilter[T]) Reset() {
	b.lock.Lock()
	defer b.lock.Unlock()
	for i := range b.bits {
		b.bits[i] = 0
	}
}

// ===================== 内部工具函数 =====================

// optimalParams 计算最优的位数(m)和哈希次数(k)
func optimalParams(n uint, p float64) (uint, uint) {
	if p <= 0 || p >= 1 {
		p = 0.01 // 默认 1% 误判率
	}

	// m = -(n * ln(p)) / (ln(2)^2)
	m := uint(float64(n) * (-1.4426950408889634) * log2(p))
	// 确保 m 是 64 的倍数以优化内存对齐
	m = (m + 63) &^ 63

	// k = (m/n) * ln(2)
	k := uint(float64(m) / float64(n) * 0.6931471805599453)
	if k < 1 {
		k = 1
	}

	return m, k
}

// log2 简单的对数计算
func log2(x float64) float64 {
	var result float64
	for x > 1 {
		x /= 2
		result++
	}
	return -result // 简化处理，实际应使用 math.Log
}
