# CDC 生产上线与回退操作手册

适用于以 Go SDK 部署的 `pkg/cdc`，源为 MySQL 或 MariaDB，目标为 Doris。按 `SourceID` 逐个发布消费者读路由。每次变更由一名发布负责人执行，数据库值班负责写入围栏和主库隔离，应用值班负责消费验证。开始前登记源、代次、两个读目标、当前路由修订、各位点、停写期限、观察窗口及回退负责人。2026-11-27 最终验收使用 MySQL 8.4.13、MariaDB 10.6.28 和单节点 Doris 3.0.8。 [最终验收](REFACTOR_DESIGN.md#612-最终发布验收)

## 1. 每源配置与容量预算

| 配置 | MySQL 示例 | MariaDB 示例 | 核对规则 |
| --- | --- | --- | --- |
| `Identity` 的 `PipelineID / SourceID / Generation` | `orders_cdc / orders_mysql / g20261127_01` | `payments_cdc / payments_maria / g20261127_01` | 全部登记。同一代次重启或 GTID 切主保持原值。 |
| `Source.Flavor / SourceIdentity` | `mysql / orders_primary_group` | `mariadb / payments_primary_group` | `SourceIdentity` 是稳定的复制拓扑名。初始化时用 `Cursor.SourceIdentity` 作为 `ShadowRoute.SourceIdentity`。注册表迁移后保留这个初始配置，当前物理成员从已发布水位读取。 |
| `Source.Address / ServerID` | `orders-db.internal:3306 / 72001` | `payments-db.internal:3306 / 72002` | 使用已核验物理节点的地址，复制客户端 ID 在拓扑内唯一，并与数据库成员 `server_id` 区分。 |
| `Source.Database / Tables` | `orders / events` | `payments / events` | 显式列出所有同步表。要求 InnoDB 和稳定主键，记录键列顺序、投影及类型。 |
| `Directory` | `/var/lib/cdc/orders_mysql/g20261127_01` | `/var/lib/cdc/payments_maria/g20261127_01` | 持久卷，运行账号可读写。Doris 写入权与原卷、绝对路径和代次绑定。按第 2.1 节交接。 |
| `TargetDatabase / Doris.ControlDatabase` | `orders_shadow_g01 / orders_cdc_g01` | `payments_shadow_g01 / payments_cdc_g01` | 旧库、影子库与 CDC 控制库隔离。重建使用新代次、空影子库、独立控制库与新目录。 |
| `Doris.SQLDSN / FrontendURL / StreamLoadURL` | 专用 SQL、FE HTTP 和可直达的 BE HTTP | 同左 | 三个地址必须属于同一实际集群。将凭据注入应用，`Doris.Username`、`Password` 可单独设置。SDK 禁止 HTTP 跳转。 |
| `Source.GTIDAutoPosition` | 首次快照前确定 | 首次快照前确定 | 默认物理续订。启用需要该代次从快照起具备完整 GTID 基线。物理模式保留同一物理成员及地址，MariaDB 的物理身份含地址。 |
| `SnapshotBatchRows / MaxPendingBytes / Doris.HTTPTimeout` | `500 / 32*1024*1024 / 60*time.Second` | 同左 | 显式配置复现验收预算。若将 `MaxPendingBytes` 留零，SDK 默认使用 256 MiB。 |
| `MaxTransactionBytes` | `32*1024*1024` | 同左 | 单笔事务的准备预算，默认沿用 `MaxPendingBytes`。按最大真实事务和暂存空间校准。 |
| `Doris.ControlRetentionRecords` | `4096` | `4096` | 每个流保留的最近已确认记录数。按证据保留窗口和源峰值记录率配置。 |
| `Doris.LoadRecheckTimeout` | `15s` | `15s` | 导入结果未明时核对原 Label 的预算。按实际审计延迟配置，最大 `10m`。首次核对时持久化截止时间。 |
| `Doris.DDLJobTimeout` | `1h` | `1h` | 已识别异步 DDL 作业的跟踪预算，首次绑定作业时持久化。单次 SQL 核对期限为 30 秒。 |
| `ZeroDatePolicy` | `reject` | `reject` | 默认拒绝零日期。开启 `epoch` 前核对时间主键中无真实的 `1970-01-01` 值，切换策略时新建代次、缓冲目录和目标库。 [零日期配置与统计](REFACTOR_DESIGN.md#配置动态更新) |

路由元数据库由独立的权威 MySQL 或 MariaDB 主库提供。初始化账号可建表和插入，发布账号需要 `SELECT`、`UPDATE`，消费者账号仅需 `SELECT`。源端快照会短暂取得全局读锁，初次快照期间冻结选表 DDL。源适配器的 `MySQLConfig` 没有 TLS 配置字段。部署要求加密时，先验证加密传输方案能同时覆盖 SQL 和 binlog 订阅，且保持物理节点绑定。 [源配置](pipeline.go) · [Doris 配置](doris.go) · [路由存储](shadow_route_store.go)

| 每源指标 | 上线门槛 | MySQL 最终实测 | MariaDB 最终实测 |
| --- | --- | --- | --- |
| 常规行，约 480 B | ≥ 350 次行操作/s，p95 ≤ 500 ms | 400.1/s，165 ms | 400.1/s，198 ms |
| 宽行，4–8 KiB | ≥ 200 次行操作/s，p95 ≤ 1 s | 250.6/s，258 ms | 250.8/s，388 ms |
| 逻辑积压和缓冲盘 | 32 MiB 逻辑限额，盘空间按下述暂存预算 | 历史目录文件长 34,423,581 B | 历史目录文件长 34,421,291 B |
| Doris 启动至追平 | ≤ 90 s | 48.810 s | 48.705 s |
| CDC 异常退出后重新启动至追平 | ≤ 30 s | 5.140 s | 5.205 s |

以上吞吐按行操作计数，更新、插入、删除各计一次。历史停机验收使用 32 MiB 逻辑预算时，每源落盘 31 笔，另 44 笔留在源 Binlog。宽行样本约 2.54 MiB/s，每源 30 秒全留本地约需 76 MiB 逻辑空间。新版按 `P = MaxPendingBytes`、`T = MaxTransactionBytes` 为每源预留 `2P + max(2T, T+P, 2P)`，再另计目录元数据、结构历史和文件系统分配余量。两项均为 32 MiB 时基础预留是 128 MiB/源。旧 `hcdc`、临时代次、日志、源 Binlog 和 Doris 存储另算。用实际峰值、最大事务和并行全量测出磁盘与堆峰值后再调节生产预留。

为生产记录实际峰值编码字节率、最大事务和源实例全量 binlog 字节率。按峰值乘以目标缓冲秒数估算本地容量，另计最大单笔及文件系统余量。binlog 保留目标至少覆盖从最早可能未落盘位点起算的最长快照或暂停时长、故障检测、隔离处置、目标恢复和追平。验收中的 30 秒停机加 90 秒追平预算只为该场景提供 120 秒下界，另计停止耗时及人工处置。用生产负载验证时间和空间两种保留限制，包含备选主库。 [负载模型与预算](REFACTOR_DESIGN.md#68-生产模拟负载doris-停机与进程恢复)

## 2. 上线前检查

以下 Bash 命令使用 `mysql` 或 `mariadb` 客户端。`${SRC_CNF}`、`${DORIS_CNF}`、`${ROUTE_CNF}` 指向权限受限的客户端选项文件，例如 `~/.config/cdc/orders-source.cnf`，在 `[client]` 中配置对应的 host、port、user、password 和要求的 TLS 参数。`${SPOOL}` 指向该源的持久目录。SQL 示例用第一节的库名与表名，改成变更单中登记的值后逐源执行。

```bash
mysql --defaults-extra-file="$SRC_CNF" --protocol=TCP <<'SQL'
SELECT VERSION(), @@GLOBAL.server_id, @@GLOBAL.log_bin,
       @@GLOBAL.binlog_format, @@GLOBAL.binlog_row_image;
SHOW BINARY LOGS;
SHOW GRANTS FOR CURRENT_USER;
SELECT TABLE_NAME, ENGINE
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'orders' AND TABLE_NAME IN ('events');
SELECT TABLE_NAME, SEQ_IN_INDEX, COLUMN_NAME
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = 'orders' AND TABLE_NAME IN ('events') AND INDEX_NAME = 'PRIMARY'
ORDER BY TABLE_NAME, SEQ_IN_INDEX;
SHOW CREATE TABLE orders.events;
SQL

findmnt -T "$SPOOL"
df -Pm "$SPOOL"
df -Pi "$SPOOL"
du -sm "$SPOOL"
```

- 确认每张选表均返回 InnoDB 和完整主键，`log_bin=ON`、`binlog_format=ROW`、`binlog_row_image=FULL`。用 CDC 账号验证选表及元数据读取、binlog 位点读取、复制订阅和快照全局读锁权限。通过同权限预演确认快照握手和锁等待可接受。检查 Bitmap32/Bitmap64 列注释、空值、无符号边界和目标投影，对无法完整比较的列先完成适配与验证。 [Bitmap 映射](REFACTOR_DESIGN.md#43-自定义-bitmap32-与-bitmap64)
- 确认目录挂载到预期持久卷，重启后挂载与所有权一致。检查可用块、inode 和配额。在预发确认单写约束，服务管理器应阻止副本并发接管同一源。迁移时保留旧 `hcdc` 和旧目标的独立卷及检查点。
- MySQL 8.4 用 `SHOW BINARY LOG STATUS`、`SELECT @@GLOBAL.server_uuid` 取当前文件、偏移和成员。MariaDB 10.6 用 `SHOW MASTER STATUS`、`SELECT @@GLOBAL.server_id, @@GLOBAL.gtid_binlog_pos`。启用 GTID 的代次还应执行第 7 节候选检查，并登记旧主隔离的执行入口和独立核验人。

```bash
mysql --defaults-extra-file="$DORIS_CNF" --protocol=TCP <<'SQL'
SHOW BACKENDS;
SHOW FRONTEND CONFIG LIKE 'fetch_stream_load_record_interval_second';
SHOW CREATE TABLE orders_shadow_g01.events;
SQL
```

首次建表前先执行 `SHOW BACKENDS` 和 FE 检查，`SHOW CREATE TABLE` 在建表后执行。确认目标 `UNIQUE KEY`、Merge-on-Write、列定义、主键和期望的副本策略。SDK 自动建表使用单副本，包含业务表与控制表，配置中没有副本数选项。需要多副本时，由 DBA 落实实际建表和升级策略，并在预发核对所有自动创建路径所用副本数，包括按需创建的 DDL 收据表。写入账号须能创建、读取和写入业务与控制表、应用批准的 DDL，以及查询导入事务和审计。BE 开启 `enable_stream_load_record=true`。FE 的审计采集延时应满足 `Doris.LoadRecheckTimeout`，默认 15 秒，验收用采集间隔为 5 秒。标签及审计保留覆盖最长故障和人工处置窗口。 [Doris 导入审计](https://doris.apache.org/docs/3.x/sql-manual/sql-statements/data-modification/load-and-export/SHOW-STREAM-LOAD/) · [验收配置](docker-compose.yml)

用同一集群上的隔离演练库确认首条实际 Stream Load，记录返回的 label。随后用该 label 验证 FE 状态与 SQL 一致。`DORIS_FE` 是 FE HTTP(S) 基址，`DORIS_NETRC` 指向权限受限的 curl 凭据文件。只查询可信的已确认 label。

```bash
: "${DORIS_DB:?填写该 label 所属的隔离演练库}"
: "${LABEL:?填写演练已确认的 Stream Load label}"
[[ "$DORIS_DB" =~ ^[a-zA-Z_][a-zA-Z0-9_]{0,63}$ ]] || exit 1
[[ "$LABEL" =~ ^[a-zA-Z0-9_]{1,128}$ ]] || exit 1
curl --fail-with-body --silent --show-error --netrc-file "$DORIS_NETRC" \
  --get "$DORIS_FE/api/$DORIS_DB/get_load_state" --data-urlencode "label=$LABEL"
mysql --defaults-extra-file="$DORIS_CNF" --protocol=TCP -e \
  "SHOW TRANSACTION FROM $DORIS_DB WHERE LABEL = '$LABEL';
   SHOW STREAM LOAD FROM $DORIS_DB WHERE LABEL = '$LABEL';"
```

预期 FE 返回 `code=0`、`data=VISIBLE`，SQL 事务为相同 label 且可见。审计中的库、表、批次校验和及行数与提交记录一致，`FilteredRows` 和 `UnselectedRows` 均为 0。`UNKNOWN`、审计缺失或收据冲突时停在原检查点，保留原 label 与凭据之外的证据，由值班核对。 [恢复实现](doris_load.go)

### 2.1 写入所有权、容量和恢复检查

- **Doris 写入所有权**。SDK 在 `cdc_writer_registry` 登记目标库和控制流的归属，读回原卷凭证后才允许流水线初始化或写入。生产账号需能创建并读取本注册库的所有权视图，运维账号管理注销权限。升级前停止同目标的旧版本写者。相同代次重启复用原卷和绝对路径。其他卷、代次或连接别名启动时仍会在同一 Doris 集群争用目标库声明。确需迁走原卷或注销时，先停止并隔离原写者，确认已接纳的 Stream Load、DDL 和控制写入都已有终态，再由发布负责人处置原声明。原缓冲的在线备份及副本不得作为第二份可写卷启动。 [所有权实现](doris_ownership.go)
- **启动快照恢复**。全量批次全部确认后，SDK 写入绑定原 Doris 的快照证据，再发布本地准备记录、Doris 序号 `0` 基线和本地完成记录。晚期崩溃可用原卷补确认，无需重扫。扫描中断且缺少准备证据，或者原目标和保存的证据不匹配时，保留原目录，按新代次使用空影子库。 [快照提交](pipeline.go)
- **事务和控制库容量**。上线前测单笔事务的暂存文件及待交付编码大小，两者分别受 `MaxTransactionBytes` 和 `MaxPendingBytes` 约束。为事务暂存、排序和交付文件共存预留物理空间。超过单笔预算时记录错误和原游标，调整预算后用原代次恢复。Doris 周期性清理老检查点及可核验归属的已确认回执，保留快照基线、最近检查点和恢复引用的结构版本。旧版本缺少归属的回执保留供人工核对。需要长期审计时应在保留窗口内归档。 [交付](delivery.go) · [控制表保留](doris_retention.go)
- **Binlog 事件上限**。单个原始事件解析上限为 64 MiB，压缩事件展开上限为 8 MiB。升级前核对已有 Binlog 的压缩策略和事件大小分布。遇到超限事件保留原游标，按源日志和数据对账结果调整源端策略或重建。复制协议先收包再校验大小，单个事件上限不能直接当成进程内存峰值。 [原始事件校验](internal/mysqlsource/binlog_decode.go)

用只读 SQL 检查注册对象和示例控制库的历史规模。控制库名按第 1 节配置替换。持续出现 `CDC control history cleanup failed` 时，核对控制库写入账号的清理权限及 Doris 状态。

```bash
mysql --defaults-extra-file="$DORIS_CNF" --protocol=TCP <<'SQL'
SHOW FULL TABLES FROM cdc_writer_registry;
SELECT stream_key, COUNT(*) AS retained, MAX(sequence_no) AS latest
FROM orders_cdc_g01.checkpoints GROUP BY stream_key;
SELECT COUNT(*) AS receipts FROM orders_cdc_g01.load_receipts;
SQL
```

## 3. 启动影子并核对两套链路

仓库以 SDK 提供生产控制。应用启动入口接收第 1 节的 `cdc.PipelineConfig`，调用 `cdc.StartPipeline(serviceCtx, config)`。`serviceCtx` 覆盖服务生命周期。启动成功返回句柄后，持续检查 `p.Status()`，确认 `starting` 进入 `running`，并通过 `p.Done()`、`p.Wait()` 记录异步退出原因。全量从空影子库开始。若全量中断且一致性视图失效，换新代次、空库和目录重建。正常增量重启复用原配置和持久目录。关闭使用单独限定期限的 `p.Close(ctx)`，确认 `Done()` 后再交接写入所有权。若必须清空积压，先围栏并等待共同位点和 `PendingBytes=0`，再关闭。`p.Pause(ctx, reason)` 只暂停新交付，源采集仍会运行到容量阈值。 [流水线入口](pipeline_control.go)

下面的 MySQL 示例在新代次中使用 GTID。应用通过部署配置和密钥管理注入地址、账号和凭据。MariaDB 按第一节逐项替换源配置与目录。

```go
package rollout

import (
	"context"
	"os"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/cdc"
)

func startOrders(serviceCtx context.Context) (*cdc.Pipeline, error) {
	return cdc.StartPipeline(serviceCtx, cdc.PipelineConfig{
		Identity: cdc.StreamIdentity{
			PipelineID: "orders_cdc", SourceID: "orders_mysql", Generation: "g20261127_01",
		},
		Directory: "/var/lib/cdc/orders_mysql/g20261127_01",
		Source: cdc.MySQLConfig{
			Address: os.Getenv("ORDERS_CDC_SOURCE_ADDRESS"),
			User:    os.Getenv("ORDERS_CDC_SOURCE_USER"), Password: os.Getenv("ORDERS_CDC_SOURCE_PASSWORD"),
			Flavor: "mysql", SourceIdentity: "orders_primary_group", ServerID: 72001,
			Database: "orders", Tables: []string{"events"}, GTIDAutoPosition: true,
		},
		TargetDatabase:    "orders_shadow_g01",
		SnapshotBatchRows: 500,
		MaxPendingBytes:   32 * 1024 * 1024,
		Doris: cdc.DorisConfig{
			SQLDSN:          os.Getenv("ORDERS_CDC_DORIS_SQL_DSN"),
			FrontendURL:     os.Getenv("ORDERS_CDC_DORIS_FE_URL"),
			StreamLoadURL:   os.Getenv("ORDERS_CDC_DORIS_BE_URL"),
			ControlDatabase: "orders_cdc_g01",
			HTTPTimeout:     60 * time.Second,
		},
	})
}
```

保留旧 `hcdc` 正常写旧库，影子完成全量后记录两套检查点并比对样本。经批准，在本源选表提交一笔可识别的增删改事务，等两套链路确认并核对对应 Doris 行和删行。旧 `hcdc` 检查点按已登记的旧实例 ID 查询。新代次查询示例假定控制库只含该代次。

```bash
mysql --defaults-extra-file="$DORIS_CNF" --protocol=TCP <<'SQL'
SELECT file, position, gtid FROM cdc_record.log_position WHERE instance_id = 123;
SELECT stream_key, sequence_no, checkpoint_json FROM orders_cdc_g01.checkpoints
ORDER BY sequence_no DESC LIMIT 5;
SELECT stream_key, COUNT(CASE WHEN sequence_no > 0 THEN 1 END) AS incremental_count,
       MIN(CASE WHEN sequence_no > 0 THEN sequence_no END) AS first_retained_seq,
       MAX(sequence_no) AS last_seq
FROM orders_cdc_g01.checkpoints GROUP BY stream_key;
SELECT stream_key, sequence_no, structure_revision FROM orders_cdc_g01.schema_versions
ORDER BY sequence_no DESC LIMIT 5;
SELECT load_label, target_table, expected_rows FROM orders_cdc_g01.load_receipts
WHERE verified = FALSE LIMIT 20;
SQL
```

将 `123` 换成旧实例实际 ID。如果控制库已有多条流，按登记的 `stream_key` 查询。检查点保留序号 `0` 和连续的最近增量历史。有增量时核对 `incremental_count = last_seq - first_retained_seq + 1`。未确认收据结合 `InFlight` 和是否已追平判断。全量、稳定负载、宽行和故障恢复达到第 1 节门槛后，才给该源安排灰度窗口。审计、对账及客户端日志保留源 ID、代次、位点、路由修订、label 和结果，差异样本按业务数据权限处理。

## 4. 写入围栏与逐源灰度

1. **接通持久路由。** 应用使用 `cdc.NewSQLShadowRouteStore(db, database)` 连接预建的权威元数据库。首次由初始化账号对整组 `[]cdc.ShadowRoute` 调用 `store.Initialize(ctx, configs)`，确认各源修订 1 指向各自旧库。正常启动用 `cdc.OpenPersistentShadowRouter(ctx, store, configs)` 打开发布器，消费者用 `cdc.NewShadowRouteReader(store, configs)`。每次查询或读事务先调用 `reader.Route(ctx, sourceID)`，在该次操作内固定返回的数据库。元数据不可读、行缺失或绑定不符时返回错误。先验证消费者重启仍读旧库，并记录其他源的路由作为对照。
2. **建立单源写入围栏。** 发布负责人锁定该源的发布操作。按部署平台入口阻断该源的 API、异步任务、定时任务、直接 SQL 写入及 DDL，确认在途事务已提交或回滚。保持 DBA 会话和业务围栏至发布读回。元数据主库应独立于被锁业务源。在批准的维护窗口，可用下方真实验收方式辅助锁定。在隔离演练中，从独立连接用测试行验证写入在锁内被拒绝，解锁后能够更新并回滚。MySQL 表读锁只覆盖清单表，仍需控制会使共同事务位点漂移的其他写入或 binlog 轮转。会话断开会释放 SQL 锁，围栏失效或到达停写期限时，丢弃未发布证据并记录结果。
3. **等待精确共同位点并做三方对账。** 围栏前按批准的写路径提交可识别的选表事务作为锚点。围栏下读回最终提交水位。旧库已确认位点和影子 `p.Status().Progress.Delivered.Cursor` 必须属于同一物理成员，精确到达锚点所在 binlog 文件和事务结束偏移。有完整 GTID 时核对完整集合。旧 `hcdc` 若保存末条行事件的偏移，只在下方 `SHOW BINLOG EVENTS` 证明其下一事件是对应 `Xid`、结束偏移等于共同位点时，为本次校验折算。只有物理证据时可对校验水位留空 `GTIDSet`，原持久位点照旧保留。截止时间内等不到精确水位或发现别的提交越过锚点，停止本次发布并重新安排围栏。对账为每张选表提供同一投影、完整主键和覆盖全键域的连续区间，核对源、旧 Doris、影子 Doris 三方行数、逐行值和摘要。文本键要先验证三端排序一致。
4. **发布并读回。** 同一个发布器实例在围栏内调用 `Verify`，逐表报告须全部 `Equal=true`，再调用 `Cutover`。权威元数据库读回当前源修订增加 1、指向影子库且水位正确。消费者用新请求读已知业务样本，核对实际 Doris 目标。已有读事务允许使用原路由完成。对照其他源的目标、修订、水位和业务读结果。读回完成后解除围栏，确认正常写入恢复并保留两套 CDC 写链。
5. **观察后再切下一源。** 灰度单元是完整 `SourceID`。先选一条低风险源。同源读路由一次提交即生效。建议初始观察窗口至少 15 分钟并覆盖一个代表性峰值段，实际值在变更单中登记。观察读错误、源到目标确认延迟、持续积压及业务抽样，启动独立消费者实例或按批准流程重启验证路由恢复。当前源达到阈值才切下一源，旧链保留至约定回退窗口结束。

辅助 SQL 锁通过专用交互客户端会话执行，路由发布使用其他连接。不要用执行后自动退出的 `mysql -e` 持锁。

```sql
-- MySQL 8.4，会话保持打开，按实际清单列出所有表。
LOCK TABLES orders.events READ;
SHOW BINARY LOG STATUS;
-- 发布读回完成后，在同一会话执行 UNLOCK TABLES;
```

```sql
-- MariaDB 10.6，影响整个实例，需单独批准停写范围。
FLUSH TABLES WITH READ LOCK;
SHOW MASTER STATUS;
SELECT @@GLOBAL.gtid_binlog_pos;
-- 发布读回完成后，在同一会话执行 UNLOCK TABLES;
```

用以下探针验证隔离环境中的 SQL 写入围栏。保持持锁客户端会话打开，在另一会话依次执行。替换为选表中已存在的测试行及普通值列。锁内要求 `UPDATE` 返回数据库错误 1205，解锁后要求影响 1 行。两次均执行 `ROLLBACK`。

```sql
SET SESSION lock_wait_timeout = 1, SESSION innodb_lock_wait_timeout = 1;
START TRANSACTION;
UPDATE orders.events SET amount = amount + 1 WHERE id = -1;
ROLLBACK;
```

```sql
-- 只读核对旧检查点后的下一事件，替换为实测文件和偏移。
SHOW BINLOG EVENTS IN 'mysql-bin.000014' FROM 11938 LIMIT 1;
```

应用发布入口可使用下列函数。`watermark` 三个字段分别填入围栏下的源水位、经证明的旧库水位和影子已交付水位。每个 `ShadowVerificationConfig` 的三个 `ShadowTable` 分别绑定到实际写入链路的 SQL 连接池和表。范围为 `[Start, End)`，唯一范围用 `[]cdc.ShadowKeyRange{{}}` 表示完整键域。应用在函数外持有围栏，并在返回后做权威读回和消费者验证。

```go
package rollout

import (
	"context"
	"fmt"

	"github.com/wskfjtheqian/hbuf_golang/pkg/cdc"
)

func publishFencedRoute(ctx context.Context, router *cdc.ShadowRouter, sourceID string,
	watermark cdc.ShadowCutoverWatermark, comparisons []cdc.ShadowVerificationConfig,
	rollback bool,
) (cdc.ReadRoute, error) {
	proof, reports, err := router.Verify(ctx, sourceID, watermark, comparisons)
	if err != nil {
		return cdc.ReadRoute{}, err
	}
	for i, report := range reports {
		if !report.Equal {
			return cdc.ReadRoute{}, fmt.Errorf("source %s differs on table %s", sourceID, comparisons[i].Source.Table)
		}
	}
	if rollback {
		return router.Rollback(ctx, proof)
	}
	return router.Cutover(ctx, proof)
}
```

```bash
mysql --defaults-extra-file="$ROUTE_CNF" --protocol=TCP --database=cdc_route_registry <<'SQL'
SELECT JSON_UNQUOTE(JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.SourceID')) AS source_id,
       revision,
       JSON_UNQUOTE(JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.Database')) AS target_db,
       JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.Watermark') AS watermark
FROM cdc_read_routes WHERE revision > 0 ORDER BY source_id;
SQL
```

用 `router.RouteContext(ctx, sourceID)` 或消费者 `reader.Route(ctx, sourceID)` 读回权威路由。`ErrShadowRouteRevisionChanged` 表示修订已推进，重新读回后以新围栏和证据发布。`ErrShadowRoutePublicationUnknown` 要求停止发布、从权威主库读回完整路由，并核对目标、修订和水位。围栏仍有效且能证明预期版本已提交时才能按已发布处理。结果仍未知则按停写期限升级处置，冻结该源的新发布，并通知消费端值班。直接改注册表会跳过提交校验。 [发布接口与证据](shadow_routing.go) · [持久化协议](REFACTOR_DESIGN.md#611-生产消费者路由持久化与发布)

## 5. 每源监控与放行条件

应用可用 `Pipeline.Status()` 查询单次快照，或用 `Pipeline.WatchStatus` 接收状态回调。监控按 `SourceID`、`Generation` 分组，并单独采集消费者实际路由、Doris 查询、文件系统及源 Binlog 指标。确认延迟从源 `COMMIT` 成功到对应事务进入已确认检查点。生产可用已知事务或探针采样计算 p95，仅在具备相应写入负载的窗口核对吞吐。 [状态结构](pipeline_control.go) · [进度结构](delivery_types.go)

在获得流水线句柄 `p` 后，可以由应用启动监听。注册后立即收到当前快照，之后每个监听串行接收。回调慢时中间变化会合并到最新快照。`stopped` 或 `failed` 快照送达后监听退出，失败原因在 `s.Err`。取消监听上下文只停止监听。如果 `Manager.SetConfig` 更换了流水线句柄，从 `Manager.Pipeline(sourceID)` 取得新句柄重新监听。

```go
go func() {
	err := p.WatchStatus(ctx, func(callbackCtx context.Context, s cdc.PipelineStatus) {
		hlog.Debug(callbackCtx, "CDC source=%s state=%s durable=%d delivered=%d pending_bytes=%d",
			s.Identity.SourceID, s.State, s.Progress.Durable.Sequence,
			s.Progress.Delivered.Sequence, s.Progress.PendingBytes)
	})
	if err != nil && ctx.Err() == nil {
		hlog.Warn(ctx, "CDC status watch stopped: %v", err)
	}
}()
```

| 指标或状态 | 观察与处置 |
| --- | --- |
| `State`、`Since`、`Reason`、`Err`、`PendingDDL` | 记录状态及持续时间。`failed`、计划外暂停、未处理审批或身份错误时立即停止该源发布。`Since` 表示状态切换时间。 |
| `ReadCursor`、`Progress.Durable`、`Progress.Delivered`、`InFlight` | 对比采集、持久和已确认的边界。有源事务写入时，已确认位点长期不前进即查交付。没有新写入时结合探针判断。序号差以有序记录计量，业务行数从事务或批次另采。跨文件或跨成员按身份、文件序列或 GTID 核对位点。 |
| `Progress.PendingBytes`、`Backpressured`、卷可用块及 inode | 建议以逻辑预算的 80% 为告警起点。持续背压、可用空间接近总预留或估算 binlog 即将清除所需位置时停止新发布并升级。剩余空间放不下一整笔时，未到 32 MiB 也可能背压。 |
| 源日志最早可续订水位和预计保留时长 | 源实例、备选主库分别核对。预计可用时间内无法追平时，立即进入事故处置。最老积压事务年龄需由应用或监控系统补采。 |
| `DeliveryError`、Doris 实际业务查询、Stream Load 审计及收据 | 目标短暂不可达时观察重试及积压。过滤行、目标不匹配、标签冲突或审计无法确认时保留现场，禁止快进位点。 |
| `CaptureError` | 源端瞬时传输错误触发退避并从已落盘位点续订。字段清空后确认新事务已交付。持续故障时核对源身份和日志保留，记录重连时长。 |
| 源到已确认吞吐、p95、重启追平 | 使用第 1 节门槛及正式窗口。Doris 计时从启动命令发出开始，CDC 计时从新进程启动开始，都以追到计划水位且积压清空为通过。 |
| 消费者 `Database`、`Revision`、`Watermark`、读错误率和样本 | 每次变更后核对目标和修订，检查真实查询结果和其他源的路由。重启后应读回相同修订。所有数据差异按故障处理。 |

独立控制台设置 `CDC_MONITOR_RELOAD_TOKEN` 后可用 `POST /api/cdc/reload` 重读启动时指定的 YAML。令牌至少 32 字节，使用 `Authorization: Bearer`。在本机权限受限的 curl 配置文件中保存请求头。重载通过 `Manager.SetConfig` 交接运行源，新增源保持待启动。同代次候选配置失败时，读回原源状态和检查点复核恢复。

```bash
curl --fail-with-body --config "$CDC_RELOAD_CURL_CONFIG" \
  --request POST http://127.0.0.1:8095/api/cdc/reload
curl --fail http://127.0.0.1:8095/api/cdc/sources
```

## 6. 回退消费者路由与恢复 CDC

| 触发条件 | 当前操作 |
| --- | --- |
| 错库、消费者读失败、三方差异、错误路由修订、其他源受影响 | 立即停止扩大灰度，保留路由、样本和对账证据，启动逐源回退评估。正在发布且结果未知时，先从权威库确认已生效路由。 |
| 常规或宽行持续超出第 1 节的吞吐或 p95 门槛，或读错误超出应用 SLO | 超过变更单指定的观察窗口时启动回退评估。核查是否为影子链路特有故障，确认旧链能提供安全读服务。 |
| Doris 恢复超过 90 秒、CDC 重启恢复超过 30 秒，或连续积压危及 binlog 保留 | 停止下一源变更，升级事故并评估原路由和两条链路能否追到同一水位。共享 Doris 停机时先恢复集群。 |
| 位点不连续、DDL 屏障未解决、导入结果未知或缺少源历史 | 保留现有数据、目录、label 和最后证据，先恢复链路或准备新代次。无法证明旧目标一致时暂停受影响的消费或按应用故障策略降级。 |

**执行读路由回退。** 先通过第 4 节 SQL 或 `RouteContext` 确认该源正读影子，旧目标写链仍在运行。在该源重建写入围栏，取得新的事务共同位点，等待旧库和影子确认，并用当前修订调用 `Verify`。全部对账报告相等后，调用 `Rollback`，在围栏内确认修订加 1、目标回到旧库、消费者读到预期行、其他源保持原路由。解除围栏后复验新写入，并重启消费者实例确认路由仍指向旧库。旧目标落后或任一方存在真实差异时，普通回退流程暂停，先修复、补齐并重新对账。每次回退使用新证据，按受影响源依次处理。

**恢复 CDC 进程或版本。** 如需停新链路，限定运行账号与范围后调用 `p.Close(ctx)` 并等待 `Done()`，保留原卷、控制库和故障证据。恢复增量使用相同流身份、同一代次、原目录、原目标，并确认新进程是唯一写入者。回滚应用版本前须验证它能识别当前缓冲格式、结构版本及目标表结构。无法验证兼容性时保留旧代次供对账，开新代次恢复。读路由回退不会修改已确认 CDC 位点。

**处置 DDL 屏障。** 读取 `PendingDDL` 的表、计划、ID 和原因，核对目标当前结构与作业记录。单表清空、单个非键非 Bitmap 删列或改名、满足约束的无损整数扩宽，核准其数据影响后调用 `p.ApproveDDL(ctx, id)`。手动暂停另用 `p.Resume(ctx)` 清除。确认先写结构版本、再推进屏障检查点和其后的 DML。`Kind=unsupported`、主键或类型投影变化等走新代次重建。目标只显示旧结构且原 DDL 提交结果不明时，核对原作业与收据。提交证明未恢复前保持阻塞。新影子数据库或改变投影会改变路由配置摘要，需先演练发布和消费者的注册表迁移。 [破坏性 DDL 决策表](REFACTOR_DESIGN.md#610-破坏性-ddl-的审批与重建)

**处置导入与慢 DDL。** 导入结果不明时，SDK 在持久化的截止期限内核对原 Label。窗口耗尽后停止流水线并保留原收据和批次。用第 2 节命令核对原 Label，证据齐备后用原卷启动。已识别的 Doris DDL 作业在异步运行期间，检查点留在屏障之前，SDK 按原作业标识继续查询。确认目标结构后才放行后续 DML。 [导入核对](doris_load.go) · [DDL 回执](doris_ddl.go)

## 7. GTID 切主、旧主隔离与验证

本节处理源数据库写主切换。先确认当前代次从快照起使用 `GTIDAutoPosition=true`。记录正常或故障前最后可用的 `Progress.Delivered.Cursor`、`Progress.Durable.Cursor` 和候选拓扑。对已接入持久路由的源，按第 7.1 节准备迁移并冻结发布。若原进程失联，保留卷，由重启恢复和 SDK 实际订阅确认持久恢复点。物理模式只可从原成员的已验证文件和偏移恢复。改用 GTID 需新代次快照。

1. **冻结业务写入，隔离旧主。** 用部署平台的故障切换入口撤下旧主写端点，阻断应用和高权限旁路，清理现存写连接，确认旧主实例已关停或其写网络和权限均被隔离。由数据库值班从独立路径核验。旧主可达时，使用下方 SQL 加一道只读检查。MariaDB `READ ONLY ADMIN` 账号仍可写。旧主不可达且无法证明隔离时，保持停写并升级，直到完成隔离。计划切换应记录停写后的旧主最后提交 GTID。异步复制事故若该水位不可得，将 RPO 标为未知并由事故负责人决策。 [MySQL 只读变量](https://dev.mysql.com/doc/refman/8.4/en/server-system-variables.html#sysvar_super_read_only) · [MariaDB 只读与权限](https://mariadb.com/docs/server/ha-and-performance/standard-replication/read-only-replicas)
2. **核验候选。** 在候选执行以下只读 SQL，核对预期成员、复制谱系、接收和应用状态、最后错误及日志保留。等候选应用完计划切换要求的旧主最终提交和已接收事务。MySQL 用两个 `GTID_SUBSET` 条件核对候选覆盖已落盘集合，且清理边界在已落盘集合中。MariaDB 以 `gtid_binlog_pos` 和最早文件的 `BINLOG_GTID_POS(file, 4)` 核对保留范围、domain、server ID 和序号。由 SDK 复核精确边界。候选落后、历史不完整或谱系不符时暂停切换，换可证明的候选或评估新代次重建。 [MySQL GTID 函数](https://dev.mysql.com/doc/refman/8.4/en/replication-gtids-functions.html) · [MariaDB 日志下界](https://mariadb.com/docs/server/reference/sql-functions/secondary-functions/information-functions/binlog_gtid_pos)
3. **提升并验证续订。** 通过部署平台已演练的选主操作提升候选，确认复制拓扑及候选可写。仍保持业务写入围栏。停止或隔离旧 CDC 写者，以新物理 `Source.Address` 和对应凭据启动同一代次，保留流身份、逻辑拓扑标识、表清单、Doris 地址和目录。启动期间监测是否出现 `failed`。之前的手动暂停如仍存在，经确认后显式 `Resume`。取回 Doris 检查点，核对已落盘前缀按序确认、续订水位、连续序号和原批次 label。经审批向新主提交探针事务，确认目标行值、删除结果及检查点，再开放新主对业务的写入口。旧主继续隔离，直到按流程接入新谱系并验证。
4. **迁移已发布读路由。** 准备期间消费者继续读原 Doris 目标。旧、新两条写入链在新主追平后，重新建立同一写入围栏，按第 7.1 节提交该源的新物理绑定。后续切换和回退各自需要新证据。

下列 SQL 按角色连接实例。旧主只读命令会修改实例状态，仅在数据库值班确认目标身份且进入维护窗口后执行。部署平台执行写端点控制、旧主隔离和候选提升。示例通过客户端参数文件指定物理节点。

```sql
-- MySQL 8.4 旧主，先核对成员身份，再执行隔离辅助操作。
SELECT @@GLOBAL.server_uuid, @@GLOBAL.server_id;
SET GLOBAL super_read_only = ON;
SELECT @@GLOBAL.read_only, @@GLOBAL.super_read_only, @@GLOBAL.gtid_executed;
```

```sql
-- MariaDB 10.6 旧主，同样先核对物理成员。
SELECT @@GLOBAL.server_id;
SET GLOBAL read_only = ON;
SELECT @@GLOBAL.read_only, @@GLOBAL.gtid_binlog_pos;
```

```sql
-- MySQL 8.4 候选，替换占位字符串为实际水位。
SHOW REPLICA STATUS\G
SHOW BINARY LOGS;
SELECT @@GLOBAL.server_uuid, @@GLOBAL.server_id, @@GLOBAL.gtid_mode,
       @@GLOBAL.enforce_gtid_consistency, @@GLOBAL.log_replica_updates,
       @@GLOBAL.read_only, @@GLOBAL.super_read_only;
SET @durable = '<CDC 已落盘 GTID 集合>';
SELECT GTID_SUBSET(@durable, @@GLOBAL.gtid_executed) AS candidate_covers_durable,
       GTID_SUBSET(@@GLOBAL.gtid_purged, @durable) AS retained_from_durable;
-- 计划切换另验旧主最后提交集合，结果应为 1。
SELECT GTID_SUBSET('<停写后旧主最终 GTID 集合>', @@GLOBAL.gtid_executed) AS candidate_covers_old_primary;
```

```sql
-- MariaDB 10.6 候选，最早文件使用 SHOW BINARY LOGS 的首行。
SHOW SLAVE STATUS\G
SHOW BINARY LOGS;
SELECT @@GLOBAL.server_id, @@GLOBAL.gtid_current_pos, @@GLOBAL.gtid_binlog_pos,
       @@GLOBAL.gtid_slave_pos, @@GLOBAL.gtid_strict_mode,
       @@GLOBAL.log_slave_updates, @@GLOBAL.read_only;
SELECT BINLOG_GTID_POS('mariadb-bin.000001', 4) AS retained_floor;
```

候选提升后再次验证 `ROW/FULL` 和可写状态。MySQL 要求 `gtid_mode` 和 `enforce_gtid_consistency` 为 `ON`、启用 `log_replica_updates`。MariaDB 要求启用 `gtid_strict_mode`，有复制历史时开启 `log_slave_updates`，且 `gtid_current_pos` 与 `gtid_binlog_pos` 相符。真实 MariaDB 切主验收覆盖单 GTID domain。两源实测分别在订阅新主后 281 ms 和 286 ms 追平，这个计时不含隔离、选主和业务恢复。候选缺必要 GTID 或所需日志被清理时，SDK 安全阻塞在原交付位点。 [GTID 专项验收](REFACTOR_DESIGN.md#69-跨主切换与-gtid-自动续订)

### 7.1 迁移消费者路由注册表

先滚动升级使用该注册表的发布器和消费者，使其识别迁移状态。停用上一版本的发布入口，确认所有读写实例均使用同一个权威元数据库。保留最初初始化的 `[]cdc.ShadowRoute`，包括原 `SourceIdentity`、目标库、选表和投影。注册表以这份配置验证绑定，迁移只在该源的记录中写入当前物理身份，其他源和组配置保持原值。更新后用原配置重启发布器或消费者即可恢复。旧版消费者不支持迁移后的记录。

1. **准备并冻结发布。** 从 `RouteContext(ctx, sourceID)` 读取修订 `N`，登记目标、位点与其他源完整路由。调用 `router.PrepareSourceMigration(ctx, sourceID, N)`。准备成功后，该源普通 `Verify`、`Cutover`、`Rollback` 均返回 `ErrShadowRouteMigrationPending`，消费者继续读原目标和修订 `N`。同修订重试准备是幂等的。计划切主在隔离旧主前完成准备。故障切主先停用该源所有发布入口，再从权威库读回并准备迁移。
2. **核对旧主基线。** 记录旧主上观测到的已交付完整 GTID 检查点，确认其物理位点达到或越过当前路由已发布位点。完成第 7 节的旧主隔离和新主续传。若旧主不可恢复且缺少可信的历史检查点，保持发布冻结并处理数据恢复。之前的物理 XID 证明仍可用于核对历史记录，新迁移必须提供旧主完整 GTID 检查点。
3. **在新主取共同位点。** 两套 Doris 写链都必须提供新主上精确的事务结束偏移和完整 GTID 集合。提交经批准的标记事务，保持同一业务写入围栏，等待两套写链确认。新主共同位点须属于同一逻辑复制拓扑的不同物理成员，GTID 须覆盖并推进旧检查点。`watermark.Source`、`LegacyDelivered`、`ShadowDelivered` 必须在同一新主、同一文件和偏移并具有相同 GTID。任一写链无法提供完整证明时，保留当前读目标和迁移冻结状态。
4. **提交并读回。** 维持围栏，调用 `router.ResumeSourceMigration(ctx, sourceID, N, oldCheckpoint, watermark, comparisons)`，用绑定到实际新主和两套 Doris 的连接对所有选表做完整主键区间比较，并核对新主可写、实时 GTID 集合与共同位点一致。提交后修订为 `N+1`，目标库和 `UsingShadow` 保持原值，水位指向新主，`SwitchedAt` 记录本次提交时间。通过消费者及下方 SQL 核对。旧实例证据和低于当前修订的证据均失效。首次迁移的既有 `MigrateSource` 入口仍可使用，重复迁移或曾取消的源使用带准备修订的 `ResumeSourceMigration`。对账、GTID 或身份校验失败时保留原目标和发布冻结，按第 7.2 节处理。
5. **恢复逐源发布。** 由原配置重新打开发布器和消费者，验证消费者实际读到目标库的样本并保持同一修订。后续每次 `Verify` 和 `Cutover` 或 `Rollback` 都在新主重新围栏和取共同 GTID 位点。对照其他源的完整路由及实际查询。回退消费者只切换其 Doris 读目标，当前源物理绑定保持在新主。

用只读账号查询权威元数据库。`MigrationPending` 为空表示普通发布可在通过校验后进行。`ReboundSourceIdentity` 记录最近迁入的物理成员，首次迁移前为空。`LastCanceledRevision` 记录上一次安全取消所消耗的准备修订。

```bash
mysql --defaults-extra-file="$ROUTE_CNF" --protocol=TCP --database=cdc_route_registry <<'SQL'
SELECT revision,
       JSON_UNQUOTE(JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.Database')) AS target_db,
       JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.MigrationPending') AS migration_pending,
       JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.LastCanceledRevision') AS last_canceled_revision,
       JSON_UNQUOTE(JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.ReboundSourceIdentity')) AS member,
       JSON_EXTRACT(CONVERT(route_json USING utf8mb4), '$.Watermark') AS watermark
FROM cdc_read_routes WHERE source_key = UNHEX(SHA2('orders_mysql', 256));
SQL
```

准备、恢复迁移和取消均以源配置摘要、修订和前次提交令牌做原子比较。连接断开后 SDK 以同一次提交令牌和完整记录读回确认。按第 7.2 节处置无法确认的结果。 [迁移 SDK](shadow_route_migration.go) · [注册表提交协议](shadow_route_store.go)

### 7.2 中断恢复和安全取消

重启发布器时使用原注册配置连接权威库，调用 `router.SourceMigrationStatus(ctx, sourceID)`。登记 `Route.Revision`、`Route.Database`、`Route.Watermark`、`SourceIdentity`、`Pending`、`LastCanceledRevision`，并用消费者核对原目标可读和另一源的路由。`Pending=true` 时继续冻结该源普通发布，使用读回的同一准备修订。重新取证前先证明当前唯一写主，恢复过程中持续遵守第 7 节的写入围栏。

| 权威读回或现场状态 | 操作 |
| --- | --- |
| 准备修订 `N`，`Pending=true`，绑定和原路由匹配，候选已被安全提升 | 在候选上重新取得两条写链的完整共同位点并三方对账，再调用 `ResumeSourceMigration`，传入已登记的 `N`。 |
| 候选 GTID 不足、任一目标落后、对账失败或身份不明 | 保持普通发布冻结和原读目标，处置候选或数据。确认仍绑定同一准备时才进入重新取证。 |
| 结果未知，读回原修订 `N` 且 `Pending=true` | 该修订仍待处置。核对物理绑定、原路由和变更单一致，重新确认围栏和共同位点，再提交同一准备的恢复或取消。首次准备若读回 `N` 且 `Pending=false`，核对原记录后可用同一 `N` 再准备。 |
| 结果未知，读回 `N+1` 且 `Pending=false` | 完整核对目标、预期水位和物理绑定。迁移成功应绑定新主。取消成功应仍绑定原主，且 `LastCanceledRevision=N`。记录结果并从当前修订继续。 |
| 权威库不可读、记录不匹配或已超过预期修订 | 暂停这个源的所有发布，由值班核对提交记录和当前拓扑。消费者继续尝试读取权威库上的已发布路由。 |

**安全取消。** 确认当前绑定成员仍是唯一可写主。候选必须保持只读或已隔离，并确认没有在候选上接受需要保留的业务写入。候选因 GTID 不足被拒绝时也要核实写主状态。如果原成员离线、已经有新主写入或存在分叉，维持发布冻结并处理恢复。满足条件后，在原成员重新建立业务围栏，读取实时完整 GTID，确认两套 Doris 的已交付位点与源同文件、同偏移且 GTID 等价。用原成员及两套目标执行全表范围三方对账，在同一围栏内调用 `router.CancelSourceMigration(ctx, sourceID, N, watermark, comparisons)`。取消会保留原目标和物理绑定，提交新共同位点并将修订增为 `N+1`。确认 `Pending=false`、`LastCanceledRevision=N`，消费者仍读原目标，其他源路由保持原值后再解除围栏。后续发布使用新的证据。

`ErrShadowRoutePublicationUnknown` 返回后，使用新上下文和独立权威连接读回。确认结果前暂停该源后续发布，禁止直接修改 `cdc_read_routes`。已经结束的准备收到延迟的恢复或取消请求时，返回 `ErrShadowRouteRevisionChanged`。新的准备使用当前修订，旧证据不会因解除冻结而恢复有效。

### 7.3 多发布器与重复请求

- **同一源。** 以权威库中的 `Pending` 和准备修订 `N` 为准。所有发布器都传入同一个已确认的 `N`，由注册表比较该源的配置摘要、修订和前次提交令牌。成功恢复或取消只消耗一次 `N`，将修订推进至 `N+1`。收到 `ErrShadowRouteRevisionChanged` 时，读回最终物理成员、读目标、水位和 `LastCanceledRevision` 后结束该次旧请求。源端仍须由部署平台维持唯一写主和有效围栏。演练中的竞争通过延迟旧请求注入。
- **应答丢失。** 使用独立权威连接执行 `SourceMigrationStatus`。权威库仍是完全匹配的 `N` 且 `Pending=true` 时，在有效围栏内重新读共同位点和对账，再对同一轮准备提交。若已结束，记录 `N+1` 的实际结果。旧请求即使重复到达也不能消耗下一轮准备。应用需要发起新的变更时，登记当前修订并重新 `Prepare`。
- **不同源。** 可在同一权威注册库执行并行迁移。每个源各自取得唯一写主、共同位点和围栏，提交及读回只归属该源。权威库要在所有被迁移的旧主停机及持锁期间保持可写。逐源核对修订、目标及实际 Doris 读结果，在消费者重启后复核。

### 7.4 权威路由元库故障切换

元库故障切换改变 `SQLShadowRouteStore` 所用的 InnoDB 主库连接。每个业务源的原配置、当前物理绑定和 Doris 目标继续使用已复制的注册表记录。部署系统负责元库选主、旧主隔离和更新连接。`OpenPersistentShadowRouter` 按调用方提供的权威连接读取当前状态。

消费者默认直接读权威元库。业务明确允许在有限时间内读到上一次确认的目标时，可以使用 `NewShadowRouteReaderWithCache`。TTL 内的逐源缓存仅在权威库暂时不可达时生效。应用用 `RouteWithStatus` 记录是否命中降级结果，保证该期间旧 Doris 目标仍可读且符合业务水位。发布、回退和重新取证继续使用权威库。 [消费者路由](shadow_route_reader.go)

1. **暂停发布并记录切换基线。** 暂停该注册表的所有发布器，处置在途操作。计划切换时在短停写窗口内，从旧元库登记完整 GTID 和各源的完整 `ReadRoute`、`Pending`、`LastCanceledRevision`、物理绑定及提交令牌。提交结果仍未知时按第 7.2 节登记该请求与预期状态。未能证明最后确认的元数据提交已保留时，保持发布停止，保留旧主卷和候选日志，按事故流程核对。
2. **验证候选并隔离旧主。** 区分候选 I/O 已接收与 SQL 已应用的 GTID。候选需要覆盖最后确认的元库位点，并核对相同拓扑及复制错误。撤下旧元库写入口，阻断既有写连接与高权限旁路，从独立路径核对旧主确实隔离。元库不可达时，新发布与新路由查询均返回错误。对每个新查询按应用策略处理失败，延迟到权威库恢复。
3. **提升并重新连接。** 在候选上确认最后确认的 GTID 已应用，再按部署流程提升，并核对当前唯一可写元库。关闭旧连接池及旧发布实例，重新建立发布器的 `SQLShadowRouteStore` 和只读消费者连接。保留原配置，使用 `OpenPersistentShadowRouter` 打开既有注册表。禁止用 `Initialize` 修补缺失记录。原元库需按副本接回流程校验，验证前维持隔离。
4. **逐源读回后恢复。** 用 `SourceMigrationStatus` 核对待迁移标记、修订、成员、目标及水位，用只读 SQL 对比提交令牌。消费者重启后执行实际 Doris 查询，确认两类路由分别落到已发布目标。同一旧修订重复请求应返回 `ErrShadowRouteRevisionChanged`。旧发布器签发的证明需要在新实例重新核验，仍处于准备状态的当前修订按第 7.2 节重新围栏和取证。任一关键记录对不上时继续冻结受影响范围。

在 MySQL 8.4 元库候选执行以下查询。把 `@registry_fence` 替换为停写后记录的完整旧主 GTID，返回 `1` 才进入状态对比。读取 GTID 和两条复制线程的状态，同时核对连接实际指向候选。

```sql
SELECT @@GLOBAL.server_uuid, @@GLOBAL.server_id, @@GLOBAL.read_only,
       @@GLOBAL.super_read_only, @@GLOBAL.gtid_executed;
SHOW REPLICA STATUS\G
SET @registry_fence = '<停写后旧元库的完整 GTID 集合>';
SELECT GTID_SUBSET(@registry_fence, @@GLOBAL.gtid_executed) AS applied;
```

在 MariaDB 10.6 元库候选核对 `gtid_binlog_pos` 和 `gtid_slave_pos` 的已应用边界，按 domain、server ID 和序号对比旧主基线。关键路由的修订、载荷和令牌也必须一致。

```sql
SELECT @@GLOBAL.server_id, @@GLOBAL.read_only, @@GLOBAL.gtid_binlog_pos,
       @@GLOBAL.gtid_current_pos, @@GLOBAL.gtid_slave_pos;
SHOW SLAVE STATUS\G
```

下例通过权威库只读连接核对指定业务源的持久记录。仅在变更记录内处理实际载荷与令牌。

```bash
mysql --defaults-extra-file="$ROUTE_CNF" --protocol=TCP --database=cdc_route_registry <<'SQL'
SELECT revision, HEX(config_digest) AS config_digest,
       CONVERT(route_json USING utf8mb4) AS route,
       HEX(publication_token) AS publication_token
FROM cdc_read_routes WHERE source_key = UNHEX(SHA2('orders_mysql', 256));
SQL
```

## 8. 发布前演练和变更收尾

从仓库根目录执行配置检查和本地测试。真实数据库验证按设计文档第 6.4 节配置五项连接变量，在隔离 MySQL、MariaDB 和 Doris 上运行核心集成测试。 [核心集成测试](REFACTOR_DESIGN.md#64-验证真实链路和-ddl-恢复)

```bash
docker compose -f pkg/cdc/docker-compose.yml config --quiet
go vet ./pkg/cdc/...
CDC_TEST_REQUIRE_INTEGRATION=1 CDC_TEST_DORIS_AUDIT=1 \
CDC_TEST_DESTRUCTIVE_DDL=1 CDC_TEST_PERSISTENT_ROUTES=1 \
  go test -race -p 1 ./pkg/cdc/... -count=1 -v -timeout 9m
```

发布前按第 4 至 7 节在隔离环境演练灰度、回退和适用的故障切换场景。独立演练各用一组跟随原主的新候选，涉及停机或提升时只使用可丢弃的测试实例。 [GTID 演练拓扑](REFACTOR_DESIGN.md#69-跨主切换与-gtid-自动续订)

升级前将早期缺少清单摘要的物理位点缓冲排空，确认 `PendingBytes=0` 且目标检查点读回一致。已交付末条仍可核对。旧格式待交付记录不能覆盖写入新的清单摘要，无法排空时按新代次重建。回退前还须核对旧程序能读取当前快照完成记录和缓冲格式。 [缓冲验证](delivery.go)

每条源收尾登记最终目标和修订、Doris 检查点、容量与延迟样本、业务对账、故障时长及处理记录。确认消费者重启读到权威路由，其他源路由保持原值，旧主隔离和旧目标保留满足变更单约定。旧链路、目录和元数据待回退窗口结束及复核后按保留策略处置。

## 9. 2026-11-27 上线与回退演练记录

这轮演练复用了 `hbuf-cdc-stage2` 隔离容器。源为 MySQL 8.4.13 和 MariaDB 10.6.28，目标为 Doris 3.0.8，缓冲目录位于本机 NVMe ext4。确认 Doris BE 存活，FE 导入审计采集间隔为 5 秒。完整容器回归串行执行，`pkg/cdc` 用时 293.233 秒，两个内部包均通过，覆盖破坏性 DDL、Stream Load 响应丢失、全量与增量、双源隔离、生产停机和消费者路由。

| 源 | 常规行操作/s 与 p95 | 宽行行操作/s 与 p95 | Doris 启动至追平 | CDC 强杀重启至追平 |
| --- | --- | --- | ---: | ---: |
| MySQL | 400.1，155 ms | 250.8，177 ms | 45.974 s | 7.195 s |
| MariaDB | 400.1，158 ms | 250.9，178 ms | 46.274 s | 7.196 s |

Doris 从停机完成到发出启动命令间隔 29.659 秒。每源 32 MiB 逻辑限额下落盘 31 笔，44 笔留在 binlog，实际缓冲目录文件长度分别为 34,423,581 B 和 34,421,291 B。Doris 恢复后每源 11,400 行逐列一致，CDC 强杀恢复后每源 11,592 行逐列一致，检查点推进至 412，原有 label 均只发送一次 PUT。所有指标满足第 1 节预算。

直连突发测试双源合计 1,421 次行操作/s。每次导入增加 40 ms 的复测合计 1,064 次/s，每源在 2 MiB 限额下落盘 23 笔，17 笔留待 binlog 续读。MySQL 和 MariaDB 重启追平分别为 7.552 秒和 7.592 秒，恢复后各 6,800 行逐列一致。该复测用于观察突发积压，常规和宽行的延迟预算使用上表的等速写入结果。

**写入围栏、灰度和回退。** 先发布 MySQL，再发布 MariaDB，随后按相反顺序回退。MySQL 在选表上持读锁，MariaDB 持全局读锁。四次变更在持锁期间，从独立连接执行 UPDATE 均收到源库错误 1205，解锁后同一 SQL 均更新 1 行并在探针事务内回滚。每次发布前按两个相邻主键区间对账，区间分别含 1 行与 2 行，源、旧库和影子库的逐行数据及摘要一致。两源持久发布复测耗时 26.44 秒。

| 源 | 修订 2 指向影子时的 XID 位点 | 修订 3 回到旧库时的 XID 位点 |
| --- | --- | --- |
| MySQL | `mysql-bin.000019:281095990` | `mysql-bin.000019:281099879` |
| MariaDB | `mariadb-bin.000019:445979639` | `mariadb-bin.000019:445980472` |

消费者使用只读账号，分别在初始路由、双源切换后和双源回退后重启，恢复修订 1、2、3。从实际 Doris 读出的测试值依次为 100、110、120。每次发布只改变当前源的路由，另一源的目标、修订、水位及测试读结果保持原值。影子落后时发布被拒绝。两个发布器均拒绝上一修订的证据，重启发布器也拒绝旧实例签发的证据。此前验收只执行了持锁流程，缺少独立连接的写入探针和修订 1 的重启验证。本轮补齐两项检查并通过回归。

**旧主隔离与 GTID 续传。** 复用两个隔离主库，另建两个一次性候选。测试先让候选接收事务并暂停应用，在旧主形成一笔已落盘、一笔因真实容量背压留在 binlog 的积压。实际停止旧主容器，检查 Docker 报告 `Running=false` 且原 SQL 端点拒绝连接，然后验证落后候选无法推进 Doris。候选完成 relay log 应用并提升后，以原代次、原目录和新地址启动 CDC，读回原暂停状态后恢复交付。

| 源 | 旧主日志 → 新主日志 | 启动新地址至序号 3 | 追平后重启及行值核对 |
| --- | --- | ---: | --- |
| MySQL | `mysql-bin.000019` → `mysql-promoted.000001` | 296 ms | 10 行一致，检查点 0–5 连续，6 个 label 各一次 PUT |
| MariaDB | `mariadb-bin.000019` → `mariadb-promoted.000002` | 311 ms | 10 行一致，检查点 0–5 连续，6 个 label 各一次 PUT |

两种源都拒绝落后候选和缺少所需 binlog 的候选，Doris 检查点和 PUT 数保持原值。GTID 专项耗时 16.662 秒。首次准备夹具时，MySQL 8.4 的 `CHANGE REPLICATION SOURCE TO` 对超过 32 字符的 `SOURCE_PASSWORD` 返回 3056。清理该轮夹具后，使用 32 字符随机复制密码重新初始化，确认两种候选的接收与应用线程均运行。

该轮隔离演练通过。GTID 专项采用实际停止容器验证旧主隔离，持久路由专项在独立场景验证消费者重启与回退。临时从库和复制账号已回收，原 MySQL、MariaDB 和 Doris 测试容器继续运行。跨主与路由注册表迁移的联合结果见第 10 节。

## 10. 2026-11-27 跨主路由迁移验收

演练使用 MySQL 8.4.13、MariaDB 10.6.28、Doris 3.0.8 隔离容器及两台一次性从库。迁移专项使用重新准备的一组跟随原主的从库。每种引擎分别作为被切换源，在另一种源的实例上建立权威注册表，使被测旧主停止时仍可读路由。旧库和影子库分别由两条独立 GTID 流水线写入。同一套消费者进程配置从旧主阶段沿用到整个演练结束。联合测试耗时 33.98 秒，MySQL 22.76 秒，MariaDB 11.22 秒。

- 旧主先以修订 2 发布到影子。准备迁移后实际停止该主库，确认 SQL 不可达。使用原配置重启消费者，仍以修订 2 查询原影子库，实际业务样本值为 120。原发布器持有的旧证明返回 `ErrShadowRouteMigrationPending`。
- 提升候选后，两条写入链都以原代次及持久目录继续订阅。在新主持锁等待两套 Doris 到达精确、完整的 GTID 位点，对账覆盖两个主键区间，各含 1 行和 2 行，源及两个目标逐行数据和摘要一致。持锁期间独立 `UPDATE` 返回 1205，解锁后可更新 1 行并回滚。迁移到修订 3 保持原影子目标，消费者读到新主写入的样本值 130。迁移提交后再次使用旧证明返回 `ErrShadowRouteRevisionChanged`。
- 按新主的四个独立围栏取证完成迁移、回退、切换及再次回退。重新打开发布器并多次重启独立消费者，读回修订 3、4、5、6，样本值依次为 130、140、150、160，目标依次为影子、旧库、影子、旧库。另一源始终保持修订 1 和原路由，并在演练中持续写入。用实际 Doris 查询及三方对账确认另一源的样本从 110 更新至 120。

| 源 | 修订 3 迁移位点及耗时 | 修订 4 回退 | 修订 5 切换 | 修订 6 再次回退 |
| --- | --- | --- | --- | --- |
| MySQL | `mysql-promoted.000003:3256`，29 ms | `mysql-promoted.000003:3916` | `mysql-promoted.000003:4576` | `mysql-promoted.000003:5236` |
| MariaDB | `mariadb-promoted.000002:23712`，30 ms | `mariadb-promoted.000002:24557` | `mariadb-promoted.000002:25402` | `mariadb-promoted.000002:26247` |

耗时从开始三方对账至提交迁移并读回消费者结果，生产对账时间需要按完整表范围估算。MySQL 新主的 GTID 集合覆盖两个 SID。MariaDB 联合验证采用单 domain。

首轮并行接入发现 GTID 查询的字符串仍引用驱动回收的结果缓冲。已在释放结果前复制查询值，并用确定性单测确认复用缓冲不会改写已读取值。真实容器重新运行双 GTID 捕获通过。测试读取 MySQL 多 SID 状态后先归一化 GTID 集合，再比较精确的文件和偏移。 [GTID 查询回归](internal/mysqlsource/source_test.go)

原有持久路由、并发发布和 40 ms 慢目标压力复测均通过，合计 43.057 秒。额外验证了发布器已取得旧修订和提交令牌时，准备迁移仍能拦截其在途发布。`go test -race ./pkg/cdc/... -count=1` 和 `go vet ./pkg/cdc/...` 均通过。

原 GTID 续传专项用新建候选回归通过，耗时 16.48 秒。一次性 MySQL 候选在加载基线前去除了镜像初始化产生的本地 GTID，使该用例能按预期检查连续序号。MySQL 和 MariaDB 从新主启动后分别在 281 ms 和 276 ms 追平，均拒绝落后候选及缺少所需 binlog 的候选。原三个容器已恢复，临时复制账号和从库已清理。

## 11. 2026-11-27 路由迁移中断恢复与取消验收

演练复用 MySQL 8.4.13、MariaDB 10.6.28、Doris 3.0.8 隔离容器和一次性从库。两项子用例在未关停的另一源上保留权威路由库，并让独立消费者按实际路由查询真实 Doris。专项耗时 39.58 秒，MySQL 25.79 秒，MariaDB 13.78 秒。

| 场景 | MySQL 与 MariaDB 结果 |
| --- | --- |
| 准备后重启 | 发布器重新打开注册表，独立消费者进程重启后也读回待迁移修订 2 和原物理绑定。仍从原影子库读到测试值 120。普通发布维持冻结。 |
| 候选 GTID 不足 | 候选只读，SQL 应用线程被暂停，I/O 线程已接收但尚未应用旧主事务。按实际候选 GTID 提交恢复被拒绝，待迁移修订 2 和两源读路由保持原值。 |
| 取消事务实际回滚并丢失应答 | 故障端返回 `ErrShadowRoutePublicationUnknown`。独立连接和消费者进程均确认 `Pending=true`、修订仍为 2，原目标继续可读。 |
| 取消事务实际提交并丢失应答 | 原主保持可写且候选只读，在原主围栏下三方对账后提交。独立连接确认修订 3、`Pending=false`、`LastCanceledRevision=2`，原物理成员和影子目标保留。原证明失效，修订 2 的旧恢复与取消请求无法处置修订 3 的新准备。 |
| 跨主恢复实际提交并丢失应答 | 重新准备修订 3 后实际停旧主，消费者仍读原影子。两套 GTID 写链在新主追平，三方对账后提交。独立连接确认迁移已生效，修订 4，绑定新成员并保留影子目标。 |
| 最终发布和其他源 | 两源在新主分别完成修订 5、6、7 的回退、切换、再次回退，每次重启消费者都恢复正确目标。另一源保持修订 1 和完整原路由，并验证新写入从 110 更新至 120，三方数据一致。 |

取消时 MySQL 和 MariaDB 的原主共同位点分别为 `mysql-bin.000024:4345`、`mariadb-bin.000024:27949`。在新主恢复的共同位点分别为 `mysql-promoted.000001:3216`、`mariadb-promoted.000002:28035`。已追平后，从开始对账到读回提交结果分别用时 32 ms 和 34 ms。

故障注入在真实 InnoDB 事务提交或回滚完成后丢弃应答，并取消本次写操作的上下文，覆盖 SDK 当次无法确认结果的路径。恢复通过权威库上的独立连接完成。生产取消仍要求部署侧确认原绑定成员是唯一写主。首次夹具初始化因复制状态命令隐藏列名而误判线程未就绪，保留列名后复跑通过。原三个测试容器已恢复，临时复制账号与一次性从库已回收。

原持久路由发布、MySQL 和 MariaDB 元数据库并发提交及 40 ms 慢目标压力复测均通过，合计 46.669 秒。Go 竞态测试、`go vet` 和 Compose 配置检查通过。

## 12. 2026-11-27 多发布器并发验收

演练使用 MySQL 8.4.13、MariaDB 10.6.28 和 Doris 3.0.8 容器。同源和双源并行专项各用一组全新的一次性候选。双源并行另外使用独立 MySQL InnoDB 元库，使两个原主停机时，两个发布器和消费者仍可访问同一份权威注册表。隔离元库的测试账号具备创建及清理测试库和只读消费者账号的权限，两边候选分别保留写入围栏。

**双源并行。** 实际隔离两个旧主，两套候选各自完成 GTID 续传和源库、旧 Doris、影子 Doris 对账。两个独立发布器在同一注册表更新各自的行，两笔真实事务都到达 `COMMIT` 前的测试闸门。此时独立消费者仍读各自原修订和目标。放行 MySQL 提交后，MariaDB 仍保持原修订和 `Pending=true`。两笔提交完成后，每源只推进一个修订，重复使用各自的准备修订均被拒绝。消费者重启后验证如下，本专项耗时 19.92 秒。

| 源 | 准备 → 生效修订 | 新主共同位点 | 重启后实际 Doris 读结果 |
| --- | --- | --- | --- |
| MySQL | 2 → 3 | `mysql-promoted.000001:2530` | 原影子库，样本 120 |
| MariaDB | 1 → 2 | `mariadb-promoted.000002:3710` | 原旧库，样本 130 |

**同源恢复与取消竞争。** 两种源分别使用另一种引擎作为权威元库。旧主可写时先让取消发布器完成对账，再将请求延迟在注册表事务开始前。实际隔离旧主并提升候选后，恢复发布器取得相同准备修订 3 的行锁。在 `INFORMATION_SCHEMA.INNODB_TRX` 观测到已放行的取消请求进入 `LOCK WAIT`，才放行恢复发布器提交。恢复事务成功，迟到的取消返回 `ErrShadowRouteRevisionChanged`，权威记录只从修订 3 进入 4，并保留原影子库。MySQL 的生效位点为 `mysql-promoted.000001:3216`，MariaDB 为 `mariadb-promoted.000002:28020`。同源专项共 38.50 秒，MySQL 24.89 秒，MariaDB 13.61 秒。

**未知提交与重复请求。** 两种元库上均强制取消和恢复事务实际回滚后丢失应答，独立读回证明原准备修订仍待处置，在围栏内对账后可使用原修订重新提交。实际取消或恢复成功但丢失应答时，独立读回分别确认修订 3 和 4。换发布器原样重复请求均被拒绝，提交故障注入没有再次触发。修订 4 的消费者重启后读到新主同步的样本 130，后续还验证修订 5、6、7 的回退、切换和再次回退。另一源保留原路由，实际读结果随新写入从 110 更新至 120。

Go 单元和竞态测试、`go vet` 与 Compose 配置检查通过。一次性候选、复制账号和独立元库均已回收，原三个测试容器恢复运行。

## 13. 2026-11-27 权威路由元库故障切换验收

演练使用 MySQL 8.4.13、MariaDB 10.6.28 和 Doris 3.0.8 隔离容器，依次以 MySQL 和 MariaDB 作为权威元库，各用一次性 GTID 从库、隔离注册表和只读消费者账号。每轮只停承担权威注册表的数据库，另一种引擎继续提供两个逻辑业务源。专项用时 24.98 秒。

| 权威元库 | 原 `server_id` → 提升后 `server_id` | 候选提升前核对的最终 GTID | 子用例耗时 |
| --- | --- | --- | ---: |
| MySQL | 7107 → 7207 | `901a7bbd-ea05-11f1-abe7-16caeccb45c6:1-4973` | 18.42 s |
| MariaDB | 7106 → 7206 | `0-7106-4881` | 6.56 s |

- **切换前的持久状态。** 一条路由停在修订 1、`Pending=true`、读旧库。另一条路由已经提交修订 2、读影子库，提交应答由测试注入丢失。候选暂停 SQL 应用，确认收到了包含两条记录的完整元库 GTID，但尚未应用。
- **停机与旧主隔离。** 实际停止旧元库，确认容器停止且 SQL 连接被拒绝。旧连接上的路由读取、校验、准备、回退、恢复和取消均因元库断连失败。原独立消费者查询两个源都返回错误且无路由，业务源的新写入仍同步到 Doris。
- **提升与读回。** 恢复候选的 SQL 应用，确认已执行 GTID 与记录的完整基线相等后才提升。新发布器用原配置打开既有注册表，两条路由的完整载荷和发布令牌逐字节一致。只读消费者切到候选并重启后，分别按修订 1 读原旧库、按修订 2 读原影子库，两个实际 Doris 查询均返回停机期间同步的新值 120。
- **旧请求与恢复发布。** 新发布器拒绝已提交路由针对修订 1 的准备、恢复及取消请求，并拒绝旧实例签发的回退证明。待迁移源仍保持普通发布冻结，重试同一准备保留原令牌。重新围栏和对账后，在新元库上取消该源的准备并推进至修订 2，另一条路由保持原记录。

首轮旧版测试已观察到真实 TCP 断连，错误文字只存在于堆栈，随后改为按底层传输错误断言，完整双源复测通过。Go 竞态检查、`go vet` 与 Compose 配置检查通过。测试结束先将临时提升节点重新设为只读，再恢复原元库。两个候选和临时复制账号均已回收，原三个容器继续运行。

本结果覆盖候选 relay log 已接收完整停写基线的情形。部署侧负责唯一主隔离、核对最后确认 GTID、切换权威连接池及按副本流程接回原主。旧主最后提交未被任何候选接收的情形需要按事故流程核对和恢复。

## 14. 2026-11-27 生产风险修复回归

在现有 MySQL 8.4.13、MariaDB 10.6.28、Doris 3.0.8 隔离容器执行第 8 节严格竞态回归。`pkg/cdc` 耗时 67.335 秒，核心包和源适配器、缓冲、监控包全部通过。真实库用例全部执行。`go vet`、Compose 配置检查和 Vue 生产构建通过。

| 场景 | 验收结果 |
| --- | --- |
| 写入所有权与控制表回收 | 实际启动独立测试进程，用不同目录、源和代次访问同一 Doris 目标，收到所有权冲突。原目录重开仍可确认归属。重复执行回收时，快照基线、最近检查点、未确认回执和其核对期限均保留。 |
| 两源断连与超限续传 | 断开实际复制连接，两源均从原已落盘位置自动重连，补齐断连期间提交的事务。每源 8,192 行的事务在低预算下保持原检查点，提高预算后从原卷重放，Doris 分 6,486 和 1,706 行两批交付，行数、字段长度和金额总和一致。MySQL 开启会话级压缩的测试事务也已写入并核对。 |
| 导入、快照和 DDL | 实际 Doris 响应丢失回归确认原 Label 仅有一次 PUT。受控故障测试核对持久化的重查期限、快照准备记录前后恢复及原目标验证、慢 DDL 使用原作业 ID 直至完成。真实两源的全量、增量、DDL 审批与破坏性变更回归通过。 |
| 运维和兼容性 | 竞态用例验证慢控制落盘期间可按期限返回、完成信号等资源释放。另验证旧缓冲、MariaDB 多域校验、路由缓存的 TTL 和鉴权后的 YAML 重载。 |
