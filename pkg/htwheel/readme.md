# HTWheel

HTWheel 是一个**泛型分层时间轮调度器**（见 [htwheel.go](htwheel.go)）。以毫秒为最小刻度，支持从「几十毫秒后」到「数年后」的任务定时执行，内置固定大小 worker 池、失败重试、取消，以及基于 `Loader` 的持久化加载与周期轮询补齐。除项目内的 `hlog` / `htime` 外无外部依赖。

```
Schedule / Loader ──channel──► 驱动协程(分层时间轮) ──到期──► WorkerPool ──► Handler
                                      ▲                          │
                                      └──── 重试 / 完成回执 ────────┘
```

## 特性

- **泛型任务参数**：`Scheduler[P]` / `Handler[P]` / `Task[P]` 全程泛型，任务参数类型安全，无需 `any` 断言
- **分层时间轮**：单层槽位不够时按需追加更粗粒度的一层（tick 逐层放大 `size` 倍），可覆盖极远未来；`int64` 溢出时用最粗一层兜底
- **单驱动协程、无锁**：所有状态（各层时间轮、全局索引、加载去重表）只由唯一的驱动协程读写；外部 API 通过 channel 投递命令，查询用带响应 channel 的请求完成
- **并发安全的 API**：`Schedule` / `ScheduleTask` / `Update` / `Get` / `TTL` / `Cancel` / `Stop` 均可并发调用；`Stop` 后所有写入立即失效，不存在「向已关闭 channel 发送」
- **WorkerPool**：固定大小 goroutine 池，`Submit` 与 `Stop` 串行化；单个任务 panic 被 `recover` 兜住，不拖垮进程也不泄漏 `WaitGroup` 计数
- **失败重试**：`handler` 返回 `error` 或 panic 均触发重试；支持全局默认策略 + 单条任务覆盖，重试重新入轮而非原地阻塞
- **持久化恢复**：`Loader` 从存储层加载「未来扫描窗口内」的任务，按 `id + expire` 去重；任务执行成功后回执驱动协程清理去重记录，避免重启后被重新拉回执行
- **周期轮询**：按 `poll` 间隔扫描存储，配合 `lookahead` 窗口把远期任务提前纳入内存轮
- **统一时间源**：时间推进、到期判断均使用 `htime.NowTime()`（毫秒精度的缓存时钟），不依赖 `time.Now()` 的调用抖动

## 文件结构

| 文件 | 说明 |
|---|---|
| [htwheel.go](htwheel.go) | 全部实现：类型定义、Option、WorkerPool、时间轮、Scheduler 驱动逻辑 |

## 快速开始

### 基本定时

```go
s := htwheel.NewScheduler[string](func(
	ctx context.Context, id uint64, name string,
	execTime time.Time, retries int, param string,
) error {
	fmt.Printf("task %s (id=%d, param=%s) fired at %v\n", name, id, param, execTime)
	return nil
})
defer s.Stop()

// 500ms 后执行；同 id 再次 Schedule 会覆盖旧任务
s.Schedule(1, "greet", time.Now().Add(500*time.Millisecond), "hello")
time.Sleep(time.Second)
```

`Schedule` 是**异步入轮**：返回仅代表命令已投递到驱动协程，如需确认任务已可查询，用 `Get` 轮询。

### 失败重试

```go
// 全局默认：所有新任务失败后最多重试 3 次，每次间隔 200ms
s := htwheel.NewScheduler[int](
	handler,
	htwheel.WithDefaultRetry[int](3, 200*time.Millisecond),
)

// 单条任务覆盖：先取回已有任务，设置重试策略后重新提交即可覆盖原任务
if t, ok := s.Get(1); ok {
	t.WithTaskRetry(5, time.Second)
	s.ScheduleTask(t)
}
```

`handler` 返回非 nil `error` 即视为失败，与 panic 走同一重试路径。`retries` 为 `int` 类型、逐次递减，减到 0 后放弃。

### 从存储恢复（Loader）

```go
loader := func(ctx context.Context, before time.Time, limit int) ([]*htwheel.LoaderTask[int64], error) {
	// 从 DB/Redis 查询 expire <= before 的前 limit 条待执行任务
	rows, err := queryPending(ctx, before, limit)
	if err != nil {
		return nil, err
	}
	tasks := make([]*htwheel.LoaderTask[int64], 0, len(rows))
	for _, r := range rows {
		tasks = append(tasks, htwheel.NewLoaderTask(r.ID, r.Name, r.Expire, r.Param))
	}
	return tasks, nil
}

s := htwheel.NewScheduler[int64](
	handler,
	htwheel.WithLoader(loader),
	htwheel.WithPollInterval(2*time.Second),
	htwheel.WithLookahead(30*time.Second),
	htwheel.WithBatchSize(500),
)
```

> 持久化写入与成功后的记录删除由业务侧负责：`Handler` 执行成功时删除存储中的任务记录；`Loader` 只负责把未执行的记录读回内存。调度器内部的 `loaded` 表仅用于内存去重，并在任务成功后自动清理。

## 配置项

通过 `NewScheduler` 的可变参数传入，`Option[P]` 为泛型函数：

| Option | 默认值 | 说明 |
|---|---|---|
| `WithTick` | `100ms` | 最小时间刻度（调度精度上限） |
| `WithSize` | `512` | 每层时间轮的槽位数，最小 2 |
| `WithWorkers` | `8` | 执行任务的 worker 数量 |
| `WithLoader` | `nil` | 任务加载器，用于重启恢复与周期轮询；为 nil 时不扫描存储 |
| `WithPollInterval` | `1s` | 扫描存储层的间隔 |
| `WithLookahead` | `30s` | 扫描窗口：`Expire` 早于 `now+lookahead` 的任务才会被读入内存 |
| `WithBatchSize` | `256` | 单次扫描加载的最大条数 |
| `WithDefaultRetry` | `0, 0` | 新建任务的默认重试次数与间隔（默认不重试） |

## API

### Scheduler

| 方法 | 说明 |
|---|---|
| `NewScheduler(handler, opts...)` | 创建并启动调度器（启动驱动协程） |
| `Schedule(id, name, t, p)` | 在 `t` 时刻执行任务，同 `id` 覆盖旧任务；异步入轮 |
| `ScheduleTask(task)` | 提交一条已构造的 `*Task[P]`，可携带单条重试策略；`task` 为 nil 直接忽略 |
| `Update(id, name, t, p)` | 修改已有任务的名称、执行时间与参数；对已下沉到存储的远任务同样生效 |
| `Get(id) (*Task[P], bool)` | 查询任务快照（只读，`*Task` 不可直接修改内部字段） |
| `TTL(id) (time.Duration, bool)` | 查询任务剩余时间；任务不存在或已到期返回 `false` |
| `Cancel(id)` | 取消任务；已进入执行队列的任务会在运行前二次检查取消标记（尽力而为） |
| `Stop()` | 停止调度器，等待驱动协程退出并执行完已提交任务；可重复调用 |

### Task / LoaderTask

| 类型 / 方法 | 说明 |
|---|---|
| `Task[P].Expire() int64` | 返回计划执行时间（Unix 毫秒） |
| `Task[P].WithTaskRetry(retries, delay)` | 覆盖单条任务的重试策略，返回 `*Task[P]` 便于链式调用 |
| `NewLoaderTask[P](id, name, expire, param)` | 构造一条从存储层加载的任务记录 |
| `Handler[P]` | 回调签名：`func(ctx, id, name, execTime, retries, param) error` |
| `Loader[P]` | 加载器签名：`func(ctx, before, limit) ([]*LoaderTask[P], error)` |

`Task` 含 `atomic.Bool` 字段，请使用指针、不要复制；`Expire()` 使用指针接收者正是为此。

## 工作原理

### 分层结构

`Scheduler.levels[0]` 是最细一层，之后每层的 `tick` 放大 `size` 倍。以默认 `tick=100ms`、`size=512` 为例：

| 层 | tick | 单层覆盖时长 |
|---|---|---|
| 0 | 100ms | ≈ 51.2s |
| 1 | 51.2s | ≈ 7.3h |
| 2 | ≈ 7.3h | ≈ 156 天 |
| 3 | ≈ 156 天 | ≈ 218 年 |

`place` 会把任务放进「能装下它的最细一层」：先用 `ticks = ceil(delay / tick)` 向上取整，保证**不会早于**计划时间触发（最多晚一个 tick）；已到期（`delay <= 0`）则立即提交执行。任务数量超过最粗一层容量时，对 `ticks` 取模落桶，靠后续 `advance` 重排逐步纠正。

### 驱动循环

驱动协程内的 `loop` 串行处理所有事件：`tick` 到点推进时间轮、命令 channel（增/删/查/改/完成）、存储轮询 tick、停止信号。时间推进用 `htime.NowTime()` 而非 ticker 计数，避免 ticker 抖动累积成延迟。

`advance` 推进某一层时，**先换新桶再遍历旧桶**，从而避免任务在遍历中被 `place` 重新写回正在迭代的 map。到期任务提交执行，未到期任务按当前时间重新安置；某层游标转满一圈时，递归推进上一层一格。

### 执行与重试

到期任务交给 `WorkerPool`，worker 在真正运行前再次检查取消标记。`execTask` 同时捕获 handler 返回的 `error` 与 panic：

- 成功 → 向 `finishCh` 投递完成回执，驱动协程清理 `loaded` 去重记录
- 失败且 `retries > 0` → 次数减 1，按 `delay`（为 0 时用 `tick`）重新入轮
- 失败且无剩余重试 → 同样投递完成回执，结束任务

完成回执采用**阻塞投递**（`finishCh` 有 1024 缓冲），保证「执行成功 → 必删去重记录」，避免进程重启后任务被 `Loader` 拉回重复执行。

### 加载与去重

`loadTasks` 每 `poll` 间隔调用一次 `Loader(ctx, now+ahead, batch)`，取回窗口内任务。`loaded[id] = expire` 记录已加载任务：若 `id` 相同且 `expire` 未变则跳过，避免重复入轮；`pruneLoaded` 周期性清理 `expire < now-ahead` 的陈旧条目，防止该 map 无限增长。

## 并发与生命周期

- **线程模型**：外部 API 只向 channel 投递请求，状态变更全部收敛到驱动协程，因此 `index`、`loaded`、各层 `buckets` 等 map 无需加锁
- **Stop 流程**：置 `stopped` → 关闭 `stopCh` → 等待驱动协程退出 → `wp.Stop()` 等待已提交任务执行完毕，可重复调用
- **WorkerPool**：`Submit` 在锁内完成 `Add(1)` 与投递，与 `Stop` 串行化，避免 `Add`/`Wait` 竞态；`exec` 用 `recover` 兜底，保证 `Done` 一定被调用
- **取消语义**：`Cancel` 标记 `canceled` 并从索引移除；已进入 worker 队列的任务在执行前二次检查，属于尽力而为，不保证绝对不执行

## 使用建议

- `Schedule` 后若需强一致确认，用 `Get` 轮询任务是否可见；不要假设返回即可查询
- 需要重启恢复时务必设置 `Loader`，并在 `Handler` 成功路径中删除外部存储记录
- 调度精度受 `tick` 限制，默认 100ms；需要更细精度时调小 `WithTick`，但会增加空转 tick 的 CPU 开销
- 远于 `lookahead` 窗口的任务不会长期驻留内存，依赖周期 `poll` 从存储重新加载；`poll` 越大，任务进入内存的延迟越高
