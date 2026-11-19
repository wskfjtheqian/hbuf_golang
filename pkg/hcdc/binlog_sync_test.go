package hcdc_test

// 本文件是 hcdc 的端到端增量同步（binlog）集成测试：
//
//	MySQL(源, 开启 binlog + ROW 格式) --binlog--> Canal --CSV--> Worker --Stream Load--> Doris(目标)
//
// 覆盖：
//  1. 全量同步：SetConfig 后源表已有数据应完整落到 Doris。
//  2. 增量 DML：INSERT / UPDATE / DELETE 经 binlog 捕获后同步到 Doris，最终一致。
//  3. 增量 DDL：源表结构变化（如 ALTER TABLE ADD COLUMN）触发自动暂停。
//
// 与 cdc_test.go 一致，本测试需要真实的 MySQL 与 Doris 环境，通过环境变量注入连接信息；
// 默认跳过，避免在没有环境时误报失败。运行方式：
//
//	CDC_MYSQL_HOST=127.0.0.1:3316 CDC_DORIS_HOST=127.0.0.1:9030 \
//	CDC_DORIS_LOAD_URL=http://127.0.0.1:8040/ go test ./pkg/hcdc -run Test_Binlog -v -timeout 10m

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
)

// binlogTestEnv 封装集成测试所需的连接与目标库名。
type binlogTestEnv struct {
	mysqlDSN string
	dorisDSN string
	loadURL  string
	dorisCfg *hcdc.DorisConfig
	// schema 是本次测试专用的源库名，测试结束会清理，避免污染业务库。
	schema string
	// flavor 是源数据库类型（MySQL 留空，MariaDB 为 "mariadb"）。
	flavor string
}

// newBinlogTestEnv 构造测试环境；环境变量 CDC_RUN_INTEGRATION 非空时才真正运行。
func newBinlogTestEnv(t *testing.T) *binlogTestEnv {
	t.Helper()
	if os.Getenv("CDC_RUN_INTEGRATION") == "" {
		t.Skip("跳过 binlog 集成测试：需要真实 MySQL+Doris，设置 CDC_RUN_INTEGRATION=1 并配置连接环境变量后运行")
	}

	mysqlHost := envOr("CDC_MYSQL_HOST", defaultMysqlHost)
	mysqlUser := envOr("CDC_MYSQL_USER", defaultMysqlUser)
	mysqlPass := envOr("CDC_MYSQL_PASS", defaultMysqlPass)
	// 源数据库类型：MySQL 留空即可；若源是 MariaDB，需设 CDC_MYSQL_FLAVOR=mariadb，
	// 否则 go-mysql 无法正确解析 MariaDB 的 GTID 事件，导致增量 DML 丢失。
	mysqlFlavor := os.Getenv("CDC_MYSQL_FLAVOR")

	dorisHost := envOr("CDC_DORIS_HOST", defaultDorisHost)
	dorisUser := envOr("CDC_DORIS_USER", defaultDorisUser)
	dorisPass := envOr("CDC_DORIS_PASS", defaultDorisPass)
	loadURL := envOr("CDC_DORIS_LOAD_URL", defaultLoadURL)

	env := &binlogTestEnv{
		mysqlDSN: fmt.Sprintf("%s:%s@tcp(%s)/?tls=false&parseTime=true", mysqlUser, mysqlPass, mysqlHost),
		dorisDSN: fmt.Sprintf("%s:%s@tcp(%s)/", dorisUser, dorisPass, dorisHost),
		loadURL:  loadURL,
		dorisCfg: &hcdc.DorisConfig{
			Host:     dorisHost,
			Username: dorisUser,
			Password: dorisPass,
			LoadURL:  loadURL,
		},
		schema: fmt.Sprintf("cdc_binlog_test_%d", time.Now().UnixNano()%1000000),
	}

	env.flavor = mysqlFlavor

	// 连通性自检：确保 MySQL / Doris 可用，失败时给出明确提示而非晦涩的下游错误。
	if err := env.pingMySQL(); err != nil {
		t.Fatalf("无法连接 MySQL (%s): %v", mysqlHost, err)
	}
	if err := env.pingDoris(); err != nil {
		t.Fatalf("无法连接 Doris (%s): %v", dorisHost, err)
	}
	return env
}

func (e *binlogTestEnv) pingMySQL() error {
	db, err := sql.Open("mysql", e.mysqlDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.Ping()
}

func (e *binlogTestEnv) pingDoris() error {
	db, err := sql.Open("mysql", e.dorisDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.Ping()
}

// execMySQL 在源 MySQL 上执行 SQL（用于建库建表、写入、DDL）。
func (e *binlogTestEnv) execMySQL(t *testing.T, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("mysql", e.mysqlDSN)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec mysql %q: %v", query, err)
	}
}

// queryInt 在指定连接上执行返回单个整数的查询（如 SELECT COUNT(*)）。
func queryInt(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// queryString 在指定连接上执行返回单个字符串的查询。
func queryString(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var s string
	if err := db.QueryRow(query, args...).Scan(&s); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return s
}

// setupSource 在源 MySQL 建库建表并写入全量数据。
func (e *binlogTestEnv) setupSource(t *testing.T) {
	t.Helper()
	e.execMySQL(t, "DROP DATABASE IF EXISTS `"+e.schema+"`")
	e.execMySQL(t, "CREATE DATABASE `"+e.schema+"`")
	e.execMySQL(t, fmt.Sprintf(`
		CREATE TABLE `+"`%s`.`user`"+` (
			id INT NOT NULL,
			name VARCHAR(64) NOT NULL,
			age INT NOT NULL DEFAULT 0,
			PRIMARY KEY (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, e.schema))

	// 全量初始数据（CDC 启动前已存在，应被 SetConfig 的全量拉取同步）。
	e.execMySQL(t, `INSERT INTO `+"`"+e.schema+"`.`user`"+` (id, name, age) VALUES (1, 'alice', 20), (2, 'bob', 30), (3, 'carol', 40)`)
}

// cleanup 清理源与目标上的测试库。
func (e *binlogTestEnv) cleanup(t *testing.T) {
	t.Helper()
	e.execMySQL(t, "DROP DATABASE IF EXISTS `"+e.schema+"`")
	db, err := sql.Open("mysql", e.dorisDSN)
	if err == nil {
		_, _ = db.Exec("DROP DATABASE IF EXISTS `" + e.schema + "`")
		db.Close()
	}
}

// waitFor 轮询直到条件满足或超时，返回最后一次结果；避免依赖 sleep 的脆弱时序。
func waitFor(t *testing.T, timeout time.Duration, desc string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("等待超时（%s）：%s", timeout, desc)
}

// Test_BinlogSync 验证全量 + 增量 DML 同步的最终一致性。
func Test_BinlogSync(t *testing.T) {
	env := newBinlogTestEnv(t)
	env.setupSource(t)
	defer env.cleanup(t)

	ctx := context.Background()
	cdc := hcdc.NewHCDC()
	cfg := &hcdc.Config{
		Canals: []hcdc.CanalConfig{{
			InstanceId:    1,
			Host:          envOr("CDC_MYSQL_HOST", defaultMysqlHost),
			Username:      envOr("CDC_MYSQL_USER", defaultMysqlUser),
			Password:      envOr("CDC_MYSQL_PASS", defaultMysqlPass),
			Schema:        env.schema,
			Flavor:        env.flavor,
			IncludeDBs:    []string{"^" + env.schema + "$"},
			ExcludeDBs:    []string{},
			IncludeTables: []string{"(.*)"},
			ExcludeTables: []string{},
			LogDir:        t.TempDir(),
		}},
		Doris: env.dorisCfg,
	}
	if err := cdc.SetConfig(ctx, cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	dorisDB, err := sql.Open("mysql", env.dorisDSN)
	if err != nil {
		t.Fatalf("open doris: %v", err)
	}
	defer dorisDB.Close()

	// 清理该 instanceId 在 log_position 表中的残留，保证测试可重复运行
	// （否则上次运行留下的位点会触发断点续传、跳过全量拉取）。
	defer func() {
		_, _ = dorisDB.Exec("DELETE FROM cdc_record.log_position WHERE instance_id = 1")
	}()

	// 1. 全量同步：3 行应落地。
	waitFor(t, 30*time.Second, "全量同步", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user`") == 3
	})
	if got := queryString(t, dorisDB, "SELECT name FROM `"+env.schema+"`.`user` WHERE id = 2"); got != "bob" {
		t.Fatalf("全量数据不一致：id=2 期望 bob，实际 %q", got)
	}

	// 2. 增量 DML：INSERT / UPDATE / DELETE 经 binlog 捕获后同步。
	env.execMySQL(t, `INSERT INTO `+"`"+env.schema+"`.`user`"+` (id, name, age) VALUES (4, 'dave', 50)`)
	env.execMySQL(t, `UPDATE `+"`"+env.schema+"`.`user`"+` SET age = 31 WHERE id = 2`)
	env.execMySQL(t, `DELETE FROM `+"`"+env.schema+"`.`user`"+` WHERE id = 1`)

	// 以 INSERT 的 id=4 出现作为「增量 binlog 已同步」的明确信号，
	// 避免用 COUNT 判断（全量与增量的行数可能恰好抵消而误判）。
	waitFor(t, 30*time.Second, "增量 DML 同步（id=4 出现）", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user` WHERE id = 4") == 1
	})
	// UPDATE 与 DELETE 是独立事件，需各自等待其 binlog 事件同步，而非立即断言。
	waitFor(t, 30*time.Second, "UPDATE 同步（id=2 age=31）", func() bool {
		return queryInt(t, dorisDB, "SELECT age FROM `"+env.schema+"`.`user` WHERE id = 2") == 31
	})
	waitFor(t, 30*time.Second, "DELETE 同步（id=1 删除）", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user` WHERE id = 1") == 0
	})

	// 2.5 批量 INSERT（单条语句多行，验证多行 RowsEvent 不漏行）。
	// go-mysql 会把批量 INSERT 的多行放在同一个 RowsEvent.Rows 里，若 OnRow 只取首行会丢行。
	env.execMySQL(t, `INSERT INTO `+"`"+env.schema+"`.`user`"+` (id, name, age) VALUES (10, 'b1', 1), (11, 'b2', 2), (12, 'b3', 3), (13, 'b4', 4), (14, 'b5', 5)`)
	waitFor(t, 30*time.Second, "批量 INSERT 同步（5 行全部落地）", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user` WHERE id BETWEEN 10 AND 14") == 5
	})

	// 3. 采集与上传指标应一致（无错误、无丢数据）。
	m := cdc.GetMetrics()
	if m.CaptureErrors != 0 || m.UploadErrors != 0 {
		t.Fatalf("同步存在错误：capture=%d upload=%d", m.CaptureErrors, m.UploadErrors)
	}
	t.Logf("采集 rows=%d bytes=%d | 上传 files=%d rows=%d | 延迟 avg=%dms max=%dms",
		m.CaptureRows, m.CaptureBytes, m.UploadFiles, m.UploadRows, m.DelayAvgMs, m.DelayMaxMs)
}

// Test_BinlogDDLTriggersPause 验证源表结构变化会触发自动暂停。
func Test_BinlogDDLTriggersPause(t *testing.T) {
	env := newBinlogTestEnv(t)
	env.setupSource(t)
	defer env.cleanup(t)

	ctx := context.Background()
	cdc := hcdc.NewHCDC()
	cfg := &hcdc.Config{
		Canals: []hcdc.CanalConfig{{
			InstanceId:    2,
			Host:          envOr("CDC_MYSQL_HOST", defaultMysqlHost),
			Username:      envOr("CDC_MYSQL_USER", defaultMysqlUser),
			Password:      envOr("CDC_MYSQL_PASS", defaultMysqlPass),
			Schema:        env.schema,
			Flavor:        env.flavor,
			IncludeDBs:    []string{"^" + env.schema + "$"},
			IncludeTables: []string{"(.*)"},
			LogDir:        t.TempDir(),
		}},
		Doris: env.dorisCfg,
	}
	if err := cdc.SetConfig(ctx, cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	dorisDB, err := sql.Open("mysql", env.dorisDSN)
	if err != nil {
		t.Fatalf("open doris: %v", err)
	}
	defer dorisDB.Close()

	// 清理该 instanceId 在 log_position 表中的残留，保证测试可重复运行。
	defer func() {
		_, _ = dorisDB.Exec("DELETE FROM cdc_record.log_position WHERE instance_id = 2")
	}()

	// 等全量完成，此时应处于 running 状态。
	waitFor(t, 30*time.Second, "全量同步完成", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user`") == 3
	})
	if st := cdc.GetSyncState(); st.Status != hcdc.SyncRunning {
		t.Fatalf("初始化后应处于 running，实际 %s", st.Status)
	}

	// 触发源表结构变化（加列），应自动暂停。
	env.execMySQL(t, `ALTER TABLE `+"`"+env.schema+"`.`user`"+` ADD COLUMN email VARCHAR(128) NULL`)

	waitFor(t, 30*time.Second, "DDL 触发自动暂停", func() bool {
		return cdc.GetSyncState().Status == hcdc.SyncPaused
	})
	st := cdc.GetSyncState()
	if st.Table != "user" {
		t.Fatalf("暂停原因应指向 user 表，实际 %q", st.Table)
	}

	// 手动恢复后应回到 running。
	cdc.Resume(ctx)
	waitFor(t, 5*time.Second, "手动恢复", func() bool {
		return cdc.GetSyncState().Status == hcdc.SyncRunning
	})
}

// Test_BinlogDeleteSynced 单独验证 DELETE 的位点推进与最终一致（UNIQUE KEY 表 MERGE 语义）。
func Test_BinlogDeleteSynced(t *testing.T) {
	env := newBinlogTestEnv(t)
	env.setupSource(t)
	defer env.cleanup(t)

	ctx := context.Background()
	cdc := hcdc.NewHCDC()
	cfg := &hcdc.Config{
		Canals: []hcdc.CanalConfig{{
			InstanceId:    3,
			Host:          envOr("CDC_MYSQL_HOST", defaultMysqlHost),
			Username:      envOr("CDC_MYSQL_USER", defaultMysqlUser),
			Password:      envOr("CDC_MYSQL_PASS", defaultMysqlPass),
			Schema:        env.schema,
			Flavor:        env.flavor,
			IncludeDBs:    []string{"^" + env.schema + "$"},
			IncludeTables: []string{"(.*)"},
			LogDir:        t.TempDir(),
		}},
		Doris: env.dorisCfg,
	}
	if err := cdc.SetConfig(ctx, cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	dorisDB, err := sql.Open("mysql", env.dorisDSN)
	if err != nil {
		t.Fatalf("open doris: %v", err)
	}
	defer dorisDB.Close()

	// 清理该 instanceId 在 log_position 表中的残留，保证测试可重复运行。
	defer func() {
		_, _ = dorisDB.Exec("DELETE FROM cdc_record.log_position WHERE instance_id = 3")
	}()

	waitFor(t, 30*time.Second, "全量同步完成", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user`") == 3
	})

	// 连续插入再删除，最终 Doris 侧应无该行。
	env.execMySQL(t, `INSERT INTO `+"`"+env.schema+"`.`user`"+` (id, name, age) VALUES (100, 'ghost', 1)`)
	env.execMySQL(t, `DELETE FROM `+"`"+env.schema+"`.`user`"+` WHERE id = 100`)

	waitFor(t, 30*time.Second, "DELETE 同步", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user` WHERE id = 100") == 0
	})
	if got := queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user`"); got != 3 {
		t.Fatalf("总行数应回到 3，实际 %d", got)
	}
}

// Test_BinlogResumeFromPersistedPosition 验证位点持久化 + 断点续传：
//  1. 首次 SetConfig：全量同步完成，位点写入 Doris 的 cdc_record.log_position。
//  2. 模拟进程重启：新建 HCDC、复用同一 instanceId，应读到上次位点、跳过全量，
//     之后新增 DML 仍能经 binlog 续传同步到 Doris。
func Test_BinlogResumeFromPersistedPosition(t *testing.T) {
	env := newBinlogTestEnv(t)
	env.setupSource(t)
	defer env.cleanup(t)

	ctx := context.Background()
	// 用随机 instanceId，避免与历史残留的 log_position 记录冲突。
	const instanceId hcdc.InstanceId = 4242
	// 复用固定 LogDir，模拟重启后本地缓冲不丢失（不能用 t.TempDir 的随机目录）。
	logDir := t.TempDir()

	dorisDB, err := sql.Open("mysql", env.dorisDSN)
	if err != nil {
		t.Fatalf("open doris: %v", err)
	}
	defer dorisDB.Close()

	// 清理该 instanceId 在 log_position 表中的残留，保证测试可重复运行。
	defer func() {
		_, _ = dorisDB.Exec("DELETE FROM cdc_record.log_position WHERE instance_id = 4242")
	}()

	mkCfg := func() *hcdc.Config {
		return &hcdc.Config{
			Canals: []hcdc.CanalConfig{{
				InstanceId:    instanceId,
				Host:          envOr("CDC_MYSQL_HOST", defaultMysqlHost),
				Username:      envOr("CDC_MYSQL_USER", defaultMysqlUser),
				Password:      envOr("CDC_MYSQL_PASS", defaultMysqlPass),
				Schema:        env.schema,
				Flavor:        env.flavor,
				IncludeDBs:    []string{"^" + env.schema + "$"},
				IncludeTables: []string{"(.*)"},
				LogDir:        logDir,
			}},
			Doris: env.dorisCfg,
		}
	}

	// 第一次启动：全量同步。
	cdc1 := hcdc.NewHCDC()
	if err := cdc1.SetConfig(ctx, mkCfg()); err != nil {
		t.Fatalf("首次 SetConfig: %v", err)
	}
	waitFor(t, 30*time.Second, "首次全量同步", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user`") == 3
	})

	// 触发一条真实 binlog 增量并等它同步：位点（真实 binlog 位点）才会写入 log_position。
	// 注意：全量数据的位点是伪位点（Name="SELECT"），不会被记录，只有真实 binlog 增量会推进并持久化位点。
	env.execMySQL(t, `INSERT INTO `+"`"+env.schema+"`.`user`"+` (id, name, age) VALUES (400, 'before_restart', 1)`)
	waitFor(t, 30*time.Second, "首次增量同步", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user` WHERE id = 400") == 1
	})

	// 位点应已持久化到 log_position。
	waitFor(t, 30*time.Second, "位点持久化", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM cdc_record.log_position WHERE instance_id = 4242") == 1
	})

	// 关闭第一个实例（模拟进程退出）。
	_ = cdc1.SetConfig(ctx, nil)

	// 重启：复用同一 instanceId，应跳过全量、从上次位点续传。
	cdc2 := hcdc.NewHCDC()
	if err := cdc2.SetConfig(ctx, mkCfg()); err != nil {
		t.Fatalf("重启 SetConfig: %v", err)
	}

	// 重启后新增数据，应被续传同步（证明从持久化位点恢复，而非重新全量）。
	env.execMySQL(t, `INSERT INTO `+"`"+env.schema+"`.`user`"+` (id, name, age) VALUES (500, 'resumed', 99)`)
	waitFor(t, 30*time.Second, "重启后续传增量", func() bool {
		return queryInt(t, dorisDB, "SELECT COUNT(*) FROM `"+env.schema+"`.`user` WHERE id = 500") == 1
	})

	// 位点应持续推进（position 不小于首次记录值）。
	var pos int64
	if err := dorisDB.QueryRow("SELECT `position` FROM cdc_record.log_position WHERE instance_id = 4242").Scan(&pos); err != nil {
		t.Fatalf("读位点失败: %v", err)
	}
	t.Logf("断点续传成功，最终位点 position=%d", pos)
}

