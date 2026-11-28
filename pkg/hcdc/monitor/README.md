# hcdc 监控 SDK

`github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/monitor` 管理同步源，提供新库和新表自动发现、状态查询、回调、启停、DDL 审批和配置重载。宿主应用调用 Go 方法，并自行决定如何鉴权、记录操作及对外传输状态。

## 接入

`monitor.New(serviceCtx)` 创建源清单为空的控制器。第一次 `Reload(ctx, cfg)` 成功后，调用方按 `SourceID` 启动当时已存在的库，监控在后台自动发现并启动后来新增的库。`serviceCtx` 贯穿监控和流水线的运行周期，配置加载与手动启动分别使用有期限的调用上下文。 [动态配置](../REFACTOR_DESIGN.md#配置动态更新) · [生产放行条件](../PRODUCTION_RUNBOOK.md#5-每源监控与放行条件)

### 配置 Doris 和 Canals

在 `CDC.Doris` 配置一个 Doris 集群，在 `CDC.Canals` 列出所有源实例。下例把 MariaDB 的 `game_in`、`game_usa` 和 MySQL 的 `orders` 同步到同一 Doris 集群。后续在 MariaDB 创建 `game_br` 并建好匹配的基础表后，监控将自动启动该源并创建 `e3_game_br`。把地址、账号、占位密码及目录换成实际部署值。

```yaml
CDC:
  AutoDiscoverInterval: 30s
  Doris:
    Host: 'localhost:9130'
    Username: cdc
    Password: '<doris-password>'
    Schema: ''
    LoadURL: 'http://localhost:8140/'
    SchemaPrefix: e3_
  Canals:
    - InstanceId: 20001
      Host: 'localhost:3316'
      Username: cdc
      Password: '<source-password>'
      Schema: game
      IncludeDBs: ['^game_(in|usa|br)$']
      ServerID: 12345
      Charset: utf8mb4
      Flavor: mariadb
      LogDir: ./cdc_logs
    - InstanceId: 20002
      Host: 'localhost:3306'
      Username: cdc
      Password: '<source-password>'
      IncludeDBs: ['^orders$']
      ServerID: 12346
      Flavor: mysql
      LogDir: ./cdc_logs
```

- 源库和表规则分别匹配库名和裸表名，排除规则优先。省略 `IncludeDBs` 时会发现账号可见且满足表过滤的业务库。系统库和视图会被跳过。`Canals[].Schema` 是读取元数据时使用的默认库，填写时须确保该库存在且账号可访问。`Charset` 用于发现阶段的 SQL 连接，流水线采集连接使用 `utf8mb4`。
- 流水线首次全量时，按 `Doris.SchemaPrefix` 拼接源库名并在 Doris 中创建目标库和表。前缀为空时目标库与源库同名。不同源实例有同名库时，用 `e3_{InstanceId}_` 把实例 ID 加入前缀。例如两个实例的 `orders` 分别写到 `e3_20001_orders` 和 `e3_20002_orders`。加载时发现同一个 Doris 目标库归属多个源会返回错误，保留原目录。Doris 账号需能创建目标库和表，并读写业务数据及 CDC 元数据。
- `InstanceId` 为正整数且在 `Canals` 中唯一。示例生成 `canal-20001-game_in`、`canal-20001-game_usa` 和 `canal-20002-orders`。显式 `ServerID` 分配给与 `Canals[].Schema` 同名且被选中的库。其他库的复制客户端 ID 根据实例、配置的 `ServerID` 与库名稳定派生。部署时核对这些 ID 与源拓扑中已有的复制客户端编号。
- Doris 的 `Host` 用于 SQL，`LoadURL` 用于 Stream Load。导入状态查询使用 `FrontendURL`，同主机的 `8040` 和 `8140` 可分别推导 FE 端口 `8030` 和 `8130`，其他部署应显式指定。`Doris.Schema` 为兼容字段，保留空值即可。
- 每条库流水线和每个自动接入表都有 256 MiB 的本地积压上限。通过对应状态中的 `PendingBytes` 和 `Backpressured` 监测积压和背压。需要在同一代次跨主续传时，在该源下设置 `GTIDAutoPosition: true`，并保留从首次快照起的完整 GTID 基线。 [GTID 切主演练](../PRODUCTION_RUNBOOK.md#7-gtid-切主旧主隔离与验证)
- 同代次重启使用原 `InstanceId`、`Generation`（默认 `'1'`）、Doris 目标与 `LogDir/<SourceID>/<Generation>` 持久目录。显式修改选表规则或目标映射时，使用新代次和空目标库重建。处理包含零日期的源时，可在对应 Canal 下选择 `ZeroDatePolicy: epoch`。 [重建与发布](../PRODUCTION_RUNBOOK.md)

### 首次全量与恢复

通过 `Canals` 建立新快照基线时，SDK 持久化恢复模式和原始表清单。这个流程覆盖首次加载时已存在的库、随后自动发现的库，以及通过 `Manager.SetConfig` 启动的库。清单中的表共用一个一致性视图和 Binlog 位点。全部表结构和编码批次封存后才开始向 Doris 导入业务行。SDK 逐表核对行数，空表也要求目标行数为零，随后发布该库共享的序号 `0` 检查点。

异常退出后使用原卷、原代次、原配置和原 Doris 地址重启，宿主按原启动策略调用 `Reload`、`Start` 或 `Manager.SetConfig`。封存前退出时重取一致性快照。封存后按原 Label 核对回执，补发可安全重试的原批次，并从封存位点接续增量，无需清库或清表。旧版已完成的基线按原检查点恢复。旧版全量已部分导入且缺少快照恢复证据时，保留原库表和现场，使用新代次、空目标库与新缓冲目录重建。磁盘须容纳所有同时初始化的库与表的完整编码快照及增量缓冲，并保留接续增量和追平所需的源 Binlog。 [恢复检查](../PRODUCTION_RUNBOOK.md#21-写入所有权容量和恢复检查)

### 自动发现新库

加载配置成功后，监控每轮查询源端元数据，再等待 `CDC.AutoDiscoverInterval` 开始下一轮。省略或填 `0s` 时默认等待 30 秒，可填写 `10s` 等正时长。负值会被配置校验拒绝。首次加载时可以先登记能正常连接、暂时没有匹配基础表的源实例。库中至少有一张可见、符合规则的基础表后，监控会把新库加入 `Snapshot()`，自动启动流水线并在 Doris 建库建表。在状态中核对 `ID`、`TargetDatabase`、`State` 和 `Delivered`，启动失败原因可从 `Error` 查看。重启时新出现的表在库恢复后单独接入。

新库接入保留已有源的运行句柄和手动启停状态。元数据查询失败或目标库名冲突会写入日志，后续扫描继续发现可用的新库。宿主取消 `serviceCtx` 或调用 `Close(ctx)` 后停止扫描。修改连接、选库选表规则或 `SchemaPrefix` 时调用 `Reload` 应用新配置。

### 自动发现已有库的新表

库流水线建立增量订阅后，按同一 `CDC.AutoDiscoverInterval` 检查库内的基础表。新表命中 `IncludeTables` 且未被 `ExcludeTables` 排除时自动加入同步。新表须具备支持的名称、稳定主键和 InnoDB 存储引擎。子流复用原 Doris 库的写入权，自动创建目标表，导入一致性快照，再从该快照位点接续 Binlog。原有表继续使用原流水线同步。

用 `SourceStatus.DiscoveredTables` 查看每张新表的 `Name`、`State`、`Delivered` 和 `PendingDDL`。目录或元数据读取失败见 `TableDiscoveryError`，单表启动失败见对应表的 `Error`。表登记、快照和检查点保存在原持久目录。已确认快照的表重启后从自己的增量位点恢复。

新表在向 Doris 写入业务行前，将完整快照的编码批次、表结构和 Binlog 位点封存到本地。封存前退出时，重启可以回收未发送的本地批次并重新取快照。封存后退出时，使用原卷、原代次和原 Doris SQL、FE、BE 地址启动，SDK 会核对目标、已确认回执和事务状态，只按原 Label 和原字节补发缺失或已中止且无冲突证据的批次。确认目标行数和序号 `0` 基线后，SDK 回收快照批次并开始增量。旧版本留下部分导入而缺少封存文件、目标证据冲突或续订所需 Binlog 已清理时，查阅该表的失败状态并保留现场。 [恢复检查](../PRODUCTION_RUNBOOK.md#21-写入所有权容量和恢复检查)

磁盘需容纳每个正在初始化的新表的完整编码快照，以及增量缓冲、事务暂存和文件系统余量。`MaxPendingBytes` 约束增量队列。保留源 Binlog 至封存位点对应的增量完成续订和追平。

原有表和新表各有交付水位。同一事务横跨两类表时，Doris 的可见时间可能不同。发布涉及新表的消费者前，按新表的 `Delivered` 单独核对位点和数据。

### 在 Go 宿主中启动

下面的 Go 示例接收已打开的 YAML，解码宿主配置中的 `CDC` 字段，再加载源、启动指定源并监听状态。需要同时处理业务请求的宿主，可把 `WatchStatus` 放在独立 goroutine。

```go
package example

import (
	"context"
	"errors"
	"io"
	"log"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/monitor"
	"gopkg.in/yaml.v3"
)

type appConfig struct {
	CDC hcdc.Config `yaml:"CDC"`
}

func monitorSource(serviceCtx context.Context, configYAML io.Reader, sourceID string) (resultErr error) {
	var cfg appConfig
	decoder := yaml.NewDecoder(configYAML)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}

	controller := monitor.New(serviceCtx)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, controller.Close(shutdownCtx))
	}()

	reloadCtx, cancelReload := context.WithTimeout(serviceCtx, 30*time.Second)
	err := controller.Reload(reloadCtx, &cfg.CDC)
	cancelReload()
	if err != nil {
		return err
	}

	startCtx, cancelStart := context.WithTimeout(serviceCtx, 30*time.Second)
	err = controller.Start(startCtx, sourceID)
	cancelStart()
	if err != nil {
		return err
	}

	return controller.WatchStatus(serviceCtx, sourceID, func(_ context.Context, s monitor.SourceStatus) {
		log.Printf("source=%s target=%s state=%s durable=%d delivered=%d pending_bytes=%d error=%q",
			s.ID, s.TargetDatabase, s.State, s.Durable.Sequence, s.Delivered.Sequence, s.PendingBytes, s.Error)
	})
}
```

## 方法与状态

| 方法 | 用途 |
| --- | --- |
| `Snapshot() Snapshot` | 按配置顺序独立采样各源，返回状态和 UTC 时间 `UpdatedAt`。读取使用内存状态。 |
| `Status(sourceID) (SourceStatus, error)` | 读取指定源的状态，找不到该源时返回 `ErrSourceNotFound`。 |
| `WatchStatus(ctx, sourceID, callback)` | 串行回调 `func(context.Context, SourceStatus)`，先发送当前快照，再跟踪该次取得的流水线句柄。 |
| `Start(ctx, sourceID)` | 请求启动该源。从 `Status` 或回调确认进入 `running` 并读取异步启动错误。 |
| `Pause(ctx, sourceID, reason)`、`Resume(ctx, sourceID)` | 持久化或解除该库原有表和自动接入表的交付暂停。暂停等待已在途的交付和新表快照，采集继续到各自缓冲上限。 |
| `ApproveDDL(ctx, sourceID, id)` | 审批当前队首可执行 DDL。先读取单源状态，`id` 取自源的 `PendingDDL.ID` 或对应表的 `DiscoveredTables[i].PendingDDL.ID`。手动暂停另用 `Resume` 解除。 |
| `Stop(ctx, sourceID)` | 停止指定源。操作超时后，用状态和可执行动作确认原句柄已释放，再重启或交接。 |
| `Reload(ctx, cfg)` | 首次加载或更新完整的源配置并启用后台发现。显式加载时加入的源由调用方启动，已运行且变更的源按代次交接，移除的源停止。更新失败后读回状态及检查点核对恢复结果。 |
| `Close(ctx)` | 停止后台发现，停止接收启动和重载，等待所有受管源关闭。宿主退出时用独立的关闭上下文。 |

`WatchStatus` 是阻塞方法。慢回调期间的中间变化可合并，取消监听只结束订阅。若源尚未启动，发送一次当前状态后返回。收到 `stopped` 或 `failed` 后监听结束，流水线的错误通过快照的 `Error` 字段呈现。完成重启或重载交接后，重新为新句柄调用 `WatchStatus`。

控制操作可用 `errors.Is` 区分 `ErrSourceNotFound`、`ErrInvalidState`、`ErrClosed` 及上下文错误。关闭超时后可再次调用 `Close`，等待资源释放。

`SourceStatus` 包含生成的 `TargetDatabase`、`ReadCursor`、`Durable`、`Delivered`、`PendingBytes`、背压与重试原因、待处理 DDL、零日期计数及 `Actions`。用 `Actions` 显示当前可用的启停、暂停及恢复动作，实际调用仍需处理状态变化导致的错误。零日期行数和检查点序号在 Go 中为 `uint64`，JSON 使用十进制字符串，便于浏览器保留 64 位整数精度。浏览器组件由宿主提供数据和控制动作。 [状态定义](status.go)
