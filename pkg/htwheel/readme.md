# HtWheel

HtWheel 是一个分层时间轮（Hierarchical Timing Wheel）定时调度器（见 [htwheel.go](htwheel.go)），用于在单进程内高效管理大量定时任务。所有轮层与任务索引只由一个驱动协程持有，外部 API 通过 channel 下发命令，因此无需任何锁即可安全并发调用。支持同 id 覆盖、任务查询/改期/取消、可配置刻度与执行池，并能优雅停止。另提供可插拔的持久化层（见 [persist.go](persist.go) / [store.go](store.go)），可把任务元数据存入数据库、文件或任意后端，定期预热进内存，进程重启后自动恢复。

## 特性

- **单驱动协程、无锁**：全部状态由唯一的驱动协程读写，`Get` / `Update` 等同步接口通过带响应 channel 的请求完成，不存在并发读写 map 的问题
- **分层时间轮**：短延时任务 O(1) 入轮；超出当前层量程的任务自动落到更粗的层，层数按需增长
- **同 id 覆盖**：`Schedule` 相同 id 会标记并丢弃旧任务，只保留最新一条
- **完整的任务操作**：`Get` / `TTL` / `Update` / `Cancel` 对任意层级的任务都生效（全局索引跨层可见）
- **不提前触发**：入轮按 tick 向上取整，回调不会早于计划时间执行，最多晚一个 tick
- **可配置**：tick、每层槽位数、worker 数均可通过 Option 调整
- **健壮的生命周期**：`Stop` 幂等，等待驱动协程退出并执行完已提交任务；worker 内 `recover`，单个任务 panic 不影响进程
- **执行与调度解耦**：命中的任务投递到 `WorkerPool` 异步执行，慢任务不会阻塞时间轮推进
- **可插拔持久化**：任务元数据可存入数据库/文件/自定义后端，按 `PollInterval` 定时扫描并把即将到期的任务预热进内存
- **处理器注册表**：回调无法序列化，用 `Name` 标识处理器、`Payload` 承载参数，重启后按名称还原执行逻辑
- **失败重试**：按记录里的剩余次数与间隔改期重试（可全局默认 + 单条覆盖，可选指数退避），等待期重启不丢，用尽后上报 `ErrRetriesExhausted`

## 文件结构

| 文件 | 说明 |
|---|---|
| [htwheel.go](htwheel.go) | 核心：`Task`、`WorkerPool`、`wheel`（单层时间轮）、`Scheduler`、`Option` |
| [persist.go](persist.go) | 持久化：`Record` 接口、泛型 `Store[R]`、`PersistentScheduler[R]`、`Handler`、重试策略 |
| [record.go](record.go) | 内置 `TaskRecord`：`Record` 接口的默认实现 |
| [store.go](store.go) | 内置存储：`MemoryStore`、`FileStore`（JSON，原子替换写入） |
| [htwheel_test.go](htwheel_test.go) | 测试：正常触发、跨层查询/改期/取消、覆盖语义、并发压测、Stop 幂等、远期任务 |
| [persist_test.go](persist_test.go) | 测试：持久化调度、外部插入、取消/改期、文件恢复、慢任务防重、处理器缺失重试 |
| [retry_test.go](retry_test.go) | 测试：重试成功/用尽、默认与任务级策略、退避、panic、等待期取消 |
| [crud_test.go](crud_test.go) | 测试：Schedule 新增/整体替换、UpdateWith 改名与参数、Remove 删除 |
| [deep_test.go](deep_test.go) | 测试：缓冲饱和、远期稳定、Stop 竞争、覆盖并发、覆盖率盲区 |

## 核心概念

时间轮由若干层 `wheel` 组成，每层有 `size` 个槽位（bucket）和一个游标 `cursor`：

- 第 0 层刻度为 `tick`，单圈覆盖 `tick × size`
- 每往上一层，刻度放大 `size` 倍，单圈覆盖也随之放大 `size` 倍
- 驱动协程每 `tick` 推进第 0 层一格；某层转满一圈时，才让上一层前进一格

默认配置（`tick=100ms`、`size=512`）下的覆盖范围：

| 层级 | 每格 | 单圈覆盖 |
|---|---|---|
| 0 | 100ms | 51.2s |
| 1 | 51.2s | 约 7.28 小时 |
| 2 | 约 7.28h | 约 155 天 |

任务入轮时选择「能装下它的最细一层」：延迟小于该层单圈覆盖即放入对应槽位，否则继续向更粗的层查找；超出所有层时再新建一层（会检测 tick 溢出并兜底）。任务所在槽位到期后若仍未到执行时间，会按当前时间重新安置，保证最终在计划时间附近触发。

## 快速开始

```go
package main

import (
	"fmt"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
)

func main() {
	s := htwheel.NewScheduler() // 默认 tick=100ms, size=512, workers=8
	defer s.Stop()

	s.Schedule(1, time.Now().Add(2*time.Second), func(id uint64, exec time.Time) {
		// exec 是该任务的计划执行时间，而非实际执行时间
		fmt.Printf("task %d fired, scheduled at %v\n", id, exec)
	})

	// 查询与剩余时间
	if task, ok := s.Get(1); ok {
		fmt.Println("expire:", time.Unix(0, task.Expire()))
	}
	if ttl, ok := s.TTL(1); ok {
		fmt.Println("ttl:", ttl)
	}

	// 提前到 500ms 后执行（保持原回调）
	s.Update(1, time.Now().Add(500*time.Millisecond))

	// 取消
	// s.Cancel(1)

	time.Sleep(3 * time.Second)
}
```

## 自定义配置

```go
s := htwheel.NewScheduler(
	htwheel.WithTick(10*time.Millisecond), // 最小时间刻度
	htwheel.WithSize(256),                 // 每层槽位数（最小 2）
	htwheel.WithWorkers(16),               // 执行任务的 worker 数
)
```

| Option | 默认值 | 说明 |
|---|---|---|
| `WithTick(d)` | `100ms` | 最小时间刻度，决定触发精度 |
| `WithSize(n)` | `512` | 每层槽位数，小于 2 时忽略；越大单层覆盖越广 |
| `WithWorkers(n)` | `8` | 执行回调的 worker 数量 |

## API

### Scheduler

| 方法 | 签名 | 说明 |
|---|---|---|
| `Schedule` | `(id uint64, t time.Time, fn func(uint64, time.Time)) bool` | 在 `t` 时刻执行 `fn`，同 id 覆盖旧任务；**异步入队**，返回 true 仅代表命令已投递 |
| `Update` | `(id uint64, t time.Time) bool` | 修改已有任务的执行时间，保留原回调；任务不存在时返回 false（同步） |
| `Get` | `(id uint64) (*Task, bool)` | 查询任务快照（同步） |
| `TTL` | `(id uint64) (time.Duration, bool)` | 返回剩余时间；任务不存在或已到期返回 false（同步） |
| `Cancel` | `(id uint64) bool` | 取消任务并返回是否命中（同步）；已进入执行队列的任务会在运行前再次检查取消标记 |
| `Stop` | `()` | 停止调度器，等待驱动协程退出并执行完已提交任务；可重复调用 |

### Task

| 方法 | 说明 |
|---|---|
| `Expire() int64` | 返回计划执行时间（UnixNano） |

回调签名 `func(id uint64, execTime time.Time)` 中的 `execTime` 为**计划执行时间**（即 `Schedule` 时传入的 `t`），可用于观测调度延迟或做超时判定。

## 并发与生命周期

- `Schedule` 是异步的：返回 true 表示命令已进入队列。若需确认任务已可查询，请轮询 `Get`。
- `Update`、`Get`、`TTL`、`Cancel` 是同步的：返回时结果已确定。
- `Stop` 之后 `Schedule` / `Update` / `Cancel` 返回 false，`Get` 返回 false；尚未入轮的命令会被丢弃。
- `Stop` 会等待已提交到执行池的任务执行完毕；回调中不应无限阻塞，否则 `Stop` 会一直等待。
- 单个任务 panic 会被 worker 捕获，不会导致进程崩溃。

## 精度与注意事项

- 触发精度为 `tick`，任务不会早于计划时间触发，最多晚一个 tick。
- 计划时间已经过去（`expire <= now`）的任务会立即提交执行，不再入轮，因此这类任务通常无法再被 `Get` / `Cancel` 命中。
- 超长延时任务会自动进入更粗的层级，层数按需增长，无需预先配置。
- 回调在 worker 协程中执行，避免在回调里做长时间阻塞操作，以免占满 worker。

## 持久化与自定义存储

内存时间轮在进程退出后会丢失任务。`PersistentScheduler` 在 `Scheduler` 之上增加了一层可插拔存储：**写操作落库，后台定时扫描把即将到期的任务预热进内存**。

### 为什么存的是元数据

Go 的函数值无法序列化，所以存储层保存的是任务描述 `Record`，执行逻辑通过「处理器名」在运行时还原。

`Record` 是一个**接口**，只需实现字段的 getter/setter；内置 `TaskRecord` 是默认实现：

```go
type Record interface {
	GetID() uint64
	GetExpire() time.Time
	GetName() string
	GetPayload() []byte
	GetRetries() int
	GetRetryDelay() time.Duration

	SetID(uint64)
	SetExpire(time.Time)
	SetName(string)
	SetPayload([]byte)
	SetRetries(int)
	SetRetryDelay(time.Duration)
}

// 内置默认实现（可直接使用，也可自定义结构体实现 Record 接口）
type TaskRecord struct {
	ID         uint64        `json:"id"`
	Expire     time.Time     `json:"expire"`
	Name       string        `json:"name"`
	Payload    []byte        `json:"payload,omitempty"`
	Retries    int           `json:"retries,omitempty"`
	RetryDelay time.Duration `json:"retry_delay,omitempty"`
}
```

启动时先 `Register` 注册处理器，再创建 `PersistentScheduler`，已到期或即将到期的任务就会被加载执行。

### 快速开始

```go
store := htwheel.NewFileStore("./data/tasks.json") // 也可换成自定义 DB 实现

// 泛型调度器：R 为记录类型
// 第三个参数是基础 ctx（可为 nil），后台扫描与异步执行由它派生
p := htwheel.NewPersistentScheduler(store,
	htwheel.NewScheduler(),
	context.Background(), // baseCtx：取消它将停止后台扫描
	htwheel.WithPollInterval(time.Second),
	htwheel.WithLookahead(30*time.Second),
)
defer p.Stop()

// 处理器在启动阶段注册（必须覆盖存储中出现的所有 Name）
// 返回 error 表示本次执行失败，会按记录里的重试策略改期重试
// 第三个参数是任务记录本身（*TaskRecord），可通过 GetPayload() 读取业务参数
p.Register("send_email", func(id uint64, exec time.Time, rec *htwheel.TaskRecord) error {
	var m Email
	if err := json.Unmarshal(rec.GetPayload(), &m); err != nil {
		return err
	}
	return sendEmail(m)
})

// 持久化调度：直接传入完整记录（含 Name/Payload/Expire/重试字段）
payload, _ := json.Marshal(Email{To: "a@b.com"})
if err := p.Schedule(ctx, &htwheel.TaskRecord{
	ID: 1001, Expire: time.Now().Add(time.Hour), Name: "send_email", Payload: payload,
}); err != nil {
	panic(err)
}

// 查询/改期/取消都直接作用于存储层，远期任务无需已加载
rec, ok, _ := p.Get(ctx, 1001)
fmt.Println(rec.GetExpire(), ok)
p.Update(ctx, 1001, time.Now().Add(10*time.Minute))
p.Cancel(ctx, 1001)
```

| 方法 | 说明 |
|---|---|
| `Register(name, h)` | 注册处理器 `func(id uint64, exec time.Time, rec *TaskRecord) error` |
| `Schedule(ctx, rec)` | 落库并预热：按 ID 新增或整体替换（直接写完整 Record，含 Name/Payload/Expire/重试字段） |
| `Update(ctx, id, at)` | 改期，其余字段保持不变 |
| `UpdateWith(ctx, id, at, opts...)` | 改期并可改 Name/Payload/重试策略 |
| `Get(ctx, id)` / `TTL(ctx, id)` | 读存储层的最新记录 / 距下次执行（或下次重试）的剩余时间 |
| `Cancel(ctx, id)` / `Remove(ctx, id)` | 删除记录并撤销内存调度，重试等待期同样有效 |
| `Stop()` | 停止扫描与内存调度，幂等 |
| `Scheduler()` | 取回底层内存 `Scheduler` |

### 任务的增删改

任务的完整增删改操作：

```go
// 增/改合一：Schedule 按 ID 新增或整体替换，直接写完整 Record（含重试字段）
p.Schedule(ctx, &htwheel.TaskRecord{
    ID: 1001, Expire: at, Name: "send_email", Payload: payload,
    Retries: 3, RetryDelay: 5 * time.Second,
})

// 改：改期 + 换处理器 + 换参数 + 调重试（可组合，也可只改其中一项）
p.UpdateWith(ctx, 1001, newTime,
    htwheel.SetName[*htwheel.TaskRecord]("send_sms"),           // 换处理器名
    htwheel.SetPayload[*htwheel.TaskRecord](smsPayload),        // 换业务参数
    htwheel.SetRetries[*htwheel.TaskRecord](5),                 // 单独调次数
    htwheel.SetRetryDelay[*htwheel.TaskRecord](time.Minute),    // 单独调间隔
)

// 删：取消任务（Cancel 与 Remove 等价，幂等）
p.Cancel(ctx, 1001)   // 或 p.Remove(ctx, 1001)
```

可用的 `TaskOption`（仅 `UpdateWith` 使用，需显式指定类型参数）：

| Option | 说明 |
|---|---|
| `WithTaskRetry[R](retries, delay)` | 覆盖重试次数与首次间隔（`retries=0` 即关闭重试） |
| `SetName[R](name)` | 修改处理器名 |
| `SetPayload[R](payload)` | 修改业务参数（传 `nil` 清空） |
| `SetRetries[R](n)` | 修改剩余重试次数 |
| `SetRetryDelay[R](d)` | 修改重试间隔 |

要点：

- `Schedule` 是「有则整体替换、无则新增」，`Record` 里的 `Name`/`Payload`/`Retries`/`RetryDelay` 都会原样落库，不套用全局默认策略；空 `Name` 会报错。
- `Update`（不带 With）只改执行时间，其余字段不变；`UpdateWith` 可组合多个 `TaskOption` 精准修改任意字段。
- 改处理器名后，任务到期会按新 `Name` 查找处理器执行，旧处理器的内存任务会被同 ID 覆盖。
- 所有增删改都先写存储层、再更新内存时间轮，因此即使任务尚未预热、或正处于重试等待期，也能正确增删改。

### Store 接口

`Store` 是泛型接口，方法带 `context.Context`，实现须并发安全：

```go
type Store[R Record] interface {
	Put(ctx context.Context, rec R) error
	Get(ctx context.Context, id uint64) (R, bool, error)
	Delete(ctx context.Context, id uint64) error                       // 不存在不视为错误
	Scan(ctx context.Context, before time.Time, limit int) ([]R, error) // Expire <= before，按 Expire 升序
}
```

| 内置实现 | 说明 |
|---|---|
| `NewMemoryStore()` | map 实现，进程退出即丢失，适合测试或作为参考（实现 `Store[*TaskRecord]`） |
| `NewFileStore(path)` | JSON 数组存单文件，每次变更原子重写（临时文件 + rename），适合万级以内、单写者场景 |

### 自定义存储：数据库示例

以 MySQL/PostgreSQL 为例，表结构与实现大致如下（关键是 `expire_at` 上要有索引）：

```sql
CREATE TABLE scheduled_task (
  id          BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  expire_at   BIGINT          NOT NULL,
  name        VARCHAR(128)    NOT NULL,
  payload     BLOB            NULL,
  retries     INT             NOT NULL DEFAULT 0,
  retry_delay BIGINT          NOT NULL DEFAULT 0, -- 纳秒
  KEY idx_expire (expire_at)
);
```

```go
// SQLStore 实现 Store[*htwheel.TaskRecord]
type SQLStore struct{ db *sql.DB }

func (s *SQLStore) Put(ctx context.Context, r *htwheel.TaskRecord) error {
	_, err := s.db.ExecContext(ctx,
		`REPLACE INTO scheduled_task(id, expire_at, name, payload, retries, retry_delay) VALUES(?,?,?,?,?,?)`,
		r.ID, r.Expire.UnixMilli(), r.Name, r.Payload, r.Retries, int64(r.RetryDelay))
	return err
}

func (s *SQLStore) Get(ctx context.Context, id uint64) (*htwheel.TaskRecord, bool, error) {
	var ms, delay int64
	var name string
	var payload []byte
	var retries int
	err := s.db.QueryRowContext(ctx,
		`SELECT expire_at, name, payload, retries, retry_delay FROM scheduled_task WHERE id=?`, id,
	).Scan(&ms, &name, &payload, &retries, &delay)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &htwheel.TaskRecord{
		ID: id, Expire: time.UnixMilli(ms), Name: name, Payload: payload,
		Retries: retries, RetryDelay: time.Duration(delay),
	}, true, nil
}

func (s *SQLStore) Delete(ctx context.Context, id uint64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_task WHERE id=?`, id)
	return err
}

func (s *SQLStore) Scan(ctx context.Context, before time.Time, limit int) ([]*htwheel.TaskRecord, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, expire_at, name, payload, retries, retry_delay FROM scheduled_task
		 WHERE expire_at <= ? ORDER BY expire_at LIMIT ?`,
		before.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*htwheel.TaskRecord
	for rows.Next() {
		r := &htwheel.TaskRecord{}
		var ms, delay int64
		if err := rows.Scan(&r.ID, &ms, &r.Name, &r.Payload, &r.Retries, &delay); err != nil {
			return nil, err
		}
		r.Expire = time.UnixMilli(ms)
		r.RetryDelay = time.Duration(delay)
		out = append(out, r)
	}
	return out, rows.Err()
}

// 使用：传入底层 Scheduler 与基础 ctx
p := htwheel.NewPersistentScheduler(&SQLStore{db}, nil, ctx)
```

### 加载与执行语义

- **存储层是事实来源**：`Get` / `TTL` / `Update` 直接读存储，尚未预热进内存的远期任务同样可以查询和改期。
- **本地调度立即预热**：`Schedule` 写存储成功后马上放入内存时间轮，不必等待下一次轮询。
- **轮询负责恢复与外部写入**：后台每 `PollInterval` 执行一次 `Scan(now+Lookahead)`，把未加载的记录按 `ID` 预热进内存；直接写库的任务、进程重启后的存量任务都靠这一步恢复。
- **执行前二次校验**：`fire` 会重新读存储，若记录已删除（取消）或 `Expire` 已变化（改期），则跳过本次执行。
- **成功即删除**：处理器返回 `nil` 后删除记录。
- **失败按策略重试**：处理器返回 `error`（或 panic）时扣减重试次数并改期，详见「失败重试」。
- **至少一次（at-least-once）**：处理器 panic、或执行完但删除前进程崩溃，记录会保留并在下次轮询重试，因此处理器应尽量幂等。
- **处理器缺失不丢任务**：找不到同名处理器时保留记录并回调 `WithErrorHandler`，注册后下一轮重试，且不消耗重试次数。
- **慢任务防重**：执行期间有 `running` 标记，轮询不会把同一任务重复预热，避免并发重复执行。
- **不自动关闭 Store**：`Stop` 只停止扫描与内存调度；数据库连接等资源由调用方自行关闭。

### 失败重试

`Handler` 的返回值为 `error`，非 `nil` 即表示本次执行失败，调度器按记录里的策略处理：

```go
p.Register("send_email", func(id uint64, exec time.Time, rec *htwheel.TaskRecord) error {
	return sendEmail(rec.GetPayload()) // 返回 error 就会重试
})
```

失败后的动作：`Retries` 减 1 → `Expire = now + RetryDelay` → 写回存储并重新入轮。`Retries` 已经是 0 时，删除记录并通过 `WithErrorHandler` 上报 `ErrRetriesExhausted`。

**重试是改期重排，不是原地忙重试**：任务回到时间轮（必要时进入更粗的层），等待期间进程重启也不会丢，因为次数与间隔都落在记录里。

策略分两层：全局默认值 + 单条任务覆盖。

```go
p := htwheel.NewPersistentScheduler(store, nil, ctx,
	htwheel.WithDefaultRetry(3, 5*time.Second), // 重试间隔兜底：任务未设 RetryDelay 时用 5s
	htwheel.WithRetryBackoff(2),                // 间隔按 5s → 10s → 20s 递增（可选）
	htwheel.WithMaxRetryDelay(time.Minute),     // 退避上限（可选）
	htwheel.WithErrorHandler(func(err error) {
		if errors.Is(err, htwheel.ErrRetriesExhausted) {
			log.Printf("任务彻底失败: %v", err) // 可在此落库留痕
		}
	}),
)

// 单条任务：在记录里直接写重试字段（5 次重试、间隔 30s）
p.Schedule(ctx, &htwheel.TaskRecord{
	ID: 1001, Expire: at, Name: "send_email", Payload: payload,
	Retries: 5, RetryDelay: 30 * time.Second,
})

// 关闭单条任务的重试
p.Schedule(ctx, &htwheel.TaskRecord{
	ID: 1002, Expire: at, Name: "send_email", Payload: payload,
	Retries: 0, RetryDelay: 0,
})

// 人工再给几次机会，或改期时一并重置策略
p.UpdateWith(ctx, 1001, nextRun, htwheel.WithTaskRetry[*htwheel.TaskRecord](3, time.Minute))
```

要点：

- `Retries`、`RetryDelay` 是记录字段，直接写库的任务同样生效；`Retries` 为 0 表示失败即放弃。
- `RetryDelay` 的含义是「本次失败后等待多久」。开启退避时，每次重试会把增长后的间隔写回记录，重启后继续沿用当前档位。
- 每次失败尝试都会上报一次错误（含「retry in 5s (2 left)」信息），便于观测；用尽时上报的错误可被 `errors.Is(err, ErrRetriesExhausted)` 识别。
- 处于重试等待期的任务可以 `Cancel`，也可以 `Update` 改期；执行期间被取消/改期的任务不会用旧状态覆盖记录。
- 间隔有 1ms 下限，避免 `RetryDelay=0` 时退化成忙循环。
- `Schedule` 直接写完整记录（含重试字段）；`Update` 只改时间、保留原重试次数；`UpdateWith` 才接受 `TaskOption` 改策略。

### 配置项

| Option | 默认值 | 说明 |
|---|---|---|
| `WithPollInterval(d)` | `1s` | 扫描存储层的间隔 |
| `WithLookahead(d)` | `30s` | 预热窗口：加载 `Expire <= now+d` 的任务 |
| `WithBatchSize(n)` | `256` | 单次扫描最多读取的条数 |
| `WithDefaultRetry(retries, delay)` | `0, 0` | 重试间隔兜底：任务 `RetryDelay<=0` 时用 `delay` 作为重试间隔 |
| `WithRetryBackoff(factor)` | `1` | 重试间隔增长倍数，`2` 表示 5s → 10s → 20s |
| `WithMaxRetryDelay(d)` | 不限 | 退避后的间隔上限 |
| `WithErrorHandler(fn)` | 无 | 存储/处理器错误回调 |

任务级配置通过 `TaskOption` 传入 `UpdateWith`（完整列表见「任务的增删改」一节）：

| Option | 说明 |
|---|---|
| `WithTaskRetry(retries, delay)` | 覆盖该任务的重试次数与首次间隔（`retries=0` 即关闭重试） |

`NewPersistentScheduler(store, s, baseCtx, opts...)` 的 `s` 传 `nil` 时使用默认 `NewScheduler()`；`baseCtx` 是后台扫描与异步执行所使用的基础上下文（传 `nil` 时回退到 `context.Background()`）。所有公开方法（`Schedule`/`Get`/`Update`/`Cancel` 等）都要求显式传入 `ctx`，存储访问从上层一路传递，不再在内部创建 `context.Background()`。也可以通过 `p.Scheduler()` 取回底层实例。

## 测试

```bash
go test -race ./pkg/htwheel/
```

测试覆盖：正常触发与执行时间参数、跨层 `Get`/`TTL`/`Update`、跨层 `Cancel`、同 id 覆盖、取消后更新、并发读写压测、`Stop` 幂等、极端远期任务与多层安置；持久化部分覆盖调度与执行、外部写入、取消/改期、文件重启恢复、慢任务防重、处理器缺失重试；重试部分覆盖失败后成功、重试用尽、任务级策略、退避增长、panic 触发重试、等待期取消、重试字段持久化；增删改部分覆盖 Schedule 新增/整体替换/空名报错、UpdateWith 改处理器名与参数、参数清空、Remove 删除、停止后增删改报 ErrClosed。建议始终带 `-race` 运行。
