package hcdc

import (
	"context"
	"database/sql"
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
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

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

func (c *DorisConfig) Validate(ctx context.Context) bool {
	var valid bool = true
	return valid
}

func (c *DorisConfig) Equal(other *DorisConfig) bool {
	return c.Host == other.Host &&
		c.Username == other.Username &&
		c.Password == other.Password &&
		c.Schema == other.Schema &&
		c.LogDir == other.LogDir &&
		c.LoadURL == other.LoadURL
}

type ActionInfo struct {
}

type Doris struct {
	cfg     *DorisConfig
	workers map[SchemaTable]*Worker
	client  *http.Client
	mu      sync.RWMutex
	conn    *client.Conn
	sql     *sql.DB
	change  chan []TableInfo
}

func NewDoris(cfg *DorisConfig) *Doris {
	return &Doris{
		cfg:     cfg,
		workers: make(map[SchemaTable]*Worker),
		client:  &http.Client{Timeout: 60 * time.Second},
		change:  make(chan []TableInfo),
	}
}

func (d *Doris) RegisterWorker(ctx context.Context, schema Schema, table Table) *Worker {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := SchemaTable(string(schema) + "." + string(table))
	w := NewWorker(schema, table, d.cfg.LogDir)
	d.workers[key] = w

	go w.loop(ctx)
	return w
}

func (d *Doris) Open(ctx context.Context) error {
	d.mu.RLock()
	for _, worker := range d.workers {
		go worker.loop(ctx)
	}
	d.mu.RUnlock()

	//// 建立直连，用于执行查询和锁表操作
	var err error
	//d.conn, err = client.Connect(
	//	d.cfg.Host,
	//	d.cfg.Username,
	//	d.cfg.Password,
	//	d.cfg.Schema,
	//)
	//if err != nil {
	//	return herror.Wrap(err)
	//}
	//
	d.sql, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s)/%s", d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Schema))
	if err != nil {
		return herror.Wrap(err)
	}
	go d.loop(ctx)

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
				err := worker.ScanFile(ctx, func(ctx context.Context, infos []ColumnInfo, columns string, reader io.Reader) error {
					return d.StreamSave(ctx, worker.schema, worker.table, infos, columns, reader)
				})
				if err != nil {
					herror.PrintStack(ctx, err)
				}
			}
			d.mu.RUnlock()
		case val := <-d.change:
			for _, info := range val {
				err := d.changeTable(ctx, &info)
				if err != nil {
					herror.PrintStack(ctx, err)
				}
			}
		}
	}
}
func (d *Doris) ChangeTables(ctx context.Context, info []TableInfo) {
	d.change <- info
}
func (d *Doris) AddData(ctx context.Context, schema Schema, table Table, action Action, currentCols []ColumnInfo, values [][]RawBytes) error {
	d.mu.RLock()
	val, ok := d.workers[SchemaTable(string(schema)+"."+string(table))]
	d.mu.RUnlock()
	if !ok {
		return nil
	}
	return val.AddData(ctx, action, currentCols, values)
}

func (d *Doris) StreamSave(ctx context.Context, schema Schema, table Table, infos []ColumnInfo, columns string, value io.Reader) error {
	parse, err := url.Parse(d.cfg.LoadURL)
	if err != nil {
		return err
	}
	parse.Path = fmt.Sprintf("/api/%s/%s/_stream_load", schema, table)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, parse.String(), value)
	if err != nil {
		return herror.Wrap(err)
	}

	decoders := hutl.Slice(hutl.Filter(infos, func(info ColumnInfo) bool {
		switch info.Type {
		case "boolean", "bool", "decimal", "numeric", "double", "real", "float", "tinyint", "smallint", "int", "integer", "mediumint", "bigint", "largeint", "time", "date", "datetime", "timestamp":
			return false
		}
		return true
	}), func(i int, v ColumnInfo) string {
		typ := GetColumnType(&v)
		if typ == "bitmap32" || typ == "bitmap64" {
			return "`" + string(v.Name) + "`= bitmap_from_string(from_base64(`" + string(v.Name) + "_base`))"
		}
		return "`" + string(v.Name) + "`= from_base64(`" + string(v.Name) + "_base`)"
	})
	if len(decoders) > 0 {
		columns += "," + strings.Join(decoders, ",")
	}
	hlog.Info(ctx, "doris stream save: %s.%s columns: %s", schema, table, columns)

	req.SetBasicAuth(d.cfg.Username, d.cfg.Password)
	req.Header.Set("Expect", "100-continue")
	req.Header.Set("column_separator", ",")

	// ✨ 核心映射：动态把从文件首行解析出来的 columns 传给 Doris
	req.Header.Set("columns", columns)
	req.Header.Set("merge_type", "MERGE")
	req.Header.Set("delete", "__op=2")
	req.Header.Set("strict_mode", "false")

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

	hlog.Info(ctx, "doris change schema: %s", s.String())
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
func (d *Doris) changeTable(ctx context.Context, info *TableInfo) error {
	newInfo := d.ToDorisTable(info)
	oldInfo, err := d.GetTableInfo(ctx, info.Schema, info.Table)
	if err != nil {
		return err
	}
	if oldInfo == nil {
		return d.createTable(ctx, newInfo)
	}

	return d.modifyTable(ctx, oldInfo, newInfo)
}

// createTable 增强版：支持动态主键、自适应副本及 Range 时间分区
func (d *Doris) createTable(ctx context.Context, info *TableInfo) error {

	var s strings.Builder
	columns := info.Columns

	// 2. 拼接字段定义
	s.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s`.`%s` (\n", string(info.Schema), string(info.Table)))
	for i, col := range columns {
		s.WriteString(" `")
		s.WriteString(string(col.Name))
		s.WriteString("` ")
		s.WriteString(col.Type)
		s.WriteString(col.Args)
		s.WriteString(" ")
		if col.IsNull == "YES" {
			s.WriteString("NULL ")
		} else {
			s.WriteString("NOT NULL ")
		}
		if col.Default != nil {
			s.WriteString("Default ")
			s.WriteString(*col.Default)
			s.WriteString(" ")
		}

		s.WriteString("COMMENT ")
		s.WriteString("'")
		s.WriteString(strings.ReplaceAll(col.Comment, "'", "\\'"))
		s.WriteString("'")

		if i < len(info.Columns)-1 {
			s.WriteString(",\n")
		}
	}
	s.WriteString("\n) ENGINE=OLAP\n")

	// 3. 拼接 UNIQUE KEY (确保聚合/唯一键列排在前面)
	if len(info.Keys) > 0 {
		keysStr := strings.Join(hutl.Slice(info.Keys, func(i int, v Column) string {
			return "`" + string(v) + "`"
		}), ", ")
		s.WriteString(fmt.Sprintf("UNIQUE KEY(%s)\n", keysStr))
		// 5. 拼接 DISTRIBUTED BY
		// 分布式 Hash 键建议直接选择主键
		s.WriteString(fmt.Sprintf("DISTRIBUTED BY HASH(%s) BUCKETS 8\n", keysStr))
	}

	// 4. 动态拼接 PARTITION BY RANGE 逻辑
	if info.PartitionType == "RANGE" {
		if info.PartitionField == "" {
			println("info.PartitionField is empty")
		}
		s.WriteString(fmt.Sprintf("AUTO PARTITION BY RANGE (DATE_TRUNC(`%s`, 'DAY')) ()\n", info.PartitionField))
	}
	// 6. 动态获取系统推荐的副本数并组装 PROPERTIES
	repNum := d.GetReplicationNum(ctx)
	if repNum > 3 {
		repNum = 3
	}
	s.WriteString("PROPERTIES (\n")
	if len(info.Keys) > 0 {
		s.WriteString(fmt.Sprintf("  \"replication_num\" = \"%d\",\n", repNum))
		s.WriteString("  \"enable_unique_key_merge_on_write\" = \"true\"\n")
	} else {
		s.WriteString(fmt.Sprintf("  \"replication_num\" = \"%d\"\n", repNum))
	}
	s.WriteString(");")

	// 7. 执行 SQL
	hlog.Info(ctx, "doris change table: %s", s.String())
	result, err := d.conn.Execute(s.String())
	if err != nil {
		return fmt.Errorf("failed to execute partitioning DDL: %w. SQL: %s", err, s.String())
	}
	defer result.Close()

	return nil
}

// ToDorisType 将上游原始类型转换为合法的 Doris 字段类型
func (d *Doris) ToDorisType(info ColumnInfo) (string, string) {
	// 1. 统一转换为小写，去掉首尾空格
	rt := strings.TrimSpace(GetColumnType(&info))
	if rt == "" {
		return "VARIANT", ""
	}

	var args = info.Args
	// 3. 核心类型映射匹配
	switch rt {
	// --- 字符串与文本类型 ---
	case "varchar":
		return "VARCHAR", args
	case "char":
		// Doris 中建议优先使用 VARCHAR，CHAR 适用于固定长度短字符串
		return "VARCHAR", args
	case "text", "string", "longtext", "mediumtext", "tinytext":
		return "STRING", ""
	case "bitmap64", "bitmap32":
		return "BITMAP", ""
	case "binary", "varbinary", "blob", "longblob", "mediumblob":
		return "VARIANT", ""

	// --- 整数类型 ---
	case "tinyint":
		if args == "" {
			return "TINYINT", "3"
		}
		return "TINYINT", args
	case "smallint":
		if args == "" {
			return "SMALLINT", "5"
		}
		return "SMALLINT", args
	case "int", "integer", "mediumint":
		if args == "" {
			return "INT", "11"
		}
		return "INT", args
	case "bigint":
		if args == "" {
			return "BIGINT", "20"
		}
		return "BIGINT", args
	case "largeint":
		if args == "" {
			return "LARGEINT", "20"
		}
		return "LARGEINT", args

	// --- 浮点与高精度定点数类型 ---
	case "float":
		return "FLOAT", args
	case "double", "real":
		return "DOUBLE", args
	case "decimal", "numeric":
		// 强烈推荐全面拥抱第三代高精度定点数 DECIMALV3
		if args == "" {
			return "DECIMALV3", "(9, 0)" // Doris DECIMALV3 默认精度
		}
		return "DECIMALV3", args

	// --- 日期与时间类型 ---
	case "date":
		// 推荐使用性能更好的 DATEV2
		if args == "" {
			return "DATEV2", ""
		}
		return "DATEV2", args
	case "datetime", "timestamp":
		// 推荐使用支持微秒精度的 DATETIMEV2
		if args == "" {
			return "DATETIMEV2", "" // 默认到秒
		}
		return "DATETIMEV2", args
	case "time":
		if args == "" {
			return "TIME", ""
		}
		return "TIME", args

	// --- 半结构化与复杂类型 ---
	case "json", "jsonb":
		return "JSON", ""
	case "variant":
		return "VARIANT", ""
	case "array":
		return "ARRAY", args
	case "map":
		return "MAP", args
	case "struct":
		return "STRUCT", args

	// --- 网络与特殊高级类型 ---
	case "ipv4":
		return "IPV4", ""
	case "ipv6":
		return "IPV6", ""
	case "bitmap":
		return "BITMAP", ""
	case "hll":
		return "HLL", ""
	case "quantile_state":
		return "QUANTILE_STATE", ""
	case "agg_state":
		return "AGG_STATE", ""

	// --- 布尔类型 ---
	case "boolean", "bool":
		return "BOOLEAN", ""

	// --- 兜底策略 ---
	default:
		return "VARIANT", ""
	}
}

// ToDorisTable 将上游表结构转换为 Doris 兼容的表结构
func (d *Doris) ToDorisTable(info *TableInfo) *TableInfo {
	ret := TableInfo{
		Schema:         info.Schema,
		Table:          info.Table,
		Columns:        make([]ColumnInfo, len(info.Columns)),
		Index:          info.Index,
		Keys:           info.Keys,
		PartitionField: info.PartitionField,
	}
	for i, col := range info.Columns {
		column := &ret.Columns[i]
		column.Type, column.Args = d.ToDorisType(col)
		column.Name = col.Name
		column.Comment = col.Comment
		if column.Type == "BITMAP" {
			column.Comment += "数据已在，只是BITMAP类型的数据在工具不能正常显示"
		}
		column.KeyIndex = col.KeyIndex
		if column.Type == "BITMAP" {
			column.IsNull = "NO"
		} else {
			column.IsNull = col.IsNull
		}

		if col.Default != nil && *col.Default != "NULL" && column.Type != "VARIANT" {
			if column.Type == "DATEV2" || column.Type == "DATETIMEV2" {
				column.Default = hutl.ToPointer(strings.ReplaceAll(*col.Default, "0000-00-00", "1970-01-01"))
			} else {
				column.Default = hutl.ToPointer(*col.Default)
			}
		}
	}

	return &ret
}

// GetTableInfo 获得指定表的结构
func (c *Doris) GetTableInfo(ctx context.Context, schema Schema, table Table) (*TableInfo, error) {
	keys, err := c.GetKeys(ctx, schema, table)
	if err != nil {
		return nil, herror.Wrap(err)
	}

	//query := "SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_COMMENT,IS_NULLABLE, COLUMN_DEFAULT,DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?"
	//result, err := c.conn.Execute(query, string(schema), string(table))
	//if err != nil {
	//	return nil, herror.Wrap(err)
	//}
	//defer result.Close()
	//
	//ret := &TableInfo{
	//	Columns: make([]ColumnInfo, len(result.Values)),
	//	Keys:    make([]Column, 0),
	//}d
	//for i, rows := range result.Values {
	//	ret.Columns[i] = ColumnInfo{
	//		Name:     Column(rows[0].AsString()),
	//		Type:     strings.ToLower(string(rows[5].AsString())),
	//		Comment:  string(rows[2].AsString()),
	//		KeyIndex: keys[string(rows[0].AsString())],
	//		IsNull:   string(rows[3].AsString()) == "YES",
	//		Args:     string(rows[1].AsString()[len(rows[5].AsString()):]),
	//		Default:  string(rows[4].AsString()),
	//	}
	//}

	query, err := c.sql.QueryContext(ctx, "SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_COMMENT,IS_NULLABLE, COLUMN_DEFAULT,DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", string(schema), string(table))
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer query.Close()
	var ret = &TableInfo{
		Schema:  schema,
		Table:   table,
		Columns: make([]ColumnInfo, 0),
		Keys:    make([]Column, 0),
	}
	for query.Next() {
		var column ColumnInfo
		var args string
		if err := query.Scan(&column.Name, &args, &column.Comment, &column.IsNull, &column.Default, &column.Type); err != nil {
			return nil, herror.Wrap(err)
		}
		if args == "string" {
			column.Args = ""
			column.Type = args
		} else {
			column.Args = args[len(column.Type):]
		}
		column.Type = strings.ToUpper(column.Type)
		if column.Type == "DATETIME" {
			column.Type = "DATETIMEV2"
		} else if column.Type == "DATE" {
			column.Type = "DATEV2"
		} else if column.Type == "DECIMAL" {
			column.Type = "DECIMALV3"
			column.Args = args[len(column.Type):]
		}
		if column.Default != nil {
			column.Default = hutl.ToPointer(strings.ToUpper(*column.Default))
		}

		column.Args = strings.ReplaceAll(column.Args, " ", "")
		column.KeyIndex = keys[string(column.Name)]
		ret.Columns = append(ret.Columns, column)
	}
	if err := query.Err(); err != nil {
		return nil, herror.Wrap(err)
	}

	hutl.Sort(ret.Columns, func(i, j ColumnInfo) bool {
		return i.KeyIndex > j.KeyIndex
	})
	ret.Index = hutl.SliceToMap(ret.Columns, func(i int, column ColumnInfo) (Column, int) {
		return column.Name, i
	})

	for _, column := range ret.Columns {
		if column.KeyIndex > 0 {
			ret.Keys = append(ret.Keys, column.Name)
		}
	}

	ret.PartitionType, err = c.GetPartitionType(ctx, schema, table)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	if ret.PartitionType == "RANGE" {
		for _, key := range ret.Keys {
			column := ret.Columns[ret.Index[key]]
			if strings.HasPrefix(column.Type, "datetime") || strings.HasPrefix(column.Type, "timestamp") {
				ret.PartitionField = column.Name
				break
			}
		}
		if ret.PartitionField == "" {
			println("table", table, "ret.PartitionField", ret.PartitionField)
		}
	}

	return ret, nil
}

// GetPartitionType 动态查询 MySQL 上游表的 RANGE 分区字段
func (c *Doris) GetPartitionType(ctx context.Context, schema Schema, table Table) (string, error) {
	// 查询该表是否拥有 RANGE 类型的分区，并找出分区键 (COLUMN_NAME)
	query := `SELECT PARTITION_METHOD FROM INFORMATION_SCHEMA.PARTITIONS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? LIMIT 1;`

	//result, err := c.conn.Execute(query, string(schema), string(table))
	//if err != nil {
	//	return "", err
	//}
	//defer result.Close()
	//
	//// 如果没有查询到分区记录，说明是普通表，不启用分区
	//if len(result.Values) == 0 {
	//	return "", nil
	//}
	//ret := string(result.Values[0][0].AsString())
	//println("result", ret)

	////////////////////////////////////////////
	rows, err := c.sql.QueryContext(ctx, query, string(schema), string(table))
	if err != nil {
		return "", herror.Wrap(err)
	}
	defer rows.Close()
	for rows.Next() {
		var partitionMethod *string
		err := rows.Scan(&partitionMethod)
		if err != nil {
			return "", herror.Wrap(err)
		}
		if partitionMethod != nil {
			return *partitionMethod, nil
		}
	}
	return "", nil
}

// GetKeys 获得指定表的主键
func (c *Doris) GetKeys(ctx context.Context, schema Schema, table Table) (map[string]int, error) {
	//query := "SELECT COLUMN_NAME,CONSTRAINT_NAME FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION DESC "
	//result, err := c.conn.Execute(query, string(schema), string(table))
	//if err != nil {
	//	return nil, herror.Wrap(err)
	//}
	//defer result.Close()
	//var keys = make(map[string]int)
	//for i, rows := range result.Values {
	//	if string(rows[1].AsString()) == "PRIMARY" {
	//		keys[string(rows[0].AsString())] = i + 1
	//	}
	//}
	query, err := c.sql.QueryContext(ctx, "SELECT COLUMN_NAME,CONSTRAINT_NAME FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION DESC ", string(schema), string(table))
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer query.Close()

	var keys = make(map[string]int)
	for query.Next() {
		var column, constraint *string
		if err := query.Scan(&column, &constraint); err != nil {
			return nil, herror.Wrap(err)
		}
		if constraint == nil || column == nil {
			continue
		}
		if *constraint == "PRIMARY" {
			keys[*column] = 1
		}
	}
	if err := query.Err(); err != nil {
		return nil, herror.Wrap(err)
	}
	return keys, nil
}

func (c *Doris) modifyTable(ctx context.Context, oldInfo *TableInfo, newInfo *TableInfo) error {
	addList := make([]ColumnInfo, 0, len(newInfo.Columns))
	modifyList := make([]ColumnInfo, 0, len(newInfo.Columns))
	for _, newColumn := range newInfo.Columns {
		if val, ok := oldInfo.Index[newColumn.Name]; ok {
			oldColumn := oldInfo.Columns[val]
			if oldColumn.Type != newColumn.Type ||
				oldColumn.Args != newColumn.Args ||
				oldColumn.Comment != newColumn.Comment ||
				oldColumn.IsNull != newColumn.IsNull ||
				!hutl.Equal(oldColumn.Default, newColumn.Default) {

				modifyList = append(modifyList, newColumn)
			}
		} else {
			addList = append(addList, newColumn)
		}
	}
	var s strings.Builder
	for _, info := range addList {
		s.Reset()
		s.WriteString("ALTER TABLE `")
		s.WriteString(string(newInfo.Schema))
		s.WriteString("`.`")
		s.WriteString(string(newInfo.Table))
		s.WriteString("` ADD COLUMN ")
		s.WriteString(string(info.Name))
		s.WriteString(" ")
		s.WriteString(info.Type)
		if info.Args != "" {
			s.WriteString(info.Args)
		}
		if info.IsNull != "YES" {
			s.WriteString(" NOT NULL")
		}
		if info.Default != nil {
			s.WriteString(" DEFAULT ")
			s.WriteString(*info.Default)
		}
		if info.Comment != "" {
			s.WriteString(" COMMENT ")
			s.WriteString("'")
			s.WriteString(info.Comment)
			s.WriteString("'")
		}
		hlog.Info(ctx, "modifyTable: %s", s.String())
		_, err := c.sql.ExecContext(ctx, s.String())
		if err != nil {
			return herror.Wrap(err)
		}
		err = c.waitSchemaChangeComplete(ctx, newInfo.Schema, newInfo.Table)
		if err != nil {
			return err
		}
	}

	for _, info := range modifyList {
		s.Reset()
		s.WriteString("ALTER TABLE `")
		s.WriteString(string(newInfo.Schema))
		s.WriteString("`.`")
		s.WriteString(string(newInfo.Table))
		s.WriteString("` MODIFY COLUMN ")
		s.WriteString(string(info.Name))
		s.WriteString(" ")
		s.WriteString(info.Type)
		if info.Args != "" {
			s.WriteString(info.Args)
		}
		if info.IsNull != "YES" {
			s.WriteString(" NOT NULL")
		}
		if info.Default != nil {
			s.WriteString(" DEFAULT ")
			s.WriteString(*info.Default)
		}
		if info.Comment != "" {
			s.WriteString(" COMMENT ")
			s.WriteString("'")
			s.WriteString(info.Comment)
			s.WriteString("'")
		}
		hlog.Info(ctx, "modifyTable %s", s.String())
		_, err := c.sql.ExecContext(ctx, s.String())
		if err != nil {
			return herror.Wrap(err)
		}
		err = c.waitSchemaChangeComplete(ctx, newInfo.Schema, newInfo.Table)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Doris) waitSchemaChangeComplete(ctx context.Context, schema Schema, table Table) error {
	query := fmt.Sprintf(
		"SHOW ALTER TABLE COLUMN FROM `%s` WHERE TableName = '%s' ORDER BY CreateTime DESC LIMIT 1",
		schema, table,
	)

	hlog.Info(ctx, "waitSchemaChangeComplete: %s", query)
	maxRetries := 300 // 最多等 5 分钟（每次 1 秒）
	for i := 0; i < maxRetries; i++ {
		rows, err := c.sql.QueryContext(ctx, query)
		if err != nil {
			return herror.Wrap(err)
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return err
		}
		maps := make(map[string]int, len(columns))
		values := make([]any, len(columns))
		for i := range values {
			values[i] = new(any)
			maps[strings.ToLower(columns[i])] = i
		}

		for rows.Next() {
			err := rows.Scan(values...)
			if err != nil {
				return herror.Wrap(err)
			}
		}
		state := values[maps["state"]]
		if state != nil {
			state = *state.(*any)
		}

		progress := values[maps["progress"]]
		if progress != nil {
			progress = *progress.(*any)
		}
		switch string(state.([]byte)) {
		case "FINISHED", "CANCELLED":
			return nil
		case "RUNNING", "PENDING", "WAITING_TXN":
			hlog.Info(ctx, "Schema Change %s, progress: %s, waiting...", state, progress)
			time.Sleep(1 * time.Second)
		default:
			return fmt.Errorf("unknown state: %s", state)
		}
	}
	return fmt.Errorf("wait schema change timeout after %d seconds", maxRetries)
}
