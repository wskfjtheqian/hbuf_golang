package hutl

import "runtime"

// Task 表示一个泛型任务，包含上下文、执行函数和响应通道
type Task[T any, R any] struct {
	Ctx  T
	Exec func(T) (R, error)
	Resp chan Result[R]
}

// Result 表示任务的执行结果，包含返回值和错误信息
type Result[R any] struct {
	Val R
	Err error
}

// ShardQueue 表示一个分片队列，用于并行处理任务
type ShardQueue[T any, R any] struct {
	shards []chan Task[T, R]
	size   uint32
}

// NewShardQueue 创建一个新的分片队列，根据CPU核心数决定分片数量
func NewShardQueue[T any, R any](queueSize int) *ShardQueue[T, R] {
	shardNum := runtime.NumCPU() * 4

	q := &ShardQueue[T, R]{
		shards: make([]chan Task[T, R], shardNum),
		size:   uint32(shardNum),
	}

	for i := 0; i < shardNum; i++ {
		ch := make(chan Task[T, R], queueSize)
		q.shards[i] = ch

		go func(c chan Task[T, R]) {
			for task := range c {
				val, err := task.Exec(task.Ctx)
				task.Resp <- Result[R]{Val: val, Err: err}
			}
		}(ch)
	}

	return q
}

// Submit 提交一个任务到队列中，并根据键值选择分片
func (q *ShardQueue[T, R]) Submit(key int64, ctx T, exec func(T) (R, error)) (R, error) {
	shard := key % int64(q.size)

	resp := make(chan Result[R], 1)
	q.shards[shard] <- Task[T, R]{
		Ctx:  ctx,
		Exec: exec,
		Resp: resp,
	}

	result := <-resp
	return result.Val, result.Err
}
