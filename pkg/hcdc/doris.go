package hcdc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	//"github.com/go-mysql-org/go-mysql/client"
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
	LoadURL  string `yaml:"loadURL"`
}

func (c *DorisConfig) Validate(ctx context.Context) bool {
	if c == nil {
		hlog.Error(ctx, "doris config is nil")
		return false
	}
	if strings.TrimSpace(c.Host) == "" {
		hlog.Error(ctx, "doris config host is empty")
		return false
	}
	if strings.TrimSpace(c.Username) == "" {
		hlog.Error(ctx, "doris config username is empty")
		return false
	}
	if strings.TrimSpace(c.LoadURL) == "" {
		hlog.Error(ctx, "doris config loadURL is empty")
		return false
	}
	return true
}

func (c *DorisConfig) Equal(other *DorisConfig) bool {
	return c.Host == other.Host &&
		c.Username == other.Username &&
		c.Password == other.Password &&
		c.Schema == other.Schema &&
		c.LoadURL == other.LoadURL
}

type ActionInfo struct {
}

type Doris struct {
	cfg     *DorisConfig
	workers map[InstanceId]*Worker
	client  *http.Client
	mu      sync.RWMutex
	//conn    *client.Conn
	sql    *sql.DB
	change chan []TableInfo

	// 同步状态：paused 为 true 时停止 svc -> Doris 的数据上传，但 MySQL -> svc 的采集继续。
	paused  atomic.Bool
	stateMu sync.RWMutex
	state   SyncState

	// monitor 监控器：采集/上传吞吐、延迟、错误、DDL、暂停事件等指标。
	monitor *Monitor
}

func NewDoris(cfg *DorisConfig) *Doris {
	return &Doris{
		cfg:     cfg,
		workers: make(map[InstanceId]*Worker),
		client:  &http.Client{Timeout: 60 * time.Second},
		change:  make(chan []TableInfo),
		monitor: NewMonitor(),
	}
}

func (d *Doris) RegisterWorker(ctx context.Context, worker *Worker) {
	d.mu.Lock()
	d.workers[worker.instanceId] = worker
	d.mu.Unlock()
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
	err = d.createCDCRecord(ctx)
	if err != nil {
		return err
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
			if d.IsPaused() {
				continue
			}
			if err := d.scanFile(ctx); err != nil {
				herror.PrintStack(ctx, err)
			}
		case val := <-d.change:
			// 源表结构发生变化：先自动暂停 svc -> Doris 的数据上传，
			// 待 Doris 侧 DDL 同步完成并经人工确认后，再手动 Resume 恢复。
			for _, info := range val {
				d.pauseForSchemaChange(ctx, info)
				err := d.changeTable(ctx, &info)
				if err != nil {
					herror.PrintStack(ctx, err)
				}
			}
		}
	}
}

// Pause 手动暂停 svc -> Doris 的数据同步。MySQL -> svc 的采集不受影响，数据会继续缓冲到本地文件。
func (d *Doris) Pause(ctx context.Context, reason string) {
	d.stateMu.Lock()
	if !d.paused.Load() {
		d.state = SyncState{
			Status:   SyncPaused,
			Reason:   reason,
			PausedAt: time.Now(),
		}
	} else {
		// 已暂停时仅补充原因，保留首次暂停时间与触发源。
		if reason != "" {
			d.state.Reason = reason
		}
	}
	d.paused.Store(true)
	d.stateMu.Unlock()
	d.monitor.PauseCount.Add(1)
	hlog.Warn(ctx, "svc -> doris sync paused: %s", reason)
}

// pauseForSchemaChange 在源表结构变化时自动暂停同步。
func (d *Doris) pauseForSchemaChange(ctx context.Context, info TableInfo) {
	reason := fmt.Sprintf("source table schema changed: %s.%s (action=%s)", info.Schema, info.Table, info.Action)
	d.stateMu.Lock()
	if !d.paused.Load() {
		d.state = SyncState{
			Status:   SyncPaused,
			Reason:   reason,
			PausedAt: time.Now(),
			Schema:   info.Schema,
			Table:    info.Table,
		}
	} else {
		// 已暂停时仅补充原因，保留首次暂停时间与触发源。
		d.state.Reason = reason
	}
	d.paused.Store(true)
	d.stateMu.Unlock()
	d.monitor.PauseCount.Add(1)
	d.monitor.autoPause.Add(1)
	hlog.Warn(ctx, "svc -> doris sync auto paused: %s", reason)
}

// Resume 手动恢复 svc -> Doris 的数据同步。
func (d *Doris) Resume(ctx context.Context) {
	d.stateMu.Lock()
	d.state = SyncState{Status: SyncRunning}
	d.paused.Store(false)
	d.stateMu.Unlock()
	d.monitor.ResumeCount.Add(1)
	hlog.Info(ctx, "svc -> doris sync resumed")
}

// IsPaused 返回当前是否处于暂停状态。
func (d *Doris) IsPaused() bool {
	return d.paused.Load()
}

// GetSyncState 返回当前同步状态快照。
func (d *Doris) GetSyncState() SyncState {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.state
}

func (d *Doris) ChangeTables(ctx context.Context, infos []TableInfo) {
	d.change <- infos
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

	hlog.Info(ctx, "doris stream save: %s.%s columns: %s", schema, table, columns)
	req.SetBasicAuth(d.cfg.Username, d.cfg.Password)
	req.Header.Set("Expect", "100-continue")
	req.Header.Set("column_separator", ",")

	// ✨ 核心映射：动态把从文件首行解析出来的 columns 传给 Doris
	req.Header.Set("columns", columns)
	req.Header.Set("merge_type", "MERGE")
	req.Header.Set("delete", "__op=2")
	req.Header.Set("strict_mode", "false")

	start := time.Now()
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

	// 记录上传指标（业务表与 cdc_record 位点表共用同一上传通道，这里统一统计字节/行数与耗时）
	elapsedMs := time.Since(start).Milliseconds()
	d.monitor.UploadFiles.Add(1)
	d.monitor.UploadBytes.Add(int64(dorisResp.LoadBytes))
	d.monitor.UploadRows.Add(int64(dorisResp.NumberLoadedRows))
	d.monitor.UploadTotalMs.Add(elapsedMs)
	for {
		max := d.monitor.uploadMaxMs.Load()
		if elapsedMs <= max || d.monitor.uploadMaxMs.CompareAndSwap(max, elapsedMs) {
			break
		}
	}
	return nil
}

// Close 关闭 Doris 连接
func (d *Doris) Close() error {
	if d.sql != nil {
		return d.sql.Close()
	}
	return nil
}

func (d *Doris) CreateSchema(ctx context.Context, schema Schema) error {
	var s strings.Builder
	s.WriteString("CREATE DATABASE IF NOT EXISTS ")
	s.WriteString(string(schema))

	hlog.Info(ctx, "doris change schema: %s", s.String())
	_, err := d.sql.ExecContext(ctx, s.String())
	if err != nil {
		return err
	}
	return nil
}

// GetReplicationNum 动态探测并计算最安全的副本数
func (d *Doris) GetReplicationNum(ctx context.Context) int {
	// 1. 默认降级策略为 1
	query, err := d.sql.QueryContext(ctx, "SELECT COUNT(*) AS available_be_count FROM BACKENDS() WHERE `Alive` = true;")
	if err != nil {
		return 1
	}
	defer query.Close()

	var count int
	for query.Next() {
		if err := query.Scan(&count); err != nil {
			return 1
		}
	}
	if err := query.Err(); err != nil {
		return 1
	}

	if count > 3 {
		return 3
	}
	if count <= 0 {
		return 1
	}
	return count
}
func (d *Doris) changeTable(ctx context.Context, info *TableInfo) error {
	d.monitor.DDLCount.Add(1)
	if info.IsDrop {
		d.monitor.DropTable.Add(1)
		err := d.dropTable(ctx, info)
		if err != nil {
			d.monitor.DDLErrors.Add(1)
		}
		return err
	}
	newInfo := d.ToDorisTable(info)
	oldInfo, err := d.GetTableInfo(ctx, info.Schema, info.Table)
	if err != nil {
		d.monitor.DDLErrors.Add(1)
		return err
	}
	if oldInfo == nil {
		d.monitor.CreateTable.Add(1)
		err := d.createTable(ctx, newInfo)
		if err != nil {
			d.monitor.DDLErrors.Add(1)
		}
		return err
	}

	d.monitor.AlterTable.Add(1)
	err = d.modifyTable(ctx, oldInfo, newInfo)
	if err != nil {
		d.monitor.DDLErrors.Add(1)
	}
	return err
}

// dropTable 删除 Doris 侧的对应表
func (d *Doris) dropTable(ctx context.Context, info *TableInfo) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", info.Schema, info.Table)
	hlog.Info(ctx, "doris drop table: %s", query)
	_, err := d.sql.ExecContext(ctx, query)
	if err != nil {
		return herror.Wrap(err)
	}
	return nil
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
			hlog.Error(ctx, "createTable: PartitionType is RANGE but PartitionField is empty for table %s.%s", info.Schema, info.Table)
		} else {
			s.WriteString(fmt.Sprintf("AUTO PARTITION BY RANGE (DATE_TRUNC(`%s`, 'DAY')) ()\n", info.PartitionField))
		}
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
	_, err := d.sql.ExecContext(ctx, s.String())
	if err != nil {
		return fmt.Errorf("failed to execute partitioning DDL: %w. SQL: %s", err, s.String())
	}
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
		return "TINYINT", "(4)"
	case "smallint":
		if args == "" {
			return "SMALLINT", "(5)"
		}
		return "SMALLINT", args
	case "int", "integer", "mediumint":
		return "INT", "(11)"
	case "bigint":
		return "BIGINT", "(20)"
	case "largeint":
		if args == "" {
			return "LARGEINT", "(20)"
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
	if len(ret.Columns) == 0 {
		return nil, nil
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
			hlog.Error(ctx, "GetTableInfo: table %s.%s has RANGE partition but no datetime/timestamp partition field", schema, table)
		}
	}

	return ret, nil
}

// GetPartitionType 动态查询 MySQL 上游表的 RANGE 分区字段
func (c *Doris) GetPartitionType(ctx context.Context, schema Schema, table Table) (string, error) {
	// 查询该表是否拥有 RANGE 类型的分区，并找出分区键 (COLUMN_NAME)
	query := `SELECT PARTITION_METHOD FROM INFORMATION_SCHEMA.PARTITIONS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? LIMIT 1;`

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
				oldColumn.IsNull != newColumn.IsNull {
				modifyList = append(modifyList, newColumn)
			}
		} else {
			addList = append(addList, newColumn)
		}
	}
	var s strings.Builder
	for _, info := range addList {
		err := c.waitSchemaChangeComplete(ctx, newInfo.Schema, newInfo.Table)
		if err != nil {
			return err
		}

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
		_, err = c.sql.ExecContext(ctx, s.String())
		if err != nil {
			return herror.Wrap(err)
		}
	}

	for _, info := range modifyList {
		err := c.waitSchemaChangeComplete(ctx, newInfo.Schema, newInfo.Table)
		if err != nil {
			return err
		}

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
		_, err = c.sql.ExecContext(ctx, s.String())
		if err != nil {
			return herror.Wrap(err)
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

		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return err
		}
		maps := make(map[string]int, len(columns))
		values := make([]any, len(columns))
		for i := range values {
			values[i] = new(any)
			maps[strings.ToLower(columns[i])] = i
		}
		if !rows.Next() {
			rows.Close()
			return nil
		}

		err = rows.Scan(values...)
		if err != nil {
			rows.Close()
			return herror.Wrap(err)
		}
		rows.Close()

		stateIdx, ok := maps["state"]
		if !ok {
			return fmt.Errorf("SHOW ALTER TABLE COLUMN result missing 'state' column")
		}
		state := values[stateIdx]
		if state != nil {
			state = *state.(*any)
		}
		var stateStr string
		if state != nil {
			switch v := state.(type) {
			case []byte:
				stateStr = string(v)
			case string:
				stateStr = v
			default:
				stateStr = fmt.Sprintf("%v", state)
			}
		}

		var progressStr string
		if idx, ok := maps["progress"]; ok {
			progress := values[idx]
			if progress != nil {
				progress = *progress.(*any)
			}
			if progress != nil {
				progressStr = fmt.Sprintf("%v", progress)
			}
		}

		switch stateStr {
		case "FINISHED", "CANCELLED":
			return nil
		case "RUNNING", "PENDING", "WAITING_TXN":
			hlog.Info(ctx, "Schema Change %s, progress: %s, waiting...", stateStr, progressStr)
			time.Sleep(1 * time.Second)
		default:
			return fmt.Errorf("unknown state: %s", stateStr)
		}
	}
	return fmt.Errorf("wait schema change timeout after %d seconds", maxRetries)
}

func (d *Doris) scanFile(ctx context.Context) error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.workers) == 0 {
		return nil
	}

	// 统计当前积压（待上传的 .active 文件数与字节数），用于监控。
	var backlogFiles, backlogBytes int64
	for _, worker := range d.workers {
		f, b := worker.Backlog()
		backlogFiles += f
		backlogBytes += b
	}
	d.monitor.setBacklog(backlogFiles, backlogBytes)

	reader := bytes.Buffer{}
	for _, worker := range d.workers {
		id, pos, err := worker.ScanFile(ctx, func(ctx context.Context, schema Schema, table Table, columns string, capturedAt time.Time, r io.Reader) error {
			err := d.StreamSave(ctx, schema, table, columns, r)
			if err == nil && !capturedAt.IsZero() {
				// 记录端到端延迟：从数据被采集到上传完成。
				d.monitor.recordDelay(time.Since(capturedAt).Milliseconds())
			}
			return err
		})
		if err != nil {
			d.monitor.UploadErrors.Add(1)
			hlog.Error(ctx, "scan file error: %v", err)
			return err
		}
		if pos != nil {
			reader.WriteString(strconv.FormatInt(int64(id), 10))
			reader.WriteString(",")
			reader.WriteString(pos.Name)
			reader.WriteString(",")
			reader.WriteString(strconv.FormatUint(uint64(pos.Pos), 10))
			reader.WriteString(",0\n")
		}
	}

	if reader.Len() == 0 {
		return nil
	}
	return d.StreamSave(ctx, "cdc_record", "log_position", "instance_id,file,position,__op", &reader)
}

func (d *Doris) createCDCRecord(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS cdc_record")
	if err != nil {
		return herror.Wrap(err)
	}
	_, err = d.sql.QueryContext(ctx, `
			CREATE TABLE IF NOT EXISTS cdc_record.log_position (
				instance_id BIGINT NOT NULL COMMENT '实例ID',
				file VARCHAR(255) NOT NULL COMMENT '文件名',
				position BIGINT NOT NULL COMMENT '位置'
			)UNIQUE KEY(instance_id)
			DISTRIBUTED BY HASH(instance_id) BUCKETS 10
			PROPERTIES (
				"replication_num" = "1"
			)
       `)
	if err != nil {
		return herror.Wrap(err)
	}
	return nil
}
