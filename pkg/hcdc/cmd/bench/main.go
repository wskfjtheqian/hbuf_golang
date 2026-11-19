// 命令 bench 是 hcdc 的性能基准测试入口：用真实的 MySQL(源) 和 Doris(目标) 量化
// 三个关键吞吐指标——Stream Load 单文件导入、全量拉取、binlog 增量 DML。
//
// 用法：
//
//	go run ./pkg/hcdc/cmd/bench -mysql 127.0.0.1:3316 -doris 127.0.0.1:9030 \
//	  -doris-load http://127.0.0.1:8040/ -rows 200000 -batch 5000
package main

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
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

	rows      = flag.Int("rows", 200000, "全量/增量测试的总行数")
	batch     = flag.Int("batch", 5000, "每批 INSERT 的行数（控制事务大小）")
	rowWidth  = flag.Int("row-width", 120, "每行约字节数（控制行大小）")
	concur    = flag.Int("concur", 1, "Stream Load 并发数（测并发上限用）")
	benchOnly = flag.String("bench", "all", "仅跑哪个场景: streamload | full | incr | all")
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

	hlog.Info(ctx, "=== hcdc 性能基准测试 ===")
	hlog.Info(ctx, "MySQL=%s  Doris=%s  load=%s  rows=%d batch=%d width=%d",
		*mysqlHost, *dorisHost, *dorisLoad, *rows, *batch, *rowWidth)

	// 建专用源库，避免污染业务库。
	schema := fmt.Sprintf("bench_%d", time.Now().UnixNano())
	mysqlDSN := fmt.Sprintf("%s:%s@tcp(%s)/?parseTime=true&timeout=10s&readTimeout=30s&writeTimeout=30s",
		*mysqlUser, *mysqlPass, *mysqlHost)
	md, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		fatal(ctx, "open mysql failed: %v", err)
	}
	defer md.Close()

	if err := execAll(md,
		"CREATE DATABASE IF NOT EXISTS "+schema,
		"USE "+schema,
		"CREATE TABLE t (id BIGINT NOT NULL, name VARCHAR(255), age INT, ts DATETIME, payload VARCHAR(512), PRIMARY KEY(id)) ENGINE=InnoDB",
	); err != nil {
		fatal(ctx, "prepare source failed: %v", err)
	}
	hlog.Info(ctx, "源库准备完成: %s", schema)

	// Doris 目标连接（供清理/校验）。
	dorisDSN := fmt.Sprintf("%s:%s@tcp(%s)/", *dorisUser, *dorisPass, *dorisHost)
	dd, err := sql.Open("mysql", dorisDSN)
	if err != nil {
		fatal(ctx, "open doris failed: %v", err)
	}
	defer dd.Close()

	runBenchmark(ctx, md, dd, schema)
}

func runBenchmark(ctx context.Context, md, dd *sql.DB, schema string) {
	switch *benchOnly {
	case "streamload":
		benchStreamLoad(ctx, schema)
	case "full":
		benchFull(ctx, md, schema)
	case "incr":
		benchIncr(ctx, md, schema)
	default:
		benchStreamLoad(ctx, schema)
		fmt.Println()
		benchFull(ctx, md, schema)
		fmt.Println()
		benchIncr(ctx, md, schema)
	}
}

// benchStreamLoad 直接测 Doris 端单文件导入吞吐，隔离出「上传」这一环的瓶颈。
// 用不同并发数重复导入同一批 CSV，观察吞吐随并发的变化（单表并发上限）。
func benchStreamLoad(ctx context.Context, schema string) {
	// 先在 Doris 建库建表（StreamLoad 要求目标已存在）。
	dorisDSN := fmt.Sprintf("%s:%s@tcp(%s)/", *dorisUser, *dorisPass, *dorisHost)
	dd, err := sql.Open("mysql", dorisDSN)
	if err != nil {
		fatal(ctx, "open doris failed: %v", err)
	}
	defer dd.Close()
	if err := execAll(dd,
		"CREATE DATABASE IF NOT EXISTS "+schema,
		"CREATE TABLE IF NOT EXISTS "+schema+".t (id BIGINT, name VARCHAR(255), age INT, ts DATETIME, payload VARCHAR(512)) UNIQUE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 8 PROPERTIES(\"replication_num\"=\"1\")",
	); err != nil {
		fatal(ctx, "prepare doris failed: %v", err)
	}

	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     *dorisHost,
		Username: *dorisUser,
		Password: *dorisPass,
		LoadURL:  *dorisLoad,
	})

	cols := "`id`,`name`,`age`,`ts`,`payload`,`__op`"
	line := genLine(*rowWidth)
	var buf bytes.Buffer
	for i := 0; i < *rows; i++ {
		buf.WriteString(fmt.Sprintf("%d,%s,%d,%s,%s,0\n", i, "name_"+strconv.Itoa(i), i%100, "2026-01-01 00:00:00", line))
	}
	payload := buf.Bytes()
	totalBytes := int64(len(payload))

	hlog.Info(ctx, "--- 场景 1: Stream Load 单文件导入吞吐 ---")
	hlog.Info(ctx, "数据量: %d 行, %.2f MB", *rows, float64(totalBytes)/1024/1024)

	for _, c := range []int{1, 2, 4, 8} {
		if c > *concur && *concur > 0 {
			continue
		}
		elapsed, err := streamLoadBatch(ctx, d, schema, "t", cols, payload, c)
		if err != nil {
			hlog.Error(ctx, "streamload concur=%d 失败: %v", c, err)
			continue
		}
		rps := float64(*rows) / elapsed.Seconds()
		mbps := float64(totalBytes) / elapsed.Seconds() / 1024 / 1024
		hlog.Info(ctx, "  并发=%d: %.2fs, 吞吐=%.0f 行/s, %.2f MB/s", c, elapsed.Seconds(), rps, mbps)
	}
}

func streamLoadBatch(ctx context.Context, d *hcdc.Doris, schema, table, cols string, payload []byte, concur int) (time.Duration, error) {
	start := time.Now()
	if concur <= 1 {
		if err := d.StreamSave(ctx, hcdc.Schema(schema), hcdc.Table(table), cols, bytes.NewReader(payload)); err != nil {
			return 0, err
		}
		return time.Since(start), nil
	}
	// 并发导入：把 payload 按行均分成 concur 段（每段保留 header），并发 StreamLoad 到同一张表。
	// 用于观测 Doris 端单表并发导入的吞吐上限。
	segs, err := splitPayload(payload, concur)
	if err != nil {
		return 0, err
	}
	ch := make(chan error, concur)
	for i := 0; i < concur; i++ {
		go func(p []byte) {
			ch <- d.StreamSave(ctx, hcdc.Schema(schema), hcdc.Table(table), cols, bytes.NewReader(p))
		}(segs[i])
	}
	for i := 0; i < concur; i++ {
		if err := <-ch; err != nil {
			return 0, err
		}
	}
	return time.Since(start), nil
}

// splitPayload 把「N 行数据」的 CSV（不含 header）按行均分成 n 段（纯数据，不带 header）。
func splitPayload(payload []byte, n int) ([][]byte, error) {
	lines := bytes.Count(payload, []byte{'\n'})
	per := lines / n
	if per < 1 {
		return nil, fmt.Errorf("too few lines to split")
	}

	// 先把 payload 按行切出所有行的 offset 边界。
	type seg struct{ s, e int }
	boundaries := make([]seg, 0, lines)
	s := 0
	for i := 0; i < lines; i++ {
		idx := bytes.IndexByte(payload[s:], '\n')
		if idx < 0 {
			break
		}
		boundaries = append(boundaries, seg{s, s + idx + 1})
		s += idx + 1
	}

	segs := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		lineStart := i * per
		lineEnd := (i + 1) * per
		if i == n-1 {
			lineEnd = lines
		}
		b := boundaries[lineStart].s
		e := boundaries[lineEnd-1].e
		segs = append(segs, payload[b:e])
	}
	return segs, nil
}

// benchFull 测 hcdc 全量拉取（SELECT 全量 → CSV → Stream Load）端到端吞吐。
func benchFull(ctx context.Context, md *sql.DB, schema string) {
	hlog.Info(ctx, "--- 场景 2: 全量拉取吞吐（MySQL -> Doris）---")
	// 先造数据。
	line := genLine(*rowWidth)
	t0 := time.Now()
	for off := 0; off < *rows; off += *batch {
		var sb strings.Builder
		sb.WriteString("INSERT INTO t (id,name,age,ts,payload) VALUES ")
		end := off + *batch
		if end > *rows {
			end = *rows
		}
		for i := off; i < end; i++ {
			if i > off {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf("(%d,'name_%d',%d,'2026-01-01 00:00:00','%s')", i, i, i%100, line))
		}
		if _, err := md.Exec(sb.String()); err != nil {
			fatal(ctx, "insert data failed: %v", err)
		}
	}
	insertElapsed := time.Since(t0)
	hlog.Info(ctx, "源表插入 %d 行耗时 %.2fs (%.0f 行/s)", *rows, insertElapsed.Seconds(), float64(*rows)/insertElapsed.Seconds())

	serverID := uint32(time.Now().UnixNano() & 0x7fffffff)
	cfg := &hcdc.Config{
		Canals: []hcdc.CanalConfig{{
			InstanceId: hcdc.InstanceId(time.Now().UnixNano() & 0x7fffffff),
			Host:       *mysqlHost,
			Username:   *mysqlUser,
			Password:   *mysqlPass,
			Schema:     schema,
			ServerID:   &serverID,
			Charset:    "utf8mb4",
			Flavor:     "mariadb",
			LogDir:     "./logs_bench_full",
		}},
		Doris: &hcdc.DorisConfig{
			Host:     *dorisHost,
			Username: *dorisUser,
			Password: *dorisPass,
			LoadURL:  *dorisLoad,
		},
	}
	// 只同步本测试库，避免全量扫描同步到 dump 等其他库（dump 是 Doris 保留字会报错）。
	cfg.Canals[0].IncludeDBs = []string{"^" + schema + "$"}

	cdc := hcdc.NewHCDC()
	t1 := time.Now()
	if err := cdc.SetConfig(ctx, cfg); err != nil {
		fatal(ctx, "SetConfig failed: %v", err)
	}
	// 全量数据同步写入 CSV 后，flush(5s) + scan(2s周期) + 上传为异步，且大表会被 rotateSize 切成
	// 多个文件分多轮上传。这里固定等待足够时长，读最终累计行数计算吞吐。
	time.Sleep(20 * time.Second)
	elapsed := time.Since(t1)
	m := cdc.GetMetrics()
	rps := float64(m.UploadRows) / elapsed.Seconds()
	mbps := float64(m.UploadBytes) / elapsed.Seconds() / 1024 / 1024
	hlog.Info(ctx, "全量同步耗时 %.2fs | 上传 %d 行 %.2fMB | 吞吐=%.0f 行/s, %.2f MB/s",
		elapsed.Seconds(), m.UploadRows, float64(m.UploadBytes)/1024/1024, rps, mbps)
	_ = cdc.SetConfig(ctx, nil)
}

// benchIncr 测 binlog 增量 DML 吞吐。
func benchIncr(ctx context.Context, md *sql.DB, schema string) {
	hlog.Info(ctx, "--- 场景 3: binlog 增量 DML 吞吐 ---")
	// 先全量建立基线，再测增量。
	line := genLine(*rowWidth)

	// 预插一小批全量基线数据（正 id），让全量拉取走完整链路、位点正确推进。
	const baseRows = 1000
	t0 := time.Now()
	for off := 0; off < baseRows; off += *batch {
		var sb strings.Builder
		sb.WriteString("INSERT INTO t (id,name,age,ts,payload) VALUES ")
		end := off + *batch
		if end > baseRows {
			end = baseRows
		}
		for i := off; i < end; i++ {
			if i > off {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf("(%d,'name_%d',%d,'2026-01-01 00:00:00','%s')", i, i, i%100, line))
		}
		if _, err := md.Exec(sb.String()); err != nil {
			fatal(ctx, "base insert failed: %v", err)
		}
	}

	serverID := uint32(time.Now().UnixNano() & 0x7fffffff)
	cfg := &hcdc.Config{
		Canals: []hcdc.CanalConfig{{
			InstanceId: hcdc.InstanceId(time.Now().UnixNano() & 0x7fffffff),
			Host:       *mysqlHost,
			Username:   *mysqlUser,
			Password:   *mysqlPass,
			Schema:     schema,
			ServerID:   &serverID,
			Charset:    "utf8mb4",
			Flavor:     "mariadb",
			LogDir:     fmt.Sprintf("./logs_bench_incr_%d", time.Now().UnixNano()),
		}},
		Doris: &hcdc.DorisConfig{
			Host:     *dorisHost,
			Username: *dorisUser,
			Password: *dorisPass,
			LoadURL:  *dorisLoad,
		},
	}
	cfg.Canals[0].IncludeDBs = []string{"^" + schema + "$"}
	// 无需额外 IncludeTables：bench_xxx 库是本次专用库，IncludeDBs 已限定范围。

	cdc := hcdc.NewHCDC()
	if err := cdc.SetConfig(ctx, cfg); err != nil {
		fatal(ctx, "SetConfig failed: %v", err)
	}
	// 全量数据 flush 成 .active 需 5s(flushDuration) + scan 2s + 上传，固定等待覆盖即可。
	time.Sleep(10 * time.Second)

	// 记录基线，然后做增量 INSERT（用大正 id 区间，避免与基线 id 冲突）。
	base := cdc.GetMetrics()
	hlog.Info(ctx, "基线: 已上传 %d 行，开始增量写入 %d 行...", base.UploadRows, *rows)

	const idBase = 1000000 // 增量 id 起始，避开基线 0~baseRows。
	for off := 0; off < *rows; off += *batch {
		var sb strings.Builder
		sb.WriteString("INSERT INTO t (id,name,age,ts,payload) VALUES ")
		end := off + *batch
		if end > *rows {
			end = *rows
		}
		for i := off; i < end; i++ {
			if i > off {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf("(%d,'incr_%d',%d,'2026-01-01 00:00:00','%s')", idBase+i, i, i%100, line))
		}
		if _, err := md.Exec(sb.String()); err != nil {
			fatal(ctx, "incr insert failed: %v", err)
		}
	}
	writeElapsed := time.Since(t0)
	hlog.Info(ctx, "源库增量写入 %d 行耗时 %.2fs (%.0f 行/s)", *rows, writeElapsed.Seconds(), float64(*rows)/writeElapsed.Seconds())

	// 等待同步追上：上传行数 >= 基线 + 增量行数。
	deadline := time.Now().Add(2 * time.Minute)
	target := base.UploadRows + int64(*rows)
	for {
		m := cdc.GetMetrics()
		if m.UploadRows >= target {
			break
		}
		if time.Now().After(deadline) {
			hlog.Warn(ctx, "等待同步超时，当前 %d / 目标 %d", m.UploadRows, target)
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	elapsed := time.Since(t0)
	m := cdc.GetMetrics()
	synced := m.UploadRows - base.UploadRows
	rps := float64(synced) / elapsed.Seconds()
	hlog.Info(ctx, "增量同步: 写入+同步总耗时 %.2fs, 实际同步 %d 行, 端到端吞吐=%.0f 行/s",
		elapsed.Seconds(), synced, rps)
	_ = cdc.SetConfig(ctx, nil)
}

func waitDrained(ctx context.Context, cdc *hcdc.HCDC) {
	deadline := time.Now().Add(3 * time.Minute)
	for {
		m := cdc.GetMetrics()
		if m.PendingFiles == 0 && m.PendingBytes == 0 {
			return
		}
		if time.Now().After(deadline) {
			hlog.Warn(ctx, "等待上传清空超时，剩余积压 files=%d bytes=%d", m.PendingFiles, m.PendingBytes)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitSettled 等待同步链路稳定：积压清空且 UploadRows 连续多周期不再增长。
// maxWait 为最长等待上限，避免异常时永久阻塞。
func waitSettled(ctx context.Context, cdc *hcdc.HCDC, maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	stable := 0
	var last int64 = -1
	for {
		m := cdc.GetMetrics()
		if m.UploadRows == last {
			stable++
		} else {
			last = m.UploadRows
			stable = 0
		}
		if m.PendingFiles == 0 && m.PendingBytes == 0 && stable >= 3 {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitRows 等待上传完成：先等到开始有上传（UploadRows>0），再等积压清空且行数稳定。
func waitRows(ctx context.Context, cdc *hcdc.HCDC, target int64) {
	deadline := time.Now().Add(3 * time.Minute)
	started := false
	stable := 0
	var last int64 = -1
	for {
		m := cdc.GetMetrics()
		if m.UploadRows > 0 {
			started = true
		}
		if m.UploadRows == last {
			stable++
		} else {
			last = m.UploadRows
			stable = 0
		}
		// 已开始上传，且积压清空且行数连续稳定，判定完成。
		if started && m.PendingFiles == 0 && m.PendingBytes == 0 && stable >= 3 {
			return
		}
		if time.Now().After(deadline) {
			hlog.Warn(ctx, "等待上传超时，当前 %d 行，积压 files=%d bytes=%d", m.UploadRows, m.PendingFiles, m.PendingBytes)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func genLine(width int) string {
	if width < 4 {
		width = 4
	}
	// 生成一个不含逗号/换行的填充串，模拟中等宽度字段。
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
