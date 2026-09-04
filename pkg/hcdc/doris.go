package hcdc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/client"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// PartitionInfo 定义建表时的分区配置
type PartitionInfo struct {
	Enable        bool   // 是否启用分区
	FieldName     string // 分区字段名，例如 "create_time" 或 "dt"
	Type          string // 分区类型，目前主流为 "RANGE"
	PreCreateDays int    // 预建多少天的分区（例如传 7，则自动建好未来 7 天每天的分区）
}

type DorisResponse struct {
	TxnId                  int    `json:"TxnId"`
	Label                  string `json:"Label"`
	Comment                string `json:"Comment"`
	TwoPhaseCommit         string `json:"TwoPhaseCommit"`
	Status                 string `json:"Status"`
	Message                string `json:"Message"`
	NumberTotalRows        int    `json:"NumberTotalRows"`
	NumberLoadedRows       int    `json:"NumberLoadedRows"`
	NumberFilteredRows     int    `json:"NumberFilteredRows"`
	NumberUnselectedRows   int    `json:"NumberUnselectedRows"`
	LoadBytes              int    `json:"LoadBytes"`
	LoadTimeMs             int    `json:"LoadTimeMs"`
	BeginTxnTimeMs         int    `json:"BeginTxnTimeMs"`
	StreamLoadPutTimeMs    int    `json:"StreamLoadPutTimeMs"`
	ReadDataTimeMs         int    `json:"ReadDataTimeMs"`
	WriteDataTimeMs        int    `json:"WriteDataTimeMs"`
	ReceiveDataTimeMs      int    `json:"ReceiveDataTimeMs"`
	CommitAndPublishTimeMs int    `json:"CommitAndPublishTimeMs"`
}

type DorisConfig struct {
	Host     string `yaml:"host"`     // 数据库主机地址
	Username string `yaml:"username"` // 数据库用户名
	Password string `yaml:"password"` // 数据库密码
	Schema   string `yaml:"schema"`   // 数据库名称
	LogDir   string `yaml:"logDir"`
	LoadURL  string `yaml:"loadURL"`
}

type Doris struct {
	cfg     *DorisConfig
	workers map[SchemaTable]*Worker
	client  *http.Client
	mu      sync.RWMutex
	conn    *client.Conn
}

func NewDoris(cfg *DorisConfig) *Doris {
	return &Doris{
		cfg:     cfg,
		workers: make(map[SchemaTable]*Worker),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (d *Doris) RegisterWorker(schema Schema, table Table) *Worker {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := SchemaTable(string(schema) + "." + string(table))
	w := NewWorker(schema, table, d.cfg.LogDir)
	d.workers[key] = w
	return w
}

func (d *Doris) Open(ctx context.Context) error {
	d.mu.RLock()
	for _, worker := range d.workers {
		go worker.loop(ctx)
	}
	d.mu.RUnlock()

	// 建立直连，用于执行查询和锁表操作
	var err error
	d.conn, err = client.Connect(
		d.cfg.Host,
		d.cfg.Username,
		d.cfg.Password,
		d.cfg.Schema,
	)
	if err != nil {
		return herror.Wrap(err)
	}
	go d.loop(ctx)

	return nil
}

func (d *Doris) AddData(ctx context.Context, schema Schema, table Table, currentCols []Column, values [][]RawBytes) error {
	d.mu.RLock()
	val, ok := d.workers[SchemaTable(string(schema)+"."+string(table))]
	d.mu.RUnlock()
	if !ok {
		return nil
	}
	return val.AddData(ctx, currentCols, values)
}

func (d *Doris) StreamSave(ctx context.Context, schema Schema, table Table, columns string, value io.Reader) error {
	parse, err := url.Parse(d.cfg.LoadURL)
	if err != nil {
		return err
	}
	parse.Path = fmt.Sprintf("/api/%s/%s/_stream_load", schema, table)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, parse.String(), value)
	if err != nil {
		return herror.Wrap(err)
	}

	req.SetBasicAuth(d.cfg.Username, d.cfg.Password)
	req.Header.Set("Expect", "100-continue")
	req.Header.Set("column_separator", ",")

	// ✨ 核心映射：动态把从文件首行解析出来的 columns 传给 Doris
	req.Header.Set("columns", columns)
	req.Header.Set("merge_type", "MERGE")
	req.Header.Set("delete", "__op=2")

	resp, err := d.client.Do(req)
	if err != nil {
		return herror.Wrap(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return herror.NewError("doris stream load failed: " + resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return herror.Wrap(err)
	}

	var dorisResp DorisResponse
	if err = json.Unmarshal(body, &dorisResp); err != nil {
		return herror.Wrap(err)
	}
	if dorisResp.Status != "Success" {
		return herror.NewError("doris stream load failed: " + string(body))
	}
	return nil
}

func (d *Doris) loop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.mu.RLock()
			for _, worker := range d.workers {
				err := worker.readFile(ctx, func(ctx context.Context, columns string, reader io.Reader) error {
					return d.StreamSave(ctx, worker.schema, worker.table, columns, reader)
				})
				if err != nil {
					herror.PrintStack(ctx, err)
				}
			}
			d.mu.RUnlock()
		}
	}
}

// Close 关闭 Doris 连接
func (d *Doris) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func (d *Doris) CreateSchema(ctx context.Context, schema Schema) error {
	var s strings.Builder
	s.WriteString("CREATE DATABASE IF NOT EXISTS ")
	s.WriteString(string(schema))

	hlog.Info(ctx, "doris create schema: %s", s.String())
	result, err := d.conn.Execute(s.String())
	if err != nil {
		return err
	}
	defer result.Close()
	return nil
}

// GetReplicationNum 动态探测并计算最安全的副本数
func (d *Doris) GetReplicationNum(ctx context.Context) int {
	// 1. 默认降级策略为 1
	defaultNum := 1

	// 2. 查出健康的 BE (Backend) 节点数量
	res, err := d.conn.Execute("SHOW BACKENDS")
	if err != nil {
		return defaultNum
	}
	defer res.Close()

	aliveCount := 0
	for range res.Values {
		// 假设在你的驱动中，Alive 状态列是可解析的字符串。
		// 或者是根据行数：通常一行代表一个 BE 节点
		aliveCount++
	}

	// 3. 查出 FE 全局默认的副本设置
	feRes, err := d.conn.Execute("ADMIN SHOW FRONTEND CONFIG LIKE '%default_replication_num%'")
	if err == nil && len(feRes.Values) > 0 {
		// 假设第 2 列是配置的值（依据不同版本，通常格式为 Key, Value）
		if len(feRes.Values[0]) >= 2 {
			valStr := feRes.Values[0][1].String()
			if num, err := strconv.Atoi(valStr); err == nil {
				defaultNum = num
			}
		}
		feRes.Close()
	}

	// 4. 终极防御：副本数绝对不能大于当前存活的 BE 节点总数，否则 Doris 建表会报错
	if aliveCount > 0 && defaultNum > aliveCount {
		return aliveCount
	}
	if defaultNum < 1 {
		return 1
	}
	return defaultNum
}

// CreateTable 增强版：支持动态主键、自适应副本及 Range 时间分区
func (d *Doris) CreateTable(ctx context.Context, schema Schema, table Table, columns []ColumnInfo, partInfo PartitionInfo) error {
	var s strings.Builder

	// 1. 收集并确定主键 (Unique Keys)
	var keyCols []string
	hasPartitionFieldInKey := false

	for _, col := range columns {
		if col.IsKey {
			keyCols = append(keyCols, fmt.Sprintf("`%s`", col.Name))
			if partInfo.Enable && col.Name == partInfo.FieldName {
				hasPartitionFieldInKey = true
			}
		}
	}

	// 兜底策略：如果没有任何主键，默认拿第一列
	if len(keyCols) == 0 && len(columns) > 0 {
		keyCols = append(keyCols, fmt.Sprintf("`%s`", columns[0].Name))
		if partInfo.Enable && columns[0].Name == partInfo.FieldName {
			hasPartitionFieldInKey = true
		}
	}

	// 🚨 核心逻辑规避：Doris 规定 Unique 模型的分区列必须包含在 Unique Key 中
	if partInfo.Enable && !hasPartitionFieldInKey {
		keyCols = append(keyCols, fmt.Sprintf("`%s`", partInfo.FieldName))
		// 同时需要更新 columns 列表中该字段的 IsKey 属性，确保排序正确
		for i := range columns {
			if columns[i].Name == partInfo.FieldName {
				columns[i].IsKey = true
			}
		}
	}

	// 2. 拼接字段定义
	s.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s`.`%s` (\n", string(schema), string(table)))
	for i, col := range columns {
		s.WriteString(fmt.Sprintf("  `%s` %s COMMENT '%s'", col.Name, d.ToDorisType(col.Type), strings.ReplaceAll(col.Comment, "'", "\\'")))
		if i < len(columns)-1 {
			s.WriteString(",\n")
		}
	}
	s.WriteString("\n) ENGINE=OLAP\n")

	// 3. 拼接 UNIQUE KEY (确保聚合/唯一键列排在前面)
	keysStr := strings.Join(keyCols, ", ")
	s.WriteString(fmt.Sprintf("UNIQUE KEY(%s)\n", keysStr))

	// 4. 动态拼接 PARTITION BY RANGE 逻辑
	if partInfo.Enable && partInfo.Type == "RANGE" {
		s.WriteString(fmt.Sprintf("PARTITION BY RANGE(`%s`) (\n", partInfo.FieldName))

		// 自动预建分区（以天为单位生成：从昨天开始，一直生成到未来 N 天）
		now := time.Now()
		daysToCreate := 7 // 默认预建一周
		if partInfo.PreCreateDays > 0 {
			daysToCreate = partInfo.PreCreateDays
		}

		var partLines []string
		// 从昨天开始建，防止边界数据由于时区或延迟无法写入
		for i := -1; i <= daysToCreate; i++ {
			t := now.AddDate(0, 0, i)
			pName := fmt.Sprintf("p%s", t.Format("20060102"))
			pLower := t.Format("2006-01-02")
			pUpper := t.AddDate(0, 0, 1).Format("2006-01-02")

			// 转换为 Doris 语法：PARTITION p220260904 VALUES [('2026-09-04'), ('2026-09-05'))
			line := fmt.Sprintf("  PARTITION %s VALUES [('%s 00:00:00'), ('%s 00:00:00'))", pName, pLower, pUpper)
			partLines = append(partLines, line)
		}
		s.WriteString(strings.Join(partLines, ",\n"))
		s.WriteString("\n)\n")
	}

	// 5. 拼接 DISTRIBUTED BY
	// 分布式 Hash 键建议直接选择主键
	s.WriteString(fmt.Sprintf("DISTRIBUTED BY HASH(%s) BUCKETS 8\n", keysStr))

	// 6. 动态获取系统推荐的副本数并组装 PROPERTIES
	repNum := d.GetReplicationNum(ctx)
	s.WriteString("PROPERTIES (\n")
	s.WriteString(fmt.Sprintf("  \"replication_num\" = \"%d\",\n", repNum))
	s.WriteString("  \"enable_unique_key_merge_on_write\" = \"true\"\n")
	s.WriteString(");")

	// 7. 执行 SQL
	hlog.Info(ctx, "doris create table: %s", s.String())
	result, err := d.conn.Execute(s.String())
	if err != nil {
		return fmt.Errorf("failed to execute partitioning DDL: %w. SQL: %s", err, s.String())
	}
	defer result.Close()

	return nil
}

// ToDorisType 将上游原始类型转换为合法的 Doris 字段类型
func (d *Doris) ToDorisType(rawType string) string {
	// 1. 统一转换为小写，去掉首尾空格
	rt := strings.TrimSpace(strings.ToLower(rawType))
	if rt == "" {
		return "VARCHAR(255)"
	}

	// 2. 提取括号中的参数（例如 "decimal(10,2)" -> "(10,2)", "bigint" -> ""）
	var args string
	if idx := strings.Index(rt, "("); idx != -1 {
		if endIdx := strings.Index(rt, ")"); endIdx != -1 && endIdx > idx {
			args = rt[idx : endIdx+1]
		}
		// 截断 rt，只保留核心类型名称（例如 "decimal(10,2)" -> "decimal"）
		rt = strings.TrimSpace(rt[:idx])
	} else {
		// 如果没有括号，剔除可能存在的修饰符（例如 "bigint unsigned" -> "bigint"）
		if spaceIdx := strings.Index(rt, " "); spaceIdx != -1 {
			rt = rt[:spaceIdx]
		}
	}

	// 3. 核心类型映射匹配
	switch rt {
	// --- 字符串与文本类型 ---
	case "varchar":
		return "VARCHAR" + args
	case "char":
		// Doris 中建议优先使用 VARCHAR，CHAR 适用于固定长度短字符串
		return "VARCHAR" + args
	case "text", "string", "longtext", "mediumtext", "tinytext":
		return "STRING"
	case "binary", "varbinary", "blob", "longblob":
		// Doris 没有原生 BLOB，通常用 STRING 存放 Base64 或使用 VARIANT
		return "VARIANT"

	// --- 整数类型 ---
	case "tinyint":
		return "TINYINT"
	case "smallint":
		return "SMALLINT"
	case "int", "integer", "mediumint":
		return "INT"
	case "bigint":
		return "BIGINT"
	case "largeint":
		return "LARGEINT"

	// --- 浮点与高精度定点数类型 ---
	case "float":
		return "FLOAT"
	case "double", "real":
		return "DOUBLE"
	case "decimal", "numeric":
		// 强烈推荐全面拥抱第三代高精度定点数 DECIMALV3
		if args == "" {
			return "DECIMALV3(9, 0)" // Doris DECIMALV3 默认精度
		}
		return "DECIMALV3" + args

	// --- 日期与时间类型 ---
	case "date":
		// 推荐使用性能更好的 DATEV2
		return "DATEV2"
	case "datetime", "timestamp":
		// 推荐使用支持微秒精度的 DATETIMEV2
		if args == "" {
			return "DATETIMEV2(0)" // 默认到秒
		}
		return "DATETIMEV2" + args
	case "time":
		return "TIME"

	// --- 半结构化与复杂类型 ---
	case "json", "jsonb":
		return "JSON"
	case "variant":
		return "VARIANT"
	case "array":
		return "ARRAY" + args
	case "map":
		return "MAP" + args
	case "struct":
		return "STRUCT" + args

	// --- 网络与特殊高级类型 ---
	case "ipv4":
		return "IPV4"
	case "ipv6":
		return "IPV6"
	case "bitmap":
		return "BITMAP"
	case "hll":
		return "HLL"
	case "quantile_state":
		return "QUANTILE_STATE"
	case "agg_state":
		return "AGG_STATE"

	// --- 布尔类型 ---
	case "boolean", "bool":
		return "BOOLEAN"

	// --- 兜底策略 ---
	default:
		return "VARCHAR(255)"
	}
}
