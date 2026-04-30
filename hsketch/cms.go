package hsketch

import "math/rand"

const sketchCounters = 1 << 20 // 计数器数量（必须是2的幂）

// ===================== TinyLFU (4位 + 4路 CMS) =====================
//约1M个计数器（打包：0.5MB）

func NewCMSketch() *CMSketch {
	n := uint32(sketchCounters)
	// 每字节2个计数器
	return &CMSketch{
		table: make([]byte, n/2),
		mask:  n - 1,
	}
}

// CMSketch 每个字节存储2个计数器（低4位，高4位）
type CMSketch struct {
	table []byte
	mask  uint32 // 计数器数量-1
}

// get 获取索引处的4位计数器
func (c *CMSketch) get(idx uint32) uint8 {
	i := (idx & c.mask) >> 1
	b := c.table[i]
	if (idx & 1) == 0 {
		return b & 0x0F
	}
	return (b >> 4) & 0x0F
}

// inc 带饱和递增（最大值15）
func (c *CMSketch) inc(idx uint32) {
	i := (idx & c.mask) >> 1
	shift := (idx & 1) << 2
	b := c.table[i]
	v := (b >> shift) & 0x0F
	if v < 15 {
		v++
	}
	// 清除4位然后设置
	b &^= 0x0F << shift
	b |= v << shift
	c.table[i] = b
}

// Estimate 4路估算：取4个哈希值的最小值
func (c *CMSketch) Estimate(h uint64) uint8 {
	return min(
		min(c.get(uint32(h)), c.get(uint32(h^(h>>17)))),
		min(c.get(uint32(h^(h>>11))), c.get(uint32(h^(h>>5)))),
	)
}

// Add 递增所有4个计数器
func (c *CMSketch) Add(h uint64) {
	c.inc(uint32(h))
	c.inc(uint32(h ^ (h >> 17)))
	c.inc(uint32(h ^ (h >> 11)))
	c.inc(uint32(h ^ (h >> 5)))
}

// DecaySample O(1) 衰减通过随机采样（无全量扫描）
func (c *CMSketch) DecaySample(k int) {
	// 采样k个字节并将两个4位计数器都减半
	for i := 0; i < k; i++ {
		idx := rand.Intn(len(c.table))
		b := c.table[idx]
		// 分别将低半字节和高半字节减半
		low := (b & 0x0F) >> 1
		high := ((b >> 4) & 0x0F) >> 1
		c.table[idx] = (high << 4) | low
	}
}
