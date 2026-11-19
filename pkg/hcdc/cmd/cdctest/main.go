// 命令 cdctest 是一个 hcdc 的集成测试入口：用真实的 MySQL（源）和 Doris（目标）
// 跑一次 CDC 全量 + 增量同步，用于验证 game / game_in / game_usa 三个库的同步链路。
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

var (
	mysqlHost = flag.String("mysql", envOr("CDC_MYSQL_HOST", "127.0.0.1:3316"), "MySQL 源地址")
	mysqlUser = flag.String("mysql-user", envOr("CDC_MYSQL_USER", "root"), "MySQL 用户名")
	mysqlPass = flag.String("mysql-pass", envOr("CDC_MYSQL_PASS", "123456"), "MySQL 密码")
	dorisHost = flag.String("doris", envOr("CDC_DORIS_HOST", "127.0.0.1:9030"), "Doris FE 地址")
	dorisLoad = flag.String("doris-load", envOr("CDC_DORIS_LOAD_URL", "http://127.0.0.1:8040/"), "Doris Stream Load 地址")
	dorisUser = flag.String("doris-user", envOr("CDC_DORIS_USER", "admin"), "Doris 用户名")
	dorisPass = flag.String("doris-pass", envOr("CDC_DORIS_PASS", ""), "Doris 密码")
	logDir    = flag.String("log-dir", "./logs", "本地 CSV 缓冲目录")
	runFor    = flag.Duration("run", 2*time.Minute, "运行时长，到期自动退出")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 捕获 Ctrl+C / SIGTERM，优雅退出。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		hlog.Warn(ctx, "收到退出信号，停止同步")
		cancel()
	}()

	hlog.Info(ctx, "=== hcdc 集成测试开始 ===")
	hlog.Info(ctx, "MySQL: %s (user=%s), Doris: %s load=%s", *mysqlHost, *mysqlUser, *dorisHost, *dorisLoad)

	cdc := hcdc.NewHCDC()
	cfg := &hcdc.Config{
		Canals: []hcdc.CanalConfig{
			{
				InstanceId: 1,
				Host:       *mysqlHost,
				Username:   *mysqlUser,
				Password:   *mysqlPass,
				Schema:     "game",
				// 三个目标库：game / game_in / game_usa（用 $ 锚定，避免误匹配 game_temp1）
				IncludeDBs:    []string{"^game$", "^game_in$", "^game_usa$"},
				ExcludeDBs:    []string{},
				IncludeTables: []string{"(.*)"},
				ExcludeTables: []string{},
				LogDir:        *logDir,
			},
		},
		Doris: &hcdc.DorisConfig{
			Host:     *dorisHost,
			Username: *dorisUser,
			Password: *dorisPass,
			LoadURL:  *dorisLoad,
		},
	}

	hlog.Info(ctx, "正在初始化 HCDC（建库建表 + 全量拉取）...")
	if err := cdc.SetConfig(ctx, cfg); err != nil {
		hlog.Error(ctx, "SetConfig 失败: %v", err)
		herror.PrintStack(ctx, err)
		os.Exit(1)
	}
	hlog.Info(ctx, "HCDC 初始化完成，开始增量同步")

	// 定期打印状态与指标，直到超时或被中断。
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	timeout := time.After(*runFor)

	for {
		select {
		case <-ctx.Done():
			hlog.Info(ctx, "测试结束（收到退出信号）")
			printReport(ctx, cdc)
			return
		case <-timeout:
			hlog.Info(ctx, "测试结束（运行时长到期 %s）", *runFor)
			printReport(ctx, cdc)
			return
		case <-ticker.C:
			printReport(ctx, cdc)
		}
	}
}

func printReport(ctx context.Context, cdc *hcdc.HCDC) {
	state := cdc.GetSyncState()
	m := cdc.GetMetrics()
	hlog.Info(ctx, "--- 状态: %s | reason=%s ---", state.Status, state.Reason)
	hlog.Info(ctx, "采集: rows=%d bytes=%d errors=%d | 上传: files=%d rows=%d bytes=%d errors=%d avgMs=%d maxMs=%d",
		m.CaptureRows, m.CaptureBytes, m.CaptureErrors,
		m.UploadFiles, m.UploadRows, m.UploadBytes, m.UploadErrors, m.UploadAvgMs, m.UploadMaxMs)
	hlog.Info(ctx, "DDL: count=%d errors=%d | 暂停=%d 恢复=%d | 延迟: avgMs=%d maxMs=%d | 积压: files=%d bytes=%d",
		m.DDLCount, m.DDLErrors, m.PauseCount, m.ResumeCount, m.DelayAvgMs, m.DelayMaxMs, m.PendingFiles, m.PendingBytes)
}
