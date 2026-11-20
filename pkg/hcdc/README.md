# hcdc

MySQL → Doris 的 CDC（Change Data Capture）增量同步引擎。以 **嵌入式 SDK** 形态集成进 Go 服务，进程内直连，零额外组件，一键完成「全量拉取 + binlog 增量订阅 + Stream Load 落库」。

```
MySQL(binlog, ROW 格式) ──canal──► Canal ──CSV──► Worker ──Stream Load──► Doris
```

## 特性

- **全量 + 增量一体**：首次启动自动全量拉取，随后切换 binlog 增量订阅，中间无缝隙
- **断点续传**：位点持久化到 Doris `cdc_record.log_position` 表，崩溃重启从上次已上传位点续传
- **GTID 双位点恢复**：优先用 GTID 恢复（跨主从切换、幂等不重），无 GTID 时回退物理位点（file+offset）
- **事务原子性**：GTID 变化识别事务边界，单个 `.active` 文件不跨事务切分，崩溃时不会出现半截事务
- **断线自愈**：canal 同步循环失败后指数退避（1s→30s）重建实例重连，重连时重新读取持久化位点
- **多实例并发**：每个 MySQL 实例一个 worker，并行采集、并发上传，位点加锁汇总
- **DDL 自动暂停**：源表结构变化时自动暂停「svc → Doris」上传，人工确认 Doris 侧 DDL 完成后手动恢复
- **自研监控**：零第三方依赖的原子计数器 + 快照，覆盖采集/上传吞吐、错误、延迟、DDL、暂停事件、缓冲积压
- **资源与安全**：连接池参数防泄漏、本地 CSV 积压磁盘水位告警（默认 10GB，仅告警不中断）

## 安装

```bash
go get github.com/wskfjtheqian/hbuf_golang/pkg/hcdc
```

> 依赖 Go 1.27+ 工具链（项目整体使用 `encoding/json/v2`）。

## 快速开始

```go
package main

import (
	"context"
	"log"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
)

func main() {
	ctx := context.Background()
	serverID := uint32(1001)

	h := hcdc.NewHCDC()
	err := h.SetConfig(ctx, &hcdc.Config{
		Canals: []hcdc.CanalConfig{
			{
				InstanceId:    1,
				Host:          "127.0.0.1:3306",
				Username:      "root",
				Password:      "123456",
				Schema:        "game",
				ServerID:      &serverID, // 生产环境建议显式配置全局唯一 serverID
				Charset:       "utf8mb4",
				IncludeTables: []string{"^user.*", "^order.*"},
				ExcludeTables: []string{"^tmp_.*"},
			},
		},
		Doris: &hcdc.DorisConfig{
			Host:            "127.0.0.1:9030",
			Username:        "admin",
			Password:        "",
			Schema:          "game",
			LoadURL:         "http://127.0.0.1:8040",
			MaxBacklogBytes: 10 << 30, // 可选：本地 CSV 积压告警阈值，0 用默认 10GB
		},
	})
	if err != nil {
		log.Fatalf("hcdc set config failed: %v", err)
	}
	// 保持进程运行即可，同步在后台 goroutine 中持续进行。
	select {}
}
```

## 配置说明

### CanalConfig（源 MySQL）

| 字段                                | 类型         | 必填 | 说明                                     |
| --------------------------------- | ---------- | -- | -------------------------------------- |
| `InstanceId`                      | `int64`    | 是  | 实例 ID，位点持久化的主键，多实例须唯一                  |
| `Host`                            | `string`   | 是  | MySQL 地址，如 `127.0.0.1:3306`            |
| `Username`                        | `string`   | 是  | 账号（需 REPLICATION SLAVE 权限）             |
| `Password`                        | `string`   | 否  | 密码                                     |
| `Schema`                          | `string`   | 是  | 数据库名                                   |
| `IncludeDBs` / `ExcludeDBs`       | `[]string` | 否  | 库级白名单/黑名单（正则）                          |
| `IncludeTables` / `ExcludeTables` | `[]string` | 否  | 表级白名单/黑名单（正则）                          |
| `ServerID`                        | `*uint32`  | 否  | binlog 订阅的 replica 标识，生产建议全局唯一；未设则随机生成 |
| `Charset`                         | `string`   | 否  | 字符集，默认 `utf8mb4`                       |
| `Flavor`                          | `string`   | 否  | 数据库类型；MySQL 留空，MariaDB 填 `"mariadb"`   |
| `LogDir`                          | `string`   | 否  | 本地 CSV 缓冲目录                            |

> **Flavor 注意**：源库为 MariaDB 时必须设 `Flavor="mariadb"`，否则 go-mysql 报 misconfigured flavor 且增量 DML 丢失（全量走 SQL SELECT 不受影响）。

### DorisConfig（目标 Doris）

| 字段                | 类型       | 必填 | 说明                                       |
| ----------------- | -------- | -- | ---------------------------------------- |
| `Host`            | `string` | 是  | Doris FE 地址，如 `127.0.0.1:9030`           |
| `Username`        | `string` | 是  | 账号                                       |
| `Password`        | `string` | 否  | 密码                                       |
| `Schema`          | `string` | 否  | 目标库名                                     |
| `SchemaPrefix`    | `string` | 否  | 目标库名前缀；同步时源 MySQL 库名会拼接该前缀作为 Doris 库名 |
| `LoadURL`         | `string` | 是  | Stream Load 地址，如 `http://127.0.0.1:8040` |
| `MaxBacklogBytes` | `int64`  | 否  | 本地 CSV 积压告警阈值（字节），0 用默认 10GB             |

> **SchemaPrefix（库名前缀）**：源库名与 Doris 目标库名默认一致；配置前缀后，同步到 Doris 的库名 = `前缀 + 源库名`。
> 例如源库 `game`、前缀 `ods_`，则建表/上传都落在 Doris 的 `ods_game` 库。前缀对「建库、建表、改表、删表、Stream Load 上传」全链路生效。
> 位点记录表 `cdc_record` 是 hcdc 内部保留库，**不受前缀影响**，始终固定。多个不同 MySQL 源若库名相同，可配合该前缀区分，避免落到同一张 Doris 表。

## API

### 生命周期

| 方法                    | 说明                                     |
| --------------------- | -------------------------------------- |
| `NewHCDC()`           | 创建引擎实例                                 |
| `SetConfig(ctx, cfg)` | 应用配置并启动同步；`cfg=nil` 关闭所有 canal 与 Doris |
| `Close()`             | 关闭单个 Canal（内部由 `SetConfig(nil)` 触发）    |

### 同步状态控制

| 方法                         | 说明                                                                 |
| -------------------------- | ------------------------------------------------------------------ |
| `Pause(ctx, reason)`       | 暂停 svc → Doris 上传（采集继续，数据缓冲到本地）                                    |
| `Resume(ctx)`              | 手动恢复上传                                                             |
| `GetSyncState() SyncState` | 返回状态快照（`Status`/`Reason`/`PausedAt`/`Schema`/`Table`/`InstanceId`） |

### 监控

| 方法                      | 说明     |
| ----------------------- | ------ |
| `GetMetrics() Snapshot` | 返回监控快照 |

`Snapshot` 覆盖：采集/上传吞吐（`CaptureRows`/`UploadRows`/`UploadBytes`）、错误计数（`CaptureErrors`/`UploadErrors`/`DDLErrors`）、延迟统计（`DelayAvgMs`/`DelayMaxMs`/`DelaySamples`）、上传耗时（`UploadAvgMs`/`UploadMaxMs`）、DDL（`DDLCount`/`CreateTable`/`AlterTable`/`DropTable`）、暂停事件（`PauseCount`/`ResumeCount`/`AutoPause`）、缓冲积压（`PendingFiles`/`PendingBytes`）。

## 工作原理

### 数据流

1. **全量拉取**：`Canal.Open` 先取 `GetMasterPos` 记录起始位点，再 `ReadData` 逐表 `SELECT` 全量数据写入 CSV，随后从该位点启动 binlog 订阅，衔接全量期间的增量。
2. **增量订阅**：go-mysql canal 解析 binlog（ROW 格式），`OnRow` 捕获 INSERT/UPDATE/DELETE，经 `Worker` 写入本地 CSV（`.active` 文件）。
3. **落库**：`Doris.loop` 周期扫描各 worker 的 `.active` 文件，并发调用 Stream Load 上传，成功后写回位点到 `cdc_record.log_position`。

### 位点持久化与断点续传

- 位点（binlog 文件 + offset + GTID）写入 Doris 的 `cdc_record.log_position` 表（`UNIQUE KEY instance_id` + MERGE 幂等）。
- 重启时 `Open` 读回上次位点：有 GTID → `StartFromGTID` 恢复；无 GTID → `RunFrom` 物理位点兜底；无记录 → 全量拉取。

### 表结构变更处理

- **初始化阶段**（建库建表 + 全量拉取）：源表建表事件同步建 Doris 表，不触发暂停。
- **运行阶段**：源表 ALTER/DROP 触发自动暂停，人工确认 Doris 侧 DDL 完成后 `Resume` 恢复。

### 列元数据来源（跟随 binlog 权威元数据）

hcdc fork 了 go-mysql 的 `canal` 包（`pkg/hcdc/canal/`），核心改造是让 `RowsEvent` 透传 binlog 自带的 `TableMapEvent` 权威元数据（`ColumnName`/`ColumnType`/`ColumnMeta`/`ColumnCount`）。`OnRow` 解析每一行时：

- **列名**：优先用 binlog 里的 `ColumnNames`（需 `binlog_row_metadata=FULL`），缺失时回退 `Table.Columns`。
- **列数**：以 binlog 的 `ColumnCount` 为准做校验，与本地 schema 缓存不一致时立即 `refreshTableMeta` 并返回错误触发重放自愈，杜绝「缓存滞后 → 下标越界 / 错列」。
- **列类型**：优先用 binlog 的 `ColumnType` 做语义分类（date/datetime/time/year/decimal 等），决定是否透传（base64）；缺失时回退 `GetColumnType`。

这相当于 MySQL 原生从库「跟着 binlog 元数据走」的做法，从根上消除「表结构变了但缓存没刷新」导致的错位。前提是源库开启 `binlog_row_metadata=FULL` 以让 binlog 携带列名。

### 错误自愈

- canal 同步循环失败 → 指数退避（1s 起，上限 30s）→ 重建 canal 实例重连（go-mysql 实例失败后不可复用）→ 重连前重新读取持久化位点。
- Doris 上传失败 → `loop` 动态 timer 退避（2s 起，上限 30s），成功后回到基准间隔。

## 前置条件

- **源 MySQL/MariaDB**：
  - 开启 binlog，且 `binlog_format=ROW`
  - 建议 `binlog_row_metadata=FULL`（MySQL 8.0 / MariaDB 10.5+），使 binlog 自带列名元数据，hcdc 可直接跟随 binlog 权威元数据做列映射与类型解析，彻底消除 schema 缓存滞后风险；未开启时回退走 INFORMATION_SCHEMA 缓存（详见「工作原理 · 列元数据来源」）
  - 同步账号需 `REPLICATION SLAVE`、`REPLICATION CLIENT` 及目标库的 `SELECT` 权限
  - `server_id` 全局唯一
- **目标 Doris**：FE（查询端口，默认 9030）与 BE 的 HTTP 端口（Stream Load，默认 8040）可访问；账号有建库建表与 Load 权限

## 测试

### 运行方式

```bash
# 单元测试（无需外部环境）
go test ./pkg/hcdc/ -run 'Test_ParseCaptureTime|Test_Monitor|Test_File|Test_DorisSyncState' -v

# 单元测试（含 race 检测）
go test -race ./pkg/hcdc/ -run 'Test_ParseCaptureTime|Test_Monitor|Test_File|Test_DorisSyncState' -count=1

# binlog 集成测试（需真实 MySQL + Doris，通过环境变量注入连接）
CDC_RUN_INTEGRATION=1 \
CDC_MYSQL_HOST=127.0.0.1:3316 \
CDC_MYSQL_FLAVOR=mariadb \
CDC_DORIS_HOST=127.0.0.1:9030 \
CDC_DORIS_LOAD_URL=http://127.0.0.1:8040/ \
  go test ./pkg/hcdc -run Test_Binlog -v -timeout 10m

# 性能基准（需真实 MySQL + Doris）
go run ./pkg/hcdc/cmd/bench -bench streamload -rows 300000 -row-width 120 -concur 8
go run ./pkg/hcdc/cmd/bench -bench full      -rows 1000000 -batch 5000
go run ./pkg/hcdc/cmd/bench -bench incr      -rows 100000  -batch 5000
```

集成测试默认跳过（无环境不报错），需设置 `CDC_RUN_INTEGRATION=1` 才会真正运行。

### 测试矩阵

| 类别     | 用例                                                  | 覆盖点                                                            | 状态 |
| ------ | --------------------------------------------------- | -------------------------------------------------------------- | -- |
| **单元** | `Test_ParseCaptureTime` / `Test_Monitor*`           | 延迟解析、监控计数器/快照                                                  | ✅  |
| **单元** | `Test_File_Backlog` / `Test_File_TxnBoundary`       | 积压统计、事务边界对齐（GTID 变化 flush）                                     | ✅  |
| **单元** | `Test_DorisSyncState` / `Test_DorisSyncStateString` | 同步状态机                                                          | ✅  |
| **集成** | `Test_BinlogSync`                                   | 全量 + 增量 INSERT/UPDATE/DELETE + **批量 INSERT（多行 RowsEvent 不漏行）** | ✅  |
| **集成** | `Test_BinlogDDLTriggersPause`                       | DDL 触发自动暂停 + 手动恢复                                              | ✅  |
| **集成** | `Test_BinlogDeleteSynced`                           | DELETE 位点推进 + MERGE 最终一致                                       | ✅  |
| **集成** | `Test_BinlogResumeFromPersistedPosition`            | 位点持久化 + 断点续传                                                   | ✅  |

> 集成测试均验证「可重复运行」：每个用例启动前清理自身 `instance_id` 的 `log_position` 残留位点，避免上次运行遗留位点触发断点续传、跳过全量拉取。

### 压力测试结果

测试环境：MariaDB 10.6（binlog ROW）@ 3316，Doris 3.0 单 BE @ 9030/8040，本机单进程。

**1. Stream Load 单文件导入吞吐**（隔离「上传」环节）

| 行宽        | 并发 1                | 并发 2    | 并发 4     | 并发 8    |
| --------- | ------------------- | ------- | -------- | ------- |
| 120B（普通行） | 105 万行/s · 164 MB/s | 90 万行/s | 100 万行/s | 84 万行/s |
| 512B（大字段） | 56 万行/s · 294 MB/s  | 47 万行/s | 52 万行/s  | —       |

**结论**：Doris 单表 Stream Load 吞吐天花板约 **110 万行/s / 160~300 MB/s**；并发导入到同一张 UNIQUE KEY 表不线性提升，并发 8 反而退化——瓶颈在 Doris 端 commit + MERGE，不在 hcdc。

**2. 全量拉取端到端吞吐**（MySQL → Doris，100 万行 / 200MB）

| 指标   | 数值                                        |
| ---- | ----------------------------------------- |
| 源端插入 | 16.4 万行/s                                 |
| 全量同步 | **4.58 万行/s · 9.2 MB/s**（完整上传 100 万行，无丢行） |

**3. 增量 DML 端到端吞吐**（binlog 增量，10 万行）

| 指标   | 数值                                                   |
| ---- | ---------------------------------------------------- |
| 源端写入 | 9344 行/s（批量 INSERT）                                  |
| 增量同步 | **5912 行/s**（写入 + binlog 捕获 + CSV + Stream Load 全链路） |

> 全量/增量端到端吞吐受 `flushDuration`(5s) + `scanFile`(2s 周期) 固定延迟主导；数据量越大，该固定开销摊薄越多。单表高吞吐优化方向是分片并发 Stream Load。

**4. 多实例吞吐曲线**（`cmd/benchmulti`，多台 MariaDB 各写 50 万行）

测试环境：3 台 MariaDB（3316/3317/3318，Docker 各自独立 binlog + server-id），单 Doris BE。

| 实例数 | separate（各写独立表） | shared（汇聚同一张表） |
| ------ | --------------------- | --------------------- |
| 1      | 71272 行/s            | 70155 行/s            |
| 2      | 123502 行/s (1.73x)   | 119560 行/s (1.70x)   |
| 3      | 120104 行/s (1.68x)   | 114350 行/s (1.63x)   |

**结论（修正）**：

1. **多实例对 hcdc 侧无负面影响**：1→2 实例吞吐近似翻倍（separate 1.73x / shared 1.70x），采集与落盘完全并行、互不干扰。
2. **真正的天花板是 Doris 单 BE 的聚合导入/compaction 容量**（约 12 万行/s）：2→3 实例几乎不再提升，separate 与 shared 收敛到同一上限。**瓶颈在 BE，不在 hcdc，也不在「是否同一张表」**。
3. 早前「并发导入同一张 UNIQUE KEY 表退化」的结论来自**单表内多并发 Stream Load**（进程内 8 并发退化成 84 万行/s）；而多实例场景是**多张表/多 worker 并发**，不会触发同样的 MERGE 竞争。纯 INSERT 下 per-table MERGE 竞争是次因；只有**大量并发 UPDATE 到同一 key** 时才会显著暴露。

> 运行方式见脚本头部注释；多实例需多台 MySQL（`-mysql "host1,host2"`），可用 Docker 一键起（`--server-id` 必须互不相同）。

## 限制

- **至少一次（at-least-once）语义**：靠目标表 `UNIQUE KEY + MERGE` 幂等去重，同时用 GTID 变化识别事务边界、单个 `.active` 文件不跨事务切分，崩溃时事务要么完整、要么完全未上传（不出现半截事务）。超大事务（超 `rotateSize`）仍会内部切分，属已知权衡。
- **源/目标单一**：仅支持 MySQL/MariaDB 源、Doris 目标。
- **单机无编排**：无分布式调度、HA、水平扩展能力。
- **类型映射**：字段级映射，已覆盖 enum/set→VARCHAR、year→SMALLINT、geometry 系列→STRING；JSON 等特殊类型仍建议自行扩展 `GetColumnType`。

