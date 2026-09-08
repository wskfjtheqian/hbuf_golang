package hcdc

import (
	"context"
	"encoding/base64"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"database/sql"

	"github.com/RoaringBitmap/roaring"
	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/go-mysql-org/go-mysql/schema"
	_ "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

type OnData func(ctx context.Context, schema Schema, table Table, action Action, columns []ColumnInfo, values [][]RawBytes) error
type OnCreateTable func(ctx context.Context, infos []TableInfo) error
type OnCreateSchema func(ctx context.Context, schema Schema) error

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
		schemas: make(map[Schema]map[Table]TableInfo),
	}
	return ret
}

type Canal struct {
	canal.DummyEventHandler // 嵌入空实现的事件处理器，按需覆盖
	cfg                     *CanalConfig
	canal                   *canal.Canal
	//conn                    *client.Conn
	excludeDBs     []*regexp.Regexp
	includeDBs     []*regexp.Regexp
	excludeTables  []*regexp.Regexp
	includeTables  []*regexp.Regexp
	onData         OnData
	onCreateTable  OnCreateTable
	onCreateSchema OnCreateSchema
	schemas        map[Schema]map[Table]TableInfo
	lock           sync.Mutex
	sql            *sql.DB
}

func (c *Canal) SetOnData(fn OnData) {
	c.onData = fn
}

func (c *Canal) setOnCreateTable(fn OnCreateTable) {
	c.onCreateTable = fn
}
func (c *Canal) setOnCreateSchema(fn OnCreateSchema) {
	c.onCreateSchema = fn
}

func (c *Canal) Open(ctx context.Context) error {
	err := c.initFilter(ctx)
	if err != nil {
		return err
	}

	// 建立直连，用于执行查询和锁表操作
	//c.conn, err = client.Connect(
	//	c.cfg.Host,
	//	c.cfg.Username,
	//	c.cfg.Password,
	//	c.cfg.Schema,
	//)
	//if err != nil {
	//	return herror.Wrap(err)
	//}

	c.sql, err = sql.Open("mysql", c.cfg.Username+":"+c.cfg.Password+"@tcp("+c.cfg.Host+")/"+c.cfg.Schema)
	if err != nil {
		return err
	}

	err = c.createSchemaTable(ctx)
	if err != nil {
		return err
	}

	err = c.loadData(ctx)
	if err != nil {
		return err
	}

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
	// 当上游加字段减字段时触发，你可以在这里重新调用 c.GetTableInfo 获取最新结构
	// 并在 Doris 侧执行 "ALTER TABLE ... ADD COLUMN" 动态同步表结构变更
	ctx := hlog.NewContext()
	info, err := c.GetTableInfo(ctx, Schema(schema), Table(table))
	if err != nil {
		return err
	}
	if info != nil {
		err = c.onCreateTable(ctx, []TableInfo{*info})
		if err != nil {
			return err
		}
	}
	return nil
}

// OnDDL 当上游执行了 CREATE TABLE、ALTER TABLE、DROP TABLE 等 SQL 语句时触发。
func (c *Canal) OnDDL(header *replication.EventHeader, nextPos mysql.Position, queryEvent *replication.QueryEvent) error {
	// 如果库不满足过滤条件，直接忽略
	if !c.FilterDatabase(string(queryEvent.Schema)) {
		return nil
	}

	//// 探测是否是创建新表 DDL (例如: "change table `test_table` ...")
	//if strings.Contains(sql, "change table") {
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

// OnPosSynced 这是高可用架构中最为关键的方法。你应该在这里将 pos (包含文件名和 Offset) 持久化存储到 Redis、MySQL 或本地文件中。这样一旦程序崩溃重启，就能从该位点精准恢复，做到不重不漏。
func (c *Canal) OnPosSynced(header *replication.EventHeader, pos mysql.Position, set mysql.GTIDSet, force bool) error {
	// 伪代码：持久化当前的 binlog 位点
	// c.savePositionToStorage(pos.Name, pos.Pos)
	return nil
}

func (c *Canal) OnRow(e *canal.RowsEvent) error {
	if c.onData == nil {
		return nil
	}
	val, ok := c.schemas[Schema(e.Table.Schema)]
	if !ok || val == nil {
		return nil
	}
	info, ok := val[Table(e.Table.Name)]
	if !ok {
		return nil
	}
	columns := hutl.Slice(e.Table.Columns, func(i int, v schema.TableColumn) ColumnInfo {
		return info.Columns[info.Index[Column(v.Name)]]
	})

	ctx := hlog.NewContext()
	if e.Action == "insert" {
		return c.onData(ctx, Schema(e.Table.Schema), Table(e.Table.Name), Insert, columns, [][]RawBytes{
			hutl.Slice(e.Rows[0], func(i int, v any) RawBytes {
				return c.toRawBytes(ctx, &info.Columns[info.Index[Column(e.Table.Columns[i].Name)]], v)
			}),
		})
	} else if e.Action == "update" {
		return c.onData(ctx, Schema(e.Table.Schema), Table(e.Table.Name), Update, columns, [][]RawBytes{
			hutl.Slice(e.Rows[1], func(i int, v any) RawBytes {
				return c.toRawBytes(ctx, &info.Columns[info.Index[Column(e.Table.Columns[i].Name)]], v)
			}),
		})
	} else if e.Action == "delete" {
		return c.onData(ctx, Schema(e.Table.Schema), Table(e.Table.Name), Delete, columns, [][]RawBytes{
			hutl.Slice(e.Rows[0], func(i int, v any) RawBytes {
				return c.toRawBytes(ctx, &info.Columns[info.Index[Column(e.Table.Columns[i].Name)]], v)
			}),
		})
	}

	return nil
}
func (c *Canal) toRawBytes(ctx context.Context, col *ColumnInfo, v any) RawBytes {
	if v == nil {
		return nil
	}
	if reflect.TypeOf(v).Kind() == reflect.Ptr {
		v = reflect.ValueOf(v).Elem().Interface()
	}
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
		val := v.([]byte)
		typ := GetColumnType(col)
		if typ == "date" || typ == "datetime" || typ == "timestamp" {
			return RawBytes(strings.ReplaceAll(string(val), "0000-00-00", "1970-01-01"))
		} else if typ == "decimal" || typ == "time" {
			return val
		} else if typ == "bitmap32" || typ == "bitmap64" {
			val = c.bitmapToArrayString(ctx, val, strings.Contains(col.Comment, "CustomType=Bitmap32"))
		}
		return RawBytes(base64.StdEncoding.EncodeToString([]byte(val)))
	case string:
		val := v.(string)
		typ := GetColumnType(col)
		if typ == "date" || typ == "datetime" || typ == "timestamp" {
			return RawBytes(strings.ReplaceAll(val, "0000-00-00", "1970-01-01"))
		} else if typ == "decimal" || typ == "time" {
			return RawBytes(val)
		} else if typ == "bitmap32" || typ == "bitmap64" {
			val = string(c.bitmapToArrayString(ctx, []byte(val), strings.Contains(col.Comment, "CustomType=Bitmap32")))
		}
		return RawBytes(base64.StdEncoding.EncodeToString([]byte(val)))
	default:
		return RawBytes(base64.StdEncoding.EncodeToString([]byte(v.(string))))
	}
}

// 转换 Bitmap
func (c *Canal) bitmapToArrayString(ctx context.Context, val []byte, bitmap32 bool) RawBytes {
	buffer := make([]byte, 0, 512)
	if bitmap32 {
		bitmap := roaring.NewBitmap()
		_, err := bitmap.FromUnsafeBytes(val)
		if err != nil {
			hlog.Error(ctx, "bitmap from bytes error: %v", err)
			return nil
		}
		iterator := bitmap.Iterator()
		for iterator.HasNext() {
			buffer = append(buffer, strconv.FormatUint(uint64(iterator.Next()), 10)...)
			buffer = append(buffer, ',')
		}
	} else {
		bitmap := roaring64.NewBitmap()
		_, err := bitmap.FromUnsafeBytes(val)
		if err != nil {
			hlog.Error(ctx, "bitmap from bytes error: %v", err)
			return nil
		}
		iterator := bitmap.Iterator()
		for iterator.HasNext() {
			buffer = append(buffer, strconv.FormatUint(iterator.Next(), 10)...)
			buffer = append(buffer, ',')
		}
	}
	if len(buffer) > 0 {
		buffer = buffer[:len(buffer)-1]
	}
	return buffer
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
	//result, err := c.conn.Execute("SHOW DATABASES")
	//if err != nil {
	//	return nil, herror.Wrap(err)
	//}
	//defer result.Close()
	//
	//var dbs []string
	//for _, rows := range result.Values {
	//	dbs = append(dbs, string(rows[0].AsString()))
	//}

	query, err := c.sql.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer query.Close()

	var dbs []string
	for query.Next() {
		var db *string
		if err := query.Scan(&db); err != nil {
			return nil, herror.Wrap(err)
		}
		if db == nil {
			continue
		}
		dbs = append(dbs, *db)
	}
	if err := query.Err(); err != nil {
		return nil, herror.Wrap(err)
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
	//query := "SHOW TABLES FROM `" + string(schema) + "`"
	//result, err := c.conn.Execute(query)
	//if err != nil {
	//	return nil, herror.Wrap(err)
	//}
	//defer result.Close()
	//
	//var tables []string
	//for _, rows := range result.Values {
	//	// 2. 增加防御性代码：确保这一行有数据，防止索引越界或空指针导致 panic
	//	if len(rows) > 0 {
	//		tables = append(tables, string(rows[0].AsString()))
	//	}
	//}

	query, err := c.sql.QueryContext(ctx, "SHOW TABLES FROM `"+string(schema)+"`")
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer query.Close()

	var tables []string
	for query.Next() {
		var table *string
		if err := query.Scan(&table); err != nil {
			return nil, herror.Wrap(err)
		}
		if table == nil {
			continue
		}
		tables = append(tables, *table)
	}
	if err := query.Err(); err != nil {
		return nil, herror.Wrap(err)
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

// GetKeys 获得指定表的主键
func (c *Canal) GetKeys(ctx context.Context, schema Schema, table Table) (map[string]int, error) {
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

// GetTableInfo 获得指定表的结构
func (c *Canal) GetTableInfo(ctx context.Context, schema Schema, table Table) (*TableInfo, error) {
	hlog.Info(ctx, "get table info: %s.%s", schema, table)
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
	//}
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
		column.Args = args[len(column.Type):]
		column.KeyIndex = keys[string(column.Name)]
		if column.Default != nil {
			column.Default = hutl.ToPointer(strings.ToUpper(*column.Default))
		}
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

func (c *Canal) ReadData(ctx context.Context, schema Schema, table Table, info TableInfo, start, end string) error {
	if c.onData == nil {
		return nil
	}

	var key Column = "id"
	for _, column := range info.Columns {
		if column.KeyIndex > 0 {
			key = column.Name
			break
		}
	}

	fields := make([]ColumnInfo, 0)
	batch := hutl.NewBatchProcess(500, func(values [][]RawBytes) error {
		return c.onData(ctx, schema, table, Insert, fields, values)
	})

	//query := "SELECT * FROM `" + string(schema) + "`.`" + string(table) + "` WHERE `" + string(key) + "` > " + start + " AND `" + string(key) + "` <= " + end
	//var result mysql.Result
	//err := c.conn.ExecuteSelectStreaming(query, &result, func(row []mysql.FieldValue) error {
	//	return batch.AddData(hutl.Slice(row, func(i int, value mysql.FieldValue) RawBytes {
	//		switch value.Type {
	//		case mysql.FieldValueTypeUnsigned:
	//			return RawBytes(strconv.FormatUint(value.AsUint64(), 10))
	//		case mysql.FieldValueTypeSigned:
	//			return RawBytes(strconv.FormatInt(value.AsInt64(), 10))
	//		case mysql.FieldValueTypeFloat:
	//			return RawBytes(strconv.FormatFloat(value.AsFloat64(), 'f', -1, 64))
	//		case mysql.FieldValueTypeString:
	//			return RawBytes(base64.StdEncoding.EncodeToString(value.AsString()))
	//		default:
	//			return nil
	//		}
	//	}))
	//}, func(result *mysql.Result) error {
	//	for _, field := range result.Fields {
	//		fields = append(fields, info.Columns[info.Index[Column(field.Name)]])
	//	}
	//	return nil
	//})
	//
	//
	//if err != nil {
	//	return err
	//}

	query, err := c.sql.QueryContext(ctx, "SELECT * FROM `"+string(schema)+"`.`"+string(table)+"` WHERE `"+string(key)+"` > ? AND `"+string(key)+"` <= ?", start, end)
	if err != nil {
		return herror.Wrap(err)
	}
	defer query.Close()
	columns, err := query.Columns()
	if err != nil {
		return herror.Wrap(err)
	}
	for _, field := range columns {
		fields = append(fields, info.Columns[info.Index[Column(field)]])
	}

	for query.Next() {
		var values = hutl.Slice(make([]any, len(fields)), func(i int, v any) any {
			return &v
		})
		if err := query.Scan(values...); err != nil {
			return herror.Wrap(err)
		}
		if err := batch.AddData(hutl.Slice(values, func(i int, v any) RawBytes {
			return c.toRawBytes(ctx, &info.Columns[info.Index[Column(columns[i])]], v)
		})); err != nil {
			return herror.Wrap(err)
		}
	}
	if err := query.Err(); err != nil {
		return herror.Wrap(err)
	}

	return batch.Finish()
}

func (c *Canal) Close() {
	if c.canal != nil {
		c.canal.Close()
		c.canal = nil
	}
	if c.sql != nil {
		_ = c.sql.Close()
		c.sql = nil
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

	infos := make([]TableInfo, 0)
	for _, db := range dbs {
		err = c.onCreateSchema(ctx, Schema(db))
		if err != nil {
			return err
		}

		tables, err := c.GetTables(ctx, Schema(db))
		if err != nil {
			return err
		}

		tables = hutl.Filter(tables, func(v string) bool {
			return c.FilterTable(v)
		})
		if _, ok := c.schemas[Schema(db)]; !ok {
			c.schemas[Schema(db)] = make(map[Table]TableInfo)
		}
		for _, table := range tables {
			if c.FilterTable(table) {

				info, err := c.GetTableInfo(ctx, Schema(db), Table(table))
				if err != nil {
					return err
				}
				if info != nil {
					c.schemas[Schema(db)][Table(table)] = *info
					infos = append(infos, *info)
				}
			}
		}
	}
	if len(infos) == 0 {
		return nil
	}
	return c.onCreateTable(ctx, infos)
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

// GetPartitionType 动态查询 MySQL 上游表的 RANGE 分区字段
func (c *Canal) GetPartitionType(ctx context.Context, schema Schema, table Table) (string, error) {
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
