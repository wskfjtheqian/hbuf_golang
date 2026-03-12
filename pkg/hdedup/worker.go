package hdedup

import (
	"hash/fnv"
)

// Event 表示一个事件，包含键和值
type Event struct {
	Key   string
	Value string
}

// Worker 表示一个工作单元，负责处理事件并存储分片数据
type Worker struct {
	baseDir string
	wal     *WAL

	input chan Event

	shards map[string]*Shard
}

// NewWorker 创建一个新的 Worker 实例
func NewWorker(baseDir string, wal *WAL) *Worker {
	w := &Worker{
		baseDir: baseDir,
		wal:     wal,
		input:   make(chan Event, 100000),
		shards:  make(map[string]*Shard),
	}
	go w.loop()
	return w
}

// loop 处理输入通道中的事件，计算哈希值并更新分片数据
func (w *Worker) loop() {
	for e := range w.input {
		hash := hash32(e.Value)

		_ = w.wal.Append(e.Key, hash)

		shard, ok := w.shards[e.Key]
		if !ok {
			shard, _ = LoadShard(w.baseDir, e.Key)
			w.shards[e.Key] = shard
		}

		shard.Add(hash)
	}
}

// Add 将事件添加到输入通道中
func (w *Worker) Add(key, value string) {
	w.input <- Event{Key: key, Value: value}
}

// Flush 刷新所有分片数据到存储中
func (w *Worker) Flush() {
	for _, s := range w.shards {
		_ = s.Save()
	}
}

// hash32 计算字符串的 32 位哈希值
func hash32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
