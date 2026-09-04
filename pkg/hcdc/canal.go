package hcdc

import (
	"context"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/client"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/go-mysql-org/go-mysql/schema"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

type OnData func(ctx context.Context, schema Schema, table Table, action Action, columns []Column, values [][]RawBytes) error
type onCreateTable func(ctx context.Context, schema Schema, table Table, columns []ColumnInfo) error

type CanalConfig struct {
	Host          string   `yaml:"host"`          // 数据库主机地址
	Username      string   `yaml:"username"`      // 数据库用户名
	Password      string   `yaml:"password"`      // 数据库密码
	Schema        string   `yaml:"schema"`        // 数据库名称
	IncludeDBs    []string `yaml:"includeDBs"`    //支持的库
	ExcludeDBs    []string `yaml:"excludeDBs"`    //排除的库
	IncludeTables []string `yaml:"includeTables"` //支持的表
	ExcludeTables []string `yaml:"excludeTables"` //排除的表
	ServerID      *uint32  `yaml:"serverID"`      // 服务器ID
	Charset       string   `yaml:"charset"`       // 字符集
	Flavor        string   `yaml:"flavor"`        // 数据库类型
}

func (c *CanalConfig) Validate(ctx context.Context) bool {
	var valid bool = true
	return valid
}
func (c *CanalConfig) Equal(other *CanalConfig) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	if len(c.IncludeDBs) != len(other.IncludeDBs) {
		return false
	}
	for i := range c.IncludeDBs {
		if c.IncludeDBs[i] != other.IncludeDBs[i] {
			return false
		}
	}

	if len(c.ExcludeDBs) != len(other.ExcludeDBs) {
		return false
	}
	for i := range c.ExcludeDBs {
		if c.ExcludeDBs[i] != other.ExcludeDBs[i] {
			return false
		}
	}

	if len(c.IncludeTables) != len(other.IncludeTables) {
		return false
	}
	for i := range c.IncludeTables {
		if c.IncludeTables[i] != other.IncludeTables[i] {
			return false
		}
	}

	if len(c.ExcludeTables) != len(other.ExcludeTables) {
		return false
	}
	for i := range c.ExcludeTables {
		if c.ExcludeTables[i] != other.ExcludeTables[i] {
			return false
		}
	}

	return c.Host == other.Host &&
		c.Username == other.Username &&
		c.Password == other.Password &&
		c.Schema == other.Schema &&
		c.Charset == other.Charset &&
		c.Flavor == other.Flavor
}

func NewCanal(cfg *CanalConfig) *Canal {
	ret := &Canal{
		cfg:     cfg,
		schemas: make(map[Schema]map[Table][]ColumnInfo),
	}
	return ret
}

type Canal struct {
	canal.DummyEventHandler // 嵌入空实现的事件处理器，按需覆盖
	cfg                     *CanalConfig
	canal                   *canal.Canal
	conn                    *client.Conn
	excludeDBs              []*regexp.Regexp
	includeDBs              []*regexp.Regexp
	excludeTables           []*regexp.Regexp
	includeTables           []*regexp.Regexp
	onData                  OnData
	onCreateTable           onCreateTable
	schemas                 map[Schema]map[Table][]ColumnInfo
	lock                    sync.Mutex
}

func (c *Canal) setOnData(fn OnData) {
	c.onData = fn
}

func (c *Canal) setOnCreateTable(fn onCreateTable) {
	c.onCreateTable = fn
}

func (c *Canal) Open(ctx context.Context) error {
	err := c.initFilter(ctx)
	if err != nil {
		return err
	}

	// 建立直连，用于执行查询和锁表操作
	c.conn, err = client.Connect(
		c.cfg.Host,
		c.cfg.Username,
		c.cfg.Password,
		c.cfg.Schema,
	)
	if err != nil {
		return herror.Wrap(err)
	}

	err = c.createSchemaTable(ctx)
	if err != nil {
		return err
	}

	//err = c.loadData(ctx)
	//if err != nil {
	//	return err
	//}

	cfg := canal.NewDefaultConfig()
	if c.cfg.ServerID != nil {
		cfg.ServerID = *c.cfg.ServerID
	}
	cfg.Addr = c.cfg.Host
	cfg.User = c.cfg.Username
	cfg.Password = c.cfg.Password
	cfg.Charset = c.cfg.Charset
	cfg.Flavor = c.cfg.Flavor
	cfg.TimestampStringLocation = time.UTC
	//cfg.ParseTime = true
	cfg.Dump.ExecutionPath = ""
	cfg.IncludeTableRegex = c.cfg.IncludeTables
	cfg.ExcludeTableRegex = c.cfg.ExcludeTables

	c.canal, err = canal.NewCanal(cfg)
	if err != nil {
		return herror.Wrap(err)
	}
	c.canal.SetEventHandler(c)

	// 获取当前 master 的 binlog 位置，作为同步起点
	pos, err := c.canal.GetMasterPos()
	if err != nil {
		return herror.Wrap(err)
	}

	go func() {
		err := c.canal.RunFrom(pos)
		if err != nil {
			hlog.Error(ctx, "canal run error: %v", err)
		}
	}()
	return nil
}

// OnTableChanged 当表结构被修改，或者是由于 DDL 导致 Canal 内部缓存的元数据失效时触发。
func (c *Canal) OnTableChanged(header *replication.EventHeader, schema string, table string) error {
	// 当上游加字段减字段时触发，你可以在这里重新调用 c.GetColumns 获取最新结构
	// 并在 Doris 侧执行 "ALTER TABLE ... ADD COLUMN" 动态同步表结构变更
	return nil
}

// OnDDL 当上游执行了 CREATE TABLE、ALTER TABLE、DROP TABLE 等 SQL 语句时触发。
func (c *Canal) OnDDL(header *replication.EventHeader, nextPos mysql.Position, queryEvent *replication.QueryEvent) error {
	//sql := strings.ToLower(string(queryEvent.Query))
	//schema := string(queryEvent.Schema)
	//
	//// 如果库不满足过滤条件，直接忽略
	//if !c.FilterDatabase(schema) {
	//	return nil
	//}
	//
	//// 探测是否是创建新表 DDL (例如: "create table `test_table` ...")
	//if strings.Contains(sql, "create table") {
	//	// 1. 简易正则或字符串解析出表名 (假设解析出来为 tableName)
	//	tableName := "parsed_table_name"
	//
	//	if c.FilterTable(tableName) {
	//		ctx := context.Background()
	//		// 2. 实时触发：直接调用我们之前写好的初始化函数（Doris 建表 + 注册 Worker）
	//		go func() {
	//			_ = c.initNewTableSync(ctx, Schema(schema), Table(tableName))
	//			c.activeTables.Store(schema+"."+tableName, true)
	//		}()
	//	}
	//}
	return nil
}

func (c *Canal) OnRow(e *canal.RowsEvent) error {
	if c.onData == nil {
		return nil
	}

	columns := hutl.Slice(e.Table.Columns, func(i int, v schema.TableColumn) Column {
		return Column(v.Name)
	})
	if e.Action == "insert" {
		return c.onData(hlog.NewContext(), Schema(e.Table.Schema), Table(e.Table.Name), Insert, columns, [][]RawBytes{
			hutl.Slice(e.Rows[0], func(i int, v any) RawBytes {
				return c.toRawBytes(e.Table.Columns[i], v)
			}),
		})
	} else if e.Action == "update" {
		return c.onData(hlog.NewContext(), Schema(e.Table.Schema), Table(e.Table.Name), Update, columns, [][]RawBytes{
			hutl.Slice(e.Rows[1], func(i int, v any) RawBytes {
				return c.toRawBytes(e.Table.Columns[i], v)
			}),
		})
	} else if e.Action == "delete" {
		return c.onData(hlog.NewContext(), Schema(e.Table.Schema), Table(e.Table.Name), Delete, columns, [][]RawBytes{
			hutl.Slice(e.Rows[0], func(i int, v any) RawBytes {
				return c.toRawBytes(e.Table.Columns[i], v)
			}),
		})
	}

	return nil
}
func (c *Canal) toRawBytes(column schema.TableColumn, v any) RawBytes {
	if v == nil {
		return nil
	}
	switch v.(type) {
	case int8:
		return RawBytes(strconv.FormatInt(int64(v.(int8)), 10))
	case int16:
		return RawBytes(strconv.FormatInt(int64(v.(int16)), 10))
	case int32:
		return RawBytes(strconv.FormatInt(int64(v.(int32)), 10))
	case int64:
		return RawBytes(strconv.FormatInt(int64(v.(int64)), 10))
	case uint8:
		return RawBytes(strconv.FormatInt(int64(v.(uint8)), 10))
	case uint16:
		return RawBytes(strconv.FormatInt(int64(v.(uint16)), 10))
	case uint32:
		return RawBytes(strconv.FormatInt(int64(v.(uint32)), 10))
	case uint64:
		return RawBytes(strconv.FormatInt(int64(v.(uint64)), 10))
	case int:
		return RawBytes(strconv.FormatInt(int64(v.(int)), 10))
	case float32:
		return RawBytes(strconv.FormatFloat(float64(v.(float32)), 'f', -1, 64))
	case float64:
		return RawBytes(strconv.FormatFloat(v.(float64), 'f', -1, 64))
	case []byte:
		return RawBytes(v.([]byte))
	case string:
		return RawBytes(v.(string))
	default:
		return RawBytes(v.(string))
	}
}

// OnPosSynced 这是高可用架构中最为关键的方法。你应该在这里将 pos (包含文件名和 Offset) 持久化存储到 Redis、MySQL 或本地文件中。这样一旦程序崩溃重启，就能从该位点精准恢复，做到不重不漏。
func (c *Canal) OnPosSynced(header *replication.EventHeader, pos mysql.Position, set mysql.GTIDSet, force bool) error {
	// 伪代码：持久化当前的 binlog 位点
	// c.savePositionToStorage(pos.Name, pos.Pos)
	return nil
}

// OnTableNotFound 作用： 当 Binlog 里存在某张表的数据变更，但是 Canal 试图去上游查询该表的元数据（列名、类型）时发现表已经不存在（可能被暴力 DROP 了）。业务实现： 应该捕获并记录高级别告警日志，防止程序 Panic，同时可在本地删除对应的 Doris Worker 映射。
func (c *Canal) OnTableNotFound(header *replication.EventHeader, event *replication.RowsEvent) error {
	// 打印告警：上游对应的表可能已经被物理删除
	return nil
}

// initFilter 初始化过滤器
func (c *Canal) initFilter(ctx context.Context) error {
	for _, item := range c.cfg.ExcludeDBs {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.excludeDBs = append(c.excludeDBs, compile)
	}
	for _, item := range c.cfg.IncludeDBs {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.includeDBs = append(c.includeDBs, compile)
	}
	for _, item := range c.cfg.ExcludeTables {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.excludeTables = append(c.excludeTables, compile)
	}
	for _, item := range c.cfg.IncludeTables {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.includeTables = append(c.includeTables, compile)
	}
	return nil
}

// GetDatabases 获得所有的库
func (c *Canal) GetDatabases(ctx context.Context) ([]string, error) {
	result, err := c.conn.Execute("SHOW DATABASES")
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer result.Close()

	var dbs []string
	for _, rows := range result.Values {
		dbs = append(dbs, string(rows[0].AsString()))
	}
	return dbs, nil
}

// FilterDatabase 过滤库
func (c *Canal) FilterDatabase(name string) bool {
	for _, exclude := range c.excludeDBs {
		if exclude.MatchString(name) {
			return false
		}
	}
	for _, include := range c.includeDBs {
		if include.MatchString(name) {
			return true
		}
	}
	return false
}

// GetTables 获得所有的表
func (c *Canal) GetTables(ctx context.Context, schema Schema) ([]string, error) {
	// 1. 将 schema 拼入 SQL，确保只查目标库。注意：如果 schema 包含特殊字符，建议用反引号包裹 `schema`
	query := "SHOW TABLES FROM `" + string(schema) + "`"
	result, err := c.conn.Execute(query)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer result.Close()

	var tables []string
	for _, rows := range result.Values {
		// 2. 增加防御性代码：确保这一行有数据，防止索引越界或空指针导致 panic
		if len(rows) > 0 {
			tables = append(tables, string(rows[0].AsString()))
		}
	}
	return tables, nil
}

// FilterTable 过滤库
func (c *Canal) FilterTable(name string) bool {
	for _, exclude := range c.excludeTables {
		if exclude.MatchString(name) {
			return false
		}
	}
	for _, include := range c.includeTables {
		if include.MatchString(name) {
			return true
		}
	}
	return false
}

// GetColumns 获得指定表的结构
func (c *Canal) GetColumns(ctx context.Context, schema Schema, table Table) ([]ColumnInfo, error) {
	query := "SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_COMMENT, COLUMN_KEY FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?"
	result, err := c.conn.Execute(query, string(schema), string(table))
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var columns = make([]ColumnInfo, len(result.Values))
	for i, rows := range result.Values {
		columns[i] = ColumnInfo{
			Name:    string(rows[0].AsString()),
			Type:    string(rows[1].AsString()),
			Comment: string(rows[2].AsString()),
			IsKey:   string(rows[3].AsString()) == "PRI",
		}
	}
	return columns, nil
}

func (c *Canal) ReadData(ctx context.Context, schema Schema, table Table, columns []ColumnInfo, start, end string) error {
	if c.onData == nil {
		return nil
	}

	key := "id"
	for _, column := range columns {
		if column.IsKey {
			key = column.Name
			break
		}
	}
	newColumns := hutl.Slice(columns, func(i int, v ColumnInfo) Column {
		return Column(v.Name)
	})

	batch := hutl.NewBatchProcess(500, func(values [][]RawBytes) error {
		return c.onData(ctx, schema, table, Insert, newColumns, values)
	})

	query := "SELECT * FROM `" + string(schema) + "`.`" + string(table) + "` WHERE `" + key + "` > " + start + " AND `" + key + "` <= " + end
	var result mysql.Result
	err := c.conn.ExecuteSelectStreaming(query, &result, func(row []mysql.FieldValue) error {
		return batch.AddData(hutl.Slice(row, func(i int, value mysql.FieldValue) RawBytes {
			switch value.Type {
			case mysql.FieldValueTypeUnsigned:
				return RawBytes(strconv.FormatUint(value.AsUint64(), 10))
			case mysql.FieldValueTypeSigned:
				return RawBytes(strconv.FormatInt(value.AsInt64(), 10))
			case mysql.FieldValueTypeFloat:
				return RawBytes(strconv.FormatFloat(value.AsFloat64(), 'f', -1, 64))
			case mysql.FieldValueTypeString:
				return RawBytes(value.AsString())
			default:
				return nil
			}
		}))
	}, func(result *mysql.Result) error {
		return nil
	})
	if err != nil {
		return err
	}

	return batch.Finish()
}

func (c *Canal) Close() {
	if c.canal != nil {
		c.canal.Close()
		c.canal = nil
	}
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Canal) createSchemaTable(ctx context.Context) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	dbs, err := c.GetDatabases(ctx)
	if err != nil {
		return err
	}
	dbs = hutl.Filter(dbs, func(v string) bool {
		return c.FilterDatabase(v)
	})

	for _, db := range dbs {
		tables, err := c.GetTables(ctx, Schema(db))
		if err != nil {
			return err
		}

		tables = hutl.Filter(tables, func(v string) bool {
			return c.FilterTable(v)
		})
		if _, ok := c.schemas[Schema(db)]; !ok {
			c.schemas[Schema(db)] = make(map[Table][]ColumnInfo)
		}
		for _, table := range tables {
			if c.FilterTable(table) {

				columns, err := c.GetColumns(ctx, Schema(db), Table(table))
				if err != nil {
					return err
				}

				c.schemas[Schema(db)][Table(table)] = columns
				err = c.onCreateTable(ctx, Schema(db), Table(table), columns)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Canal) loadData(ctx context.Context) error {
	for schema, tables := range c.schemas {
		for table := range tables {
			err := c.ReadData(ctx, schema, table, c.schemas[schema][table], "0", "9223372036854775807")
			if err != nil {
				return err
			}
		}
	}
	return nil
}
