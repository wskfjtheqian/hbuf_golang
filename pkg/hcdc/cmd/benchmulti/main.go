// 命令 benchmulti 是 hcdc 的多实例性能基准：量化「多 MySQL 实例」场景下
// 吞吐随实例数的变化曲线，并对比两种落表模式——
//
//	separate: N 个实例各自写入独立的 Doris 表（多业务库汇聚，互不干扰）
//	shared:   N 个实例写入同一张 Doris UNIQUE KEY 表（分库分表汇聚，目标表 MERGE 竞争）
//
// 实现要点：hcdc 不做目标库表重命名，Doris 目标表名 = 源库表名。因此
//「多实例写同一张 Doris 表」=「多实例用相同的源库名」。shared 模式要求
// 多台真实 MySQL（每台各建同名库），否则同一 MySQL 上的多 Canal 会重复采集同一份 binlog。
//
// 用法（多实例用逗号分隔 host:port）：
//
//	go run ./pkg/hcdc/cmd/benchmulti -instances 1,2,4,8 -rows 200000 \
//	  -mysql "127.0.0.1:3316,127.0.0.1:3317,127.0.0.1:3318" -mode all
//
// 多台 MariaDB 可一键用 Docker 起（server-id 必须互不相同）：
//
//	for i in 2 3 4; do docker run -d --name benchmulti-mariadb-$i \
//	  -e MYSQL_ROOT_PASSWORD=123456 -p $((3315+i)):3306 mariadb:10.6 \
//	  --log-bin=mysql-bin --binlog-format=ROW --server-id=$i; done
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

var (
	// 多实例地址：逗号分隔，长度即实例数。若只给一个，则按「单机多库」模拟多实例
	// （每个实例一个独立库 + 独立 binlog 采集 Worker，等价于多台 MySQL 的行为）。
	mysqlHosts = flag.String("mysql", envOr("CDC_MYSQL_HOST", "127.0.0.1:3316"),
		"MySQL 实例地址，逗号分隔（多实例各自独立，单地址则单机多库模拟）")
	mysqlUser = flag.String("mysql-user", envOr("CDC_MYSQL_USER", "root"), "MySQL 用户名")
	mysqlPass = flag.String("mysql-pass", envOr("CDC_MYSQL_PASS", "123456"), "MySQL 密码")

	dorisHost = flag.String("doris", envOr("CDC_DORIS_HOST", "127.0.0.1:9030"), "Doris FE 地址")
	dorisLoad = flag.String("doris-load", envOr("CDC_DORIS_LOAD_URL", "http://127.0.0.1:8040/"), "Doris Stream Load 地址")
	dorisUser = flag.String("doris-user", envOr("CDC_DORIS_USER", "admin"), "Doris 用户名")
	dorisPass = flag.String("doris-pass", envOr("CDC_DORIS_PASS", ""), "Doris 密码")

	instances = flag.String("instances", "1,2,4,8", "要扫描的实例数，逗号分隔（如 1,2,4,8）")
	mode      = flag.String("mode", "all", "落表模式: separate | shared | all")
	rows     = flag.Int("rows", 200000, "每个实例写入的总行数")
	batch    = flag.Int("batch", 5000, "每批 INSERT 行数")
	rowWidth = flag.Int("row-width", 120, "每行约字节数")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	flag.Parse()
	ctx := context.Background()

	hosts := strings.Split(*mysqlHosts, ",")
	insts := parseInstances(*instances)

	hlog.Info(ctx, "=== hcdc 多实例压测 ===")
	hlog.Info(ctx, "MySQL实例=%d  Doris=%s  load=%s  每实例行数=%d  批量=%d  行宽=%d",
		len(hosts), *dorisHost, *dorisLoad, *rows, *batch, *rowWidth)
	hlog.Info(ctx, "扫描实例数: %v  模式: %s", insts, *mode)

	run := func(m string) {
		hlog.Info(ctx, "\n========== 模式 %s ==========", m)
		results := make([]string, 0, len(insts))
		for _, n := range insts {
			rps := runOne(ctx, hosts, n, m)
			results = append(results, fmt.Sprintf("实例数=%d → %.0f 行/s", n, rps))
		}
		hlog.Info(ctx, "--- 模式 %s 吞吐曲线 ---", m)
		for _, r := range results {
			hlog.Info(ctx, "  %s", r)
		}
	}

	switch *mode {
	case "separate":
		run("separate")
	case "shared":
		run("shared")
	default:
		run("separate")
		run("shared")
	}
}

// runOne 用 n 个 MySQL 实例各写 rows 行，返回端到端吞吐（总同步行数 / 总耗时）。
//
// 落表模式的实现原理：hcdc 不做目标库表重命名，Doris 目标表名 = 源库表名。
// 因此「多实例写同一张 Doris 表」等价于「多个实例的源库名相同」。
//   - separate：每个实例一个唯一源库名 benchmulti_<ts>_<i> → Doris 上 n 张表。
//   - shared：所有实例共用一个源库名 benchmulti_shared → Doris 上同一张表。
func runOne(ctx context.Context, hosts []string, n int, mode string) float64 {
	// runTag 保证每次 runOne（不同实例数扫描）用独立的库名，避免数据跨轮次累积。
	runTag := fmt.Sprintf("%d_%d", time.Now().UnixNano()%1e6, n)

	// 1. 确定源库名（shared 模式所有实例同名；separate 每实例唯一）。
	schemas := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if mode == "shared" {
			schemas = append(schemas, "benchmulti_shared_"+runTag)
		} else {
			schemas = append(schemas, fmt.Sprintf("benchmulti_%s_%d", runTag, i))
		}
	}

	// 2. 准备源库表（先 DROP 再建，确保干净无残留数据）。
	mdbs := make([]*sql.DB, 0, n)
	dsn := func(host string) string {
		return fmt.Sprintf("%s:%s@tcp(%s)/?parseTime=true&timeout=10s&readTimeout=60s&writeTimeout=60s",
			*mysqlUser, *mysqlPass, host)
	}
	for i := 0; i < n; i++ {
		host := hosts[i%len(hosts)]
		db, err := sql.Open("mysql", dsn(host))
		if err != nil {
			fatal(ctx, "open mysql %s: %v", host, err)
		}
		schema := schemas[i]
		if err := execAll(db,
			"CREATE DATABASE IF NOT EXISTS "+schema,
			"USE "+schema,
			"DROP TABLE IF EXISTS t",
			"CREATE TABLE t (id BIGINT NOT NULL, name VARCHAR(255), age INT, ts DATETIME, payload VARCHAR(512), PRIMARY KEY(id)) ENGINE=InnoDB",
		); err != nil {
			fatal(ctx, "prepare source %s: %v", host, err)
		}
		mdbs = append(mdbs, db)
		hlog.Info(ctx, "  实例[%d] %s 源库 %s 就绪", i, host, schema)
	}

	// 3. Doris 目标表由 hcdc 在 SetConfig 时根据源表结构自动创建（changeTable）。
	// 这里不预建表——手动预建会导致 hcdc 走 modifyTable 分支、因 schema 不一致报
	// 「No key column left」。只需预建库即可（hcdc 也会 CreateSchema）。
	dorisDSN := fmt.Sprintf("%s:%s@tcp(%s)/", *dorisUser, *dorisPass, *dorisHost)
	dd, err := sql.Open("mysql", dorisDSN)
	if err != nil {
		fatal(ctx, "open doris: %v", err)
	}
	defer dd.Close()
	seen := map[string]bool{}
	for _, s := range schemas {
		if seen[s] {
			continue
		}
		seen[s] = true
		if _, err := dd.Exec("CREATE DATABASE IF NOT EXISTS " + s); err != nil {
			fatal(ctx, "prepare doris db %s: %v", s, err)
		}
	}

	// 4. 组装 HCDC：n 个 Canal，每个指向一个实例库，独立 Worker。
	cfg := &hcdc.Config{
		Canals: make([]hcdc.CanalConfig, 0, n),
		Doris: &hcdc.DorisConfig{
			Host:     *dorisHost,
			Username: *dorisUser,
			Password: *dorisPass,
			LoadURL:  *dorisLoad,
		},
	}
	for i := 0; i < n; i++ {
		srvID := uint32(time.Now().UnixNano()&0x7fffffff + int64(i))
		cfg.Canals = append(cfg.Canals, hcdc.CanalConfig{
			InstanceId: hcdc.InstanceId(time.Now().UnixNano()&0x7fffffff + int64(i)),
			Host:       hosts[i%len(hosts)],
			Username:   *mysqlUser,
			Password:   *mysqlPass,
			Schema:     schemas[i],
			ServerID:   &srvID,
			Charset:    "utf8mb4",
			Flavor:     "mariadb",
			LogDir:     fmt.Sprintf("./logs_benchmulti_%d_%d", time.Now().UnixNano(), i),
			// 关键：只同步本测试库，避免采集到同一 MySQL 上的其他业务库（如 game.*）。
			IncludeDBs: []string{"^" + schemas[i] + "$"},
		})
	}

	cdc := hcdc.NewHCDC()
	if err := cdc.SetConfig(ctx, cfg); err != nil {
		fatal(ctx, "SetConfig: %v", err)
	}
	defer cdc.SetConfig(ctx, nil)

	// 4. 并发写入：n 个实例同时灌 rows 行。
	line := genLine(*rowWidth)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			insertRows(ctx, mdbs[i], *rows, *batch, i, line)
		}(i)
	}
	wg.Wait()
	writeElapsed := time.Since(start)
	totalRows := int64(*rows) * int64(n)
	hlog.Info(ctx, "  [%d 实例] 写入 %d 行耗时 %.2fs (源端 %.0f 行/s)",
		n, totalRows, writeElapsed.Seconds(), float64(totalRows)/writeElapsed.Seconds())

	// 5. 等待同步追平：总上传行数 >= 总写入行数。
	deadline := time.Now().Add(3 * time.Minute)
	for {
		m := cdc.GetMetrics()
		if m.UploadRows >= totalRows {
			break
		}
		if time.Now().After(deadline) {
			hlog.Warn(ctx, "  等待同步超时：当前 %d / 目标 %d", m.UploadRows, totalRows)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	elapsed := time.Since(start)
	m := cdc.GetMetrics()
	rps := float64(m.UploadRows) / elapsed.Seconds()
	mbps := float64(m.UploadBytes) / elapsed.Seconds() / 1024 / 1024
	hlog.Info(ctx, "  [%d 实例] 模式=%s 同步 %d 行 %.2fMB | 端到端 %.0f 行/s, %.2f MB/s",
		n, mode, m.UploadRows, float64(m.UploadBytes)/1024/1024, rps, mbps)

	return rps
}

func insertRows(ctx context.Context, db *sql.DB, total, batch, idx int, line string) {
	for off := 0; off < total; off += batch {
		var sb strings.Builder
		sb.WriteString("INSERT INTO t (id,name,age,ts,payload) VALUES ")
		end := off + batch
		if end > total {
			end = total
		}
		for i := off; i < end; i++ {
			if i > off {
				sb.WriteString(",")
			}
			// id 加实例偏移，避免多实例汇聚到同一张表时主键冲突。
			id := int64(idx)*int64(total) + int64(i)
			sb.WriteString(fmt.Sprintf("(%d,'name_%d',%d,'2026-01-01 00:00:00','%s')", id, id, id%100, line))
		}
		if _, err := db.Exec(sb.String()); err != nil {
			fatal(ctx, "instance %d insert: %v", idx, err)
		}
	}
}

func parseInstances(s string) []int {
	parts := strings.Split(s, ",")
	set := map[int]bool{}
	var out []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil || v <= 0 {
			fatal(context.Background(), "invalid instances value %q", p)
		}
		if !set[v] {
			set[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

func genLine(width int) string {
	if width < 4 {
		width = 4
	}
	return strings.Repeat("x", width-2)
}

func execAll(db *sql.DB, stmts ...string) error {
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("exec %q: %w", s, err)
		}
	}
	return nil
}

func fatal(ctx context.Context, format string, args ...interface{}) {
	hlog.Error(ctx, format, args...)
	os.Exit(1)
}
