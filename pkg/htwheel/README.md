# htwheel

一个基于 Go 泛型实现的**分层时间轮调度器**（Hierarchical Timing Wheel），用于高效调度大量定时 / 延时任务，并支持任务持久化与进程重启恢复。

## 特性

- **分层时间轮**：单层只能覆盖 `tick × size` 的时间窗口，分层后 tick 逐层放大 `size` 倍，用「少量槽位 + 多层」覆盖任意远的到期时间，入队复杂度 O(1)。
- **单协程驱动**：所有可变状态只由唯一驱动协程读写，外部 API 通过 channel 投递命令，从结构上避免锁竞争与 map 并发读写。
- **失败重试**：以回调 `panic` 作为失败信号，按策略（次数 + 间隔）自动重试，重试复用时间轮机制。
- **任务持久化**：`ScheduleWithParam` 提交的任务可写入 `Store`，进程重启后经 `Load` 恢复执行。
- **远任务下沉**：设置存储层后，到期时间超过扫描窗口（`ahead`）的任务只写存储、不常驻内存时间轮，临近到期时再拉回，减少内存占用。

## 快速开始

```go
package main

import (
	"fmt"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
)

func main() {
	// P 类型参数占位，纯内存调度用任意实现了 Recoverable 的类型即可。
	s := htwheel.NewScheduler[param](
		htwheel.WithTick[param](10 * time.Millisecond),
	)
	defer s.Stop()

	s.Schedule(1, time.Now().Add(50*time.Millisecond), func(id uint64, at time.Time) {
		fmt.Println("fired:", id, at)
	})

	time.Sleep(time.Second)
}
```

`Scheduler` 是泛型类型，类型参数 `P` 必须实现 `Recoverable` 接口。即使不使用持久化，也需要提供一个占位类型。

```go
type param struct{}

func (param) ID() uint64                          { return 0 }
func (param) Expire() time.Time                   { return time.Time{} }
func (param) Recover() func(uint64, time.Time)    { return nil }
```

## 核心 API

### 调度

| 方法 | 说明 |
|---|---|
| `Schedule(id, t, fn)` | 在 `t` 时刻执行 `fn`（同 id 覆盖旧任务），纯内存、不持久化 |
| `ScheduleWithParam(id, t, fn, p)` | 与 `Schedule` 相同，但额外携带参数 `p` 并持久化到 `Store`（若已配置） |
| `ScheduleTask(task)` | 提交已构造好的任务，可携带单条任务的重试策略 |
| `WithTaskRetry(retries, delay)` | 覆盖单条任务的重试策略（配合 `ScheduleTask` 使用） |

`fn` 签名为 `func(id uint64, at time.Time)`，`at` 是计划执行时间。

> 入轮是异步的：`Schedule` 返回 `true` 仅代表命令已投递，若要确认任务已可查询，请用 `Get` 轮询。

### 查询与变更

| 方法 | 说明 |
|---|---|
| `Get(id)` | 查询任务快照（只读），含已下沉到存储的远任务 |
| `TTL(id)` | 返回剩余时间，任务不存在或已到期返回 `false` |
| `Update(id, t)` | 修改执行时间（保持原回调），对下沉任务同样生效 |
| `Cancel(id)` | 取消任务并返回是否命中 |
| `Stop()` | 停止调度器，等待驱动协程退出并执行完已提交任务，可重复调用 |

## 重试

失败判定标准是**回调发生 `panic`**：正常返回视为成功，`panic` 触发重试。重试策略有三个来源，优先级从高到低：

1. 单任务 `WithTaskRetry(n, delay)`
2. 全局默认 `WithDefaultRetry(n, delay)`
3. 未配置 → 不重试

重试的本质是「重新入轮」：捕获 `panic` 后若还有剩余次数，则减一并构造新任务，延后 `delay`（`delay ≤ 0` 时兜底为 `tick`）重新放入时间轮；次数用尽则上报 `ErrRetriesExhausted` 并丢弃任务。

```go
s := htwheel.NewScheduler[param](
	htwheel.WithDefaultRetry[param](3, 100*time.Millisecond),
	htwheel.WithErrorHandler[param](func(err error) {
		if errors.Is(err, htwheel.ErrRetriesExhausted) {
			// 重试已用尽，任务被最终放弃
		}
	}),
)
```

## 持久化

通过 `Store[P]` 接口实现跨进程持久化，重启后自动恢复执行。

### Recoverable 接口

类型参数 `P` 需要实现 `Recoverable`，让存储层能仅凭参数本身重建「到期时间」与「执行回调」：

```go
type Recoverable interface {
	ID() uint64
	Expire() time.Time
	Recover() func(uint64, time.Time) // 返回 nil 表示不可重放，加载时跳过
}
```

### Store 接口

```go
type Store[P Recoverable] interface {
	Add(id uint64, t time.Time, task P) error
	Remove(id uint64) bool
	Update(id uint64, t time.Time, task P) bool
	Load(before time.Time, limit int) ([]P, error)
}
```

- `Load` 返回到期时间在 `before` 之前、按到期时间升序的前 `limit` 条。
- `Add` / `Remove` / `Update` 由调度器自动调用，无需业务侧手动写入。

### 完整示例

```go
// 参数类型：携带 id、到期时间，并能重建回调。
type reminder struct {
	ID_    uint64
	When   time.Time
	Msg    string
}

func (r reminder) ID() uint64        { return r.ID_ }
func (r reminder) Expire() time.Time { return r.When }
func (r reminder) Recover() func(uint64, time.Time) {
	return func(id uint64, at time.Time) {
		fmt.Println("recovered:", r.Msg)
	}
}

// 内存实现的 Store（示例，生产环境可换成数据库 / Redis）。
type memStore map[uint64]reminder

func (m memStore) Add(id uint64, t time.Time, task reminder) error {
	m[id] = task
	return nil
}
func (m memStore) Remove(id uint64) bool {
	if _, ok := m[id]; ok {
		delete(m, id)
		return true
	}
	return false
}
func (m memStore) Update(id uint64, t time.Time, task reminder) bool {
	if _, ok := m[id]; !ok {
		return false
	}
	m[id] = task
	return true
}
func (m memStore) Load(before time.Time, limit int) ([]reminder, error) {
	var out []reminder
	for _, r := range m {
		if r.When.Before(before) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.Before(out[j].When) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

s := htwheel.NewScheduler[reminder](
	htwheel.WithStore[reminder](memStore{}),
	htwheel.WithPollInterval[reminder](time.Second),
	htwheel.WithLookahead[reminder](30*time.Second),
)

s.ScheduleWithParam(1, time.Now().Add(time.Hour), func(id uint64, at time.Time) {
	fmt.Println("run")
}, reminder{ID_: 1, When: time.Now().Add(time.Hour), Msg: "hello"})
```

### 远任务下沉

设置存储层后，所有持久化任务都会写存储；但「到期时间 - 现在 > `ahead`」的**远任务**只写存储、不入内存时间轮，待临近到期时由 `Load` 扫描拉回执行，以减少常驻内存的任务量。

`ahead` 通过 `WithLookahead` 配置，扫描间隔通过 `WithPollInterval` 配置。远任务在下沉期间仍可通过 `Cancel` / `Update` / `Get` 正常操作。

## 配置项

| Option | 默认值 | 说明 |
|---|---|---|
| `WithTick(d)` | 100ms | 最小时间刻度 |
| `WithSize(n)` | 512 | 每层时间轮槽位数（最小 2） |
| `WithWorkers(n)` | 8 | 执行任务的 worker 数量 |
| `WithStore(s)` | 无 | 任务存储 |
| `WithPollInterval(d)` | 1s | 扫描存储层的间隔 |
| `WithLookahead(d)` | 30s | 扫描窗口（也是「远任务」的判定阈值） |
| `WithBatchSize(n)` | 256 | 单次扫描的最大条数 |
| `WithDefaultRetry(n, d)` | 不重试 | 默认重试策略 |
| `WithErrorHandler(fn)` | 忽略 | 存储层 / 处理器错误回调 |

## 并发模型

```
外部协程               驱动协程（唯一读写状态）               worker 协程池
   │                        │                                  │
   ├─ Schedule ─── addCh ──►├─ add / place / advance ──submit──►├─ 执行 fn
   ├─ Cancel ───── cancelCh►├─ cancel                          │
   ├─ Get ──────── getCh ──►├─ get                             │
   ├─ Update ───── updCh ──►├─ update                          │
   │                        │◄────────── finishCh ─────────────┤（执行完成清理持久化）
   │                        ├─ loadTasks ◄── Store.Load（周期性轮询）
```

- 所有可变状态（各层时间轮、`index` / `sunk` / `loaded` 索引）只由驱动协程读写，天然串行。
- 查询 / 更新通过「带响应 channel 的请求」完成同步等待。
- 任务执行在固定大小的 worker 池中进行，回调 `panic` 被捕获，不会拖垮进程。
- 存储的写入（`Add` / `Remove` / `Update`）也统一在驱动协程内完成，实现方无需额外加锁。

## 注意事项

- **重试边界**：`ScheduleTask` 套用默认重试时，判断条件是 `task.retries == 0`，无法区分「用户显式设 0」与「未设置」。若任务显式用 `WithTaskRetry(0, ...)`，会被全局默认覆盖。
- **回调签名**：`fn` 与 `Recover` 返回的回调都接收 `(id uint64, at time.Time)`，`at` 为计划执行时间。
- **`fn` 不可序列化**：持久化只保存参数 `P`，回调靠 `P.Recover()` 重建，因此 `ScheduleWithParam` 传入的 `fn` 在重启后不会被保留。
