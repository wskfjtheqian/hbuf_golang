# hcdc 监控 SDK

`github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/monitor` 管理已配置的同步源，提供状态查询、回调、启停、DDL 审批和配置重载。宿主应用调用 Go 方法，并自行决定如何鉴权、记录操作及对外传输状态。

## 接入

`monitor.New(serviceCtx)` 创建源清单为空的控制器。首次调用 `Reload(ctx, cfg)` 校验并加载 `hcdc.Config`，然后由应用按 `SourceID` 调用 `Start`。加载的源初始为 `not_started`。`serviceCtx` 覆盖宿主服务的运行周期。配置加载和启动使用各自有期限的上下文，避免请求结束时取消整个流水线。配置示例和发布规则见 [动态配置](../REFACTOR_DESIGN.md#配置动态更新) 和 [生产操作手册](../PRODUCTION_RUNBOOK.md#5-每源监控与放行条件)。

下面示例启动一个源并监听它的状态。需要同时处理业务请求的宿主，可把 `WatchStatus` 放在独立 goroutine。

```go
package example

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/monitor"
)

func monitorSource(serviceCtx context.Context, cfg *hcdc.Config, sourceID string) (resultErr error) {
	controller, err := monitor.New(serviceCtx)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, controller.Close(shutdownCtx))
	}()

	reloadCtx, cancelReload := context.WithTimeout(serviceCtx, 30*time.Second)
	err = controller.Reload(reloadCtx, cfg)
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
		log.Printf("source=%s state=%s durable=%d delivered=%d pending_bytes=%d error=%q",
			s.ID, s.State, s.Durable.Sequence, s.Delivered.Sequence, s.PendingBytes, s.Error)
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
| `Pause(ctx, sourceID, reason)`、`Resume(ctx, sourceID)` | 持久化或解除手动交付暂停。暂停期间采集继续到缓冲上限。 |
| `ApproveDDL(ctx, sourceID, id)` | 审批当前队首可执行 DDL。先读取单源状态，`id` 取自其中的 `PendingDDL.ID`。手动暂停另用 `Resume` 解除。 |
| `Stop(ctx, sourceID)` | 停止指定源。操作超时后，用状态和可执行动作确认原句柄已释放，再重启或交接。 |
| `Reload(ctx, cfg)` | 首次加载或更新完整的源配置。已运行且变更的源按代次交接，新源等待手动启动，移除的源停止。更新失败后读回状态及检查点核对恢复结果。 |
| `Close(ctx)` | 停止接收启动和重载，等待所有受管源关闭。宿主退出时用独立的关闭上下文。 |

`WatchStatus` 是阻塞方法。慢回调期间的中间变化可合并，取消监听只结束订阅。若源尚未启动，发送一次当前状态后返回。收到 `stopped` 或 `failed` 后监听结束，流水线的错误通过快照的 `Error` 字段呈现。完成重启或重载交接后，重新为新句柄调用 `WatchStatus`。

控制操作可用 `errors.Is` 区分 `ErrSourceNotFound`、`ErrInvalidState`、`ErrClosed` 及上下文错误。关闭超时后可再次调用 `Close`，等待资源释放。

`SourceStatus` 包含 `ReadCursor`、`Durable`、`Delivered`、`PendingBytes`、背压与重试原因、待处理 DDL、零日期计数及 `Actions`。用 `Actions` 显示当前可用的启停、暂停及恢复动作，实际调用仍需处理状态变化导致的错误。零日期行数和检查点序号在 Go 中为 `uint64`，JSON 使用十进制字符串，便于浏览器保留 64 位整数精度。浏览器组件由宿主提供数据和控制动作。 [状态定义](status.go)
