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

## 文件结构

| 文件 | 说明 |
|---|---|
| [htwheel.go](htwheel.go) | 核心：`Task`、`WorkerPool`、`wheel`（单层时间轮）、`Scheduler`、`Option` |
| [persist.go](persist.go) | 持久化：`Record`、`Store` 接口、`PersistentScheduler`、`Handler`、`PersistOption` |
| [store.go](store.go) | 内置存储：`MemoryStore`、`FileStore`（JSON，原子替换写入） |
| [htwheel_test.go](htwheel_test.go) | 测试：正常触发、跨层查询/改期/取消、覆盖语义、并发压测、Stop 幂等、远期任务 |
| [persist_test.go](persist_test.go) | 测试：持久化调度、外部插入、取消/改期、文件恢复、慢任务防重、处理器缺失重试 |

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

Go 的函数值无法序列化，所以存储层保存的是任务描述 `Record`，执行逻辑通过「处理器名」在运行时还原：

```go
type Record struct {
	ID      uint64    `json:"id"`
	Expire  time.Time `json:"expire"`
	Name    string    `json:"name"`    // 处理器名
	Payload []byte    `json:"payload"` // 业务参数，自行序列化
}
```

启动时先 `Register` 注册处理器，再创建 `PersistentScheduler`，已到期或即将到期的任务就会被加载执行。

### 快速开始

```go
store := htwheel.NewFileStore("./data/tasks.json") // 也可换成自定义 DB 实现

p := htwheel.NewPersistentScheduler(store, htwheel.NewScheduler(),
	htwheel.WithPollInterval(time.Second),
	htwheel.WithLookahead(30*time.Second),
)
defer p.Stop()

// 处理器在启动阶段注册（必须覆盖存储中出现的所有 Name）
p.Register("send_email", func(id uint64, exec time.Time, payload []byte) {
	var m Email
	_ = json.Unmarshal(payload, &m)
	sendEmail(m)
})

// 持久化调度：写存储 + 预热进内存
payload, _ := json.Marshal(Email{To: "a@b.com"})
if err := p.Schedule(1001, time.Now().Add(time.Hour), "send_email", payload); err != nil {
	panic(err)
}

// 查询/改期/取消都直接作用于存储层，远期任务无需已加载
rec, ok, _ := p.Get(1001)
fmt.Println(rec.Expire, ok)
p.Update(1001, time.Now().Add(10*time.Minute))
p.Cancel(1001)
```

### Store 接口

自定义存储只需实现四个方法（实现须并发安全）：

```go
type Store interface {
	Put(rec Record) error
	Get(id uint64) (Record, bool, error)
	Delete(id uint64) error                                    // 不存在不视为错误
	Scan(before time.Time, limit int) ([]Record, error)        // Expire <= before，按 Expire 升序
}
```

| 内置实现 | 说明 |
|---|---|
| `NewMemoryStore()` | map 实现，进程退出即丢失，适合测试或作为参考 |
| `NewFileStore(path)` | JSON 数组存单文件，每次变更原子重写（临时文件 + rename），适合万级以内、单写者场景 |

### 自定义存储：数据库示例

以 MySQL/PostgreSQL 为例，表结构与实现大致如下（关键是 `expire_at` 上要有索引）：

```sql
CREATE TABLE scheduled_task (
  id        BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  expire_at BIGINT          NOT NULL,
  name      VARCHAR(128)    NOT NULL,
  payload   BLOB            NULL,
  KEY idx_expire (expire_at)
);
```

```go
type SQLStore struct{ db *sql.DB }

func (s *SQLStore) Put(r htwheel.Record) error {
	_, err := s.db.Exec(
		`REPLACE INTO scheduled_task(id, expire_at, name, payload) VALUES(?,?,?,?)`,
		r.ID, r.Expire.UnixMilli(), r.Name, r.Payload)
	return err
}

func (s *SQLStore) Get(id uint64) (htwheel.Record, bool, error) {
	var ms int64
	var name string
	var payload []byte
	err := s.db.QueryRow(
		`SELECT expire_at, name, payload FROM scheduled_task WHERE id=?`, id,
	).Scan(&ms, &name, &payload)
	if err == sql.ErrNoRows {
		return htwheel.Record{}, false, nil
	}
	if err != nil {
		return htwheel.Record{}, false, err
	}
	return htwheel.Record{ID: id, Expire: time.UnixMilli(ms), Name: name, Payload: payload}, true, nil
}

func (s *SQLStore) Delete(id uint64) error {
	_, err := s.db.Exec(`DELETE FROM scheduled_task WHERE id=?`, id)
	return err
}

func (s *SQLStore) Scan(before time.Time, limit int) ([]htwheel.Record, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(
		`SELECT id, expire_at, name, payload FROM scheduled_task
		 WHERE expire_at <= ? ORDER BY expire_at LIMIT ?`,
		before.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []htwheel.Record
	for rows.Next() {
		var r htwheel.Record
		var ms int64
		if err := rows.Scan(&r.ID, &ms, &r.Name, &r.Payload); err != nil {
			return nil, err
		}
		r.Expire = time.UnixMilli(ms)
		out = append(out, r)
	}
	return out, rows.Err()
}
```

### 加载与执行语义

- **存储层是事实来源**：`Get` / `TTL` / `Update` 直接读存储，尚未预热进内存的远期任务同样可以查询和改期。
- **本地调度立即预热**：`Schedule` 写存储成功后马上放入内存时间轮，不必等待下一次轮询。
- **轮询负责恢复与外部写入**：后台每 `PollInterval` 执行一次 `Scan(now+Lookahead)`，把未加载的记录按 `ID` 预热进内存；直接写库的任务、进程重启后的存量任务都靠这一步恢复。
- **执行前二次校验**：`fire` 会重新读存储，若记录已删除（取消）或 `Expire` 已变化（改期），则跳过本次执行。
- **成功后删除**：处理器正常返回后删除记录。
- **至少一次（at-least-once）**：处理器 panic、或执行完但删除前进程崩溃，记录会保留并在下次轮询重试，因此处理器应尽量幂等。
- **处理器缺失不丢任务**：找不到同名处理器时保留记录并回调 `WithErrorHandler`，注册后下一轮重试。
- **慢任务防重**：执行期间有 `running` 标记，轮询不会把同一任务重复预热，避免并发重复执行。
- **不自动关闭 Store**：`Stop` 只停止扫描与内存调度；数据库连接等资源由调用方自行关闭。

### 配置项

| Option | 默认值 | 说明 |
|---|---|---|
| `WithPollInterval(d)` | `1s` | 扫描存储层的间隔 |
| `WithLookahead(d)` | `30s` | 预热窗口：加载 `Expire <= now+d` 的任务 |
| `WithBatchSize(n)` | `256` | 单次扫描最多读取的条数 |
| `WithErrorHandler(fn)` | 无 | 存储/处理器错误回调 |

`NewPersistentScheduler(store, s, opts...)` 的 `s` 传 `nil` 时使用默认 `NewScheduler()`；也可以传入自定义配置的内存调度器，或通过 `p.Scheduler()` 取回底层实例。

## 测试

```bash
go test -race ./pkg/htwheel/
```

测试覆盖：正常触发与执行时间参数、跨层 `Get`/`TTL`/`Update`、跨层 `Cancel`、同 id 覆盖、取消后更新、并发读写压测、`Stop` 幂等、极端远期任务与多层安置；持久化部分覆盖调度与执行、外部写入、取消/改期、文件重启恢复、慢任务防重、处理器缺失重试。建议始终带 `-race` 运行。
