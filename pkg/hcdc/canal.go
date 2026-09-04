package hcdc

import (
	"context"
	"regexp"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/client"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

type CanalConfig struct {
	Host          string   `yaml:"host"`          // 数据库主机地址
	Username      string   `yaml:"username"`      // 数据库用户名
	Password      string   `yaml:"password"`      // 数据库密码
	Schema        string   `yaml:"schema"`        // 数据库名称
	IncludeDBs    []string `yaml:"includeDBs"`    //支持的库
	ExcludeDBs    []string `yaml:"excludeDBs"`    //排除的库
	IncludeTables []string `yaml:"includeTables"` //支持的表
	ExcludeTables []string `yaml:"excludeTables"` //排除的表

}

func NewCanal(cfg *CanalConfig) *Canal {
	ret := &Canal{
		cfg: cfg,
	}
	return ret
}

type Canal struct {
	cfg           *CanalConfig
	canal         canal.Canal
	conn          *client.Conn
	excludeDBs    []*regexp.Regexp
	includeDBs    []*regexp.Regexp
	excludeTables []*regexp.Regexp
	includeTables []*regexp.Regexp
	schemaMap     map[Schema]byte
	tableMap      map[SchemaTable]byte
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
		return nil, err
	}
	defer result.Close()

	var dbs []string
	for _, rows := range result.Values {
		dbs = append(dbs, rows[0].String())
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
		return nil, err
	}
	defer result.Close()

	var tables []string
	for _, rows := range result.Values {
		// 2. 增加防御性代码：确保这一行有数据，防止索引越界或空指针导致 panic
		if len(rows) > 0 {
			tables = append(tables, rows[0].String())
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
	query := "SELECT COLUMN_NAME, COLUMN_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?"
	result, err := c.conn.Execute(query, string(schema), string(table))
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var columns = make([]ColumnInfo, len(result.Values))
	for i, rows := range result.Values {
		columns[i] = ColumnInfo{Name: rows[0].String(), Type: rows[1].String()}
	}
	return columns, nil
}
