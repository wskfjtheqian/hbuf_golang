package hdedup

import (
	"fmt"
	"time"

	"github.com/RoaringBitmap/roaring"
)

// Engine 是去重引擎的结构体，包含一个 Worker 实例用于执行具体任务
type Engine struct {
	worker *Worker
}

// NewEngine 创建一个新的去重引擎实例，接收基础目录作为参数，并返回引擎实例和可能的错误
func NewEngine(baseDir string) (*Engine, error) {
	wal, err := OpenWAL(baseDir + "/wal.log")
	if err != nil {
		return nil, err
	}

	worker := NewWorker(baseDir, wal)

	// WAL Replay 重放 WAL 日志中的操作，将数据加载到 Worker 的分片中
	err = wal.Replay(func(key string, hash uint32) {
		worker.shards[key], _ = LoadShard(baseDir, key)
		worker.shards[key].Add(hash)
	})
	if err != nil {
		return nil, err
	}

	return &Engine{
		worker: worker,
	}, nil
}

// Add 向引擎中添加新的记录，接收频道、类别、用户ID和时间作为参数
func (e *Engine) Add(channel, category, uid string, t time.Time) {
	day := Day(t)
	key := channel + "|" + category + "|" + day
	e.worker.Add(key, uid)
}

// Flush 刷新引擎中的数据，将未写入的数据持久化
func (e *Engine) Flush() {
	e.worker.Flush()
}

func (e *Engine) getShard(key string) (*Shard, error) {
	shard, ok := e.worker.shards[key]
	if ok {
		return shard, nil
	}
	return LoadShard(e.worker.baseDir, key)
}

func (e *Engine) buildKey(channel, category, date string) string {
	return fmt.Sprintf("%s|%s|%s", channel, category, date)
}

// DayCount 查询单天 UV
func (e *Engine) DayCount(channel, category string, t time.Time) uint64 {
	key := e.buildKey(channel, category, Day(t))
	shard, _ := e.getShard(key)
	return shard.Count()
}

// WeekCount 查询周 UV（多天合并）
func (e *Engine) WeekCount(channel, category string, t time.Time) uint64 {
	start := t.AddDate(0, 0, -int(t.Weekday()))

	result := roaring.New()

	for i := 0; i < 7; i++ {
		day := Day(start.AddDate(0, 0, i))
		key := e.buildKey(channel, category, day)

		shard, _ := e.getShard(key)
		result.Or(shard.bitmap)
	}

	return result.GetCardinality()
}

// MonthCount 月统计
func (e *Engine) MonthCount(channel, category string, t time.Time) uint64 {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	result := roaring.New()

	for d := first; d.Month() == t.Month(); d = d.AddDate(0, 0, 1) {
		key := e.buildKey(channel, category, Day(d))
		shard, _ := e.getShard(key)
		result.Or(shard.bitmap)
	}

	return result.GetCardinality()
}

// MultiChannelCount 多维组合统计
func (e *Engine) MultiChannelCount(channels []string, category string, t time.Time) uint64 {

	result := roaring.New()

	for _, ch := range channels {
		key := e.buildKey(ch, category, Day(t))
		shard, _ := e.getShard(key)
		result.Or(shard.bitmap)
	}

	return result.GetCardinality()
}

// OverlapCount 交集统计（重叠用户）
func (e *Engine) OverlapCount(ch1, ch2, category string, t time.Time) uint64 {

	key1 := e.buildKey(ch1, category, Day(t))
	key2 := e.buildKey(ch2, category, Day(t))

	s1, _ := e.getShard(key1)
	s2, _ := e.getShard(key2)

	result := roaring.And(s1.bitmap, s2.bitmap)

	return result.GetCardinality()
}

// RangeCount 任意时间区间统计
func (e *Engine) RangeCount(channel, category string, start, end time.Time) uint64 {

	result := roaring.New()

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := e.buildKey(channel, category, Day(d))
		shard, _ := e.getShard(key)
		result.Or(shard.bitmap)
	}

	return result.GetCardinality()
}
