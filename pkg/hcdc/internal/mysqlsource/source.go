// mysqlsource 包负责采集一致性快照和 binlog 中已提交的事务。
package mysqlsource

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	driver "github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// Config 描述一个 MySQL 或 MariaDB 源及其明确订阅的表清单。
type Config struct {
	Address          string     `yaml:"Address"`          // 源数据库的 TCP 地址
	User             string     `yaml:"User"`             // 采集账号
	Password         string     `yaml:"Password"`         // 采集账号密码
	Flavor           string     `yaml:"Flavor"`           // 源数据库类型，支持 mysql 和 mariadb
	SourceIdentity   string     `yaml:"SourceIdentity"`   // 逻辑源标识，用于校验恢复位置
	ServerID         uint32     `yaml:"ServerID"`         // binlog 复制客户端 ID
	Tables           []TableRef `yaml:"Tables"`           // 明确订阅的源表
	GTIDAutoPosition bool       `yaml:"GTIDAutoPosition"` // 是否按 GTID 自动定位
}

// TableRef 分别保存库名和表名，用于标识源表。
type TableRef struct {
	Schema string `yaml:"Schema"` // 数据库名称
	Name   string `yaml:"Name"`   // 表名
}

// Column 保存解码和映射源表物理列所需的 SQL 元数据。
type Column struct {
	Name       string
	DataType   string
	ColumnType string
	Nullable   bool
	Comment    string
	Charset    string
	// PlainDefinition 表示源列没有非空默认值或其他修饰项。旧版结构目录可能缺少这项信息。
	PlainDefinition bool `json:",omitempty"`
}

// Table 分别保存源表的物理列顺序和主键列顺序。
type Table struct {
	Ref            TableRef
	Columns        []Column
	PrimaryKey     []int
	DefaultCharset string
}

// Cursor 表示已验证源成员的 binlog 提交位置。
type Cursor struct {
	SourceIdentity string
	File           string
	Position       uint32
	Flavor         string
	GTIDSet        string
}

// Change 保存变更前后的完整行映像。变更前为空表示插入，变更后为空表示删除。
type Change struct {
	Table  TableRef
	Before []any
	After  []any
}

// Transaction 表示在 binlog 提交边界交付的事务。
type Transaction struct {
	ID      string
	Cursor  Cursor
	Changes []Change
	DDL     *DDLChange
	// RolledBack 表示此事务只推进源游标，采集端应丢弃预先暂存的行。
	RolledBack bool
}

// Source 管理采集流的数据库连接和固定的表清单。
type Source struct {
	db       *sql.DB
	config   Config
	host     string
	port     uint16
	identity string
}

// ParseAddress 校验源库的 TCP 地址，并返回主机名和端口。
func ParseAddress(address string) (host string, port uint16, err error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, herror.WrapMsg(err, "parse MySQL source address")
	}
	parsedPort, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return "", 0, herror.WrapMsg(err, "parse MySQL source port")
	}
	if host == "" || parsedPort == 0 {
		return "", 0, herror.NewError("MySQL source address requires a host and a nonzero port")
	}
	return host, uint16(parsedPort), nil
}

// Open 打开源数据库连接，验证表清单、服务器身份和 binlog 设置。
func Open(ctx context.Context, config Config) (*Source, error) {
	host, port, err := ParseAddress(config.Address)
	if err != nil {
		return nil, err
	}
	if config.User == "" || config.SourceIdentity == "" || config.ServerID == 0 || len(config.Tables) == 0 {
		return nil, herror.NewError("source requires user, identity, replication server ID, address and selected tables")
	}
	if config.Flavor == "" {
		config.Flavor = gomysql.MySQLFlavor
	}
	if config.Flavor != gomysql.MySQLFlavor && config.Flavor != gomysql.MariaDBFlavor {
		return nil, herror.NewError("source flavor must be mysql or mariadb")
	}
	selected := make(map[TableRef]struct{}, len(config.Tables))
	for _, table := range config.Tables {
		if table.Schema == "" || table.Name == "" || strings.ContainsRune(table.Schema+table.Name, '\x00') {
			return nil, herror.NewError("source table requires valid schema and table names")
		}
		if _, exists := selected[table]; exists {
			return nil, herror.NewError("duplicate table in source selection")
		}
		selected[table] = struct{}{}
	}
	config.Tables = append([]TableRef(nil), config.Tables...)

	dsn := driver.NewConfig()
	dsn.Net, dsn.Addr, dsn.User, dsn.Passwd = "tcp", config.Address, config.User, config.Password
	dsn.Timeout, dsn.Loc = 10*time.Second, time.UTC
	dsn.Params = map[string]string{"time_zone": "'+00:00'", "charset": "utf8mb4"}
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return nil, herror.WrapMsg(err, "open source database")
	}
	db.SetMaxOpenConns(4)
	source := &Source{db: db, config: config, host: host, port: port}
	id, err := source.preflight(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	source.identity = id
	return source, nil
}

// preflight 读取源库的 binlog 配置和版本，并确认服务器类型与配置相符。
func (s *Source) preflight(ctx context.Context) (string, error) {
	connection, err := s.db.Conn(ctx)
	if err != nil {
		return "", herror.WrapMsg(err, "obtain source preflight connection")
	}
	defer connection.Close()
	var logBin bool
	var format, image, version string
	err = connection.QueryRowContext(ctx, "SELECT @@GLOBAL.log_bin, @@GLOBAL.binlog_format, @@GLOBAL.binlog_row_image, VERSION()").
		Scan(&logBin, &format, &image, &version)
	if err != nil {
		return "", herror.WrapMsg(err, "verify source binary log configuration")
	}
	if !logBin || !strings.EqualFold(format, "ROW") || !strings.EqualFold(image, "FULL") {
		return "", herror.NewError("source requires enabled binary logs, ROW format, and FULL row images")
	}
	mariaDB := strings.Contains(strings.ToLower(version), "mariadb")
	if mariaDB != (s.config.Flavor == gomysql.MariaDBFlavor) {
		return "", herror.NewError("source flavor does not match the SQL server")
	}
	return s.readSourceIdentity(ctx, connection)
}

// identityQueryer 提供校验源成员标识所需的单行查询。
type identityQueryer interface {
	// QueryRowContext 在指定上下文中查询源库的一行元数据。
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// readSourceIdentity 根据服务器 UUID 或 MariaDB 的地址和服务器 ID 构建源成员身份。
func (s *Source) readSourceIdentity(ctx context.Context, queryer identityQueryer) (string, error) {
	if s.config.Flavor == gomysql.MariaDBFlavor {
		var serverID uint32
		if err := queryer.QueryRowContext(ctx, "SELECT @@GLOBAL.server_id").Scan(&serverID); err != nil {
			return "", herror.WrapMsg(err, "read MariaDB server ID")
		}
		if serverID == 0 || serverID == s.config.ServerID {
			return "", herror.NewError("MariaDB server ID must be distinct from the replication client")
		}
		return fmt.Sprintf("%s/%s/%d", s.config.SourceIdentity, s.config.Address, serverID), nil
	}
	var uuid string
	var serverID uint32
	if err := queryer.QueryRowContext(ctx, "SELECT @@GLOBAL.server_uuid, @@GLOBAL.server_id").Scan(&uuid, &serverID); err != nil {
		return "", herror.WrapMsg(err, "read MySQL server identity")
	}
	if uuid == "" || serverID == 0 || serverID == s.config.ServerID {
		return "", herror.NewError("MySQL server identity must be set and its server ID distinct from the replication client")
	}
	return s.config.SourceIdentity + "/" + uuid, nil
}

// Close 关闭源端适配器持有的 SQL 连接池。
func (s *Source) Close() error {
	if err := s.db.Close(); err != nil {
		return herror.WrapMsg(err, "close source database")
	}
	return nil
}

// quoteIdentifier 使用反引号引用 SQL 标识符，并转义标识符内的反引号。
func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// qualifiedTable 将库名和表名分别引用后拼接为完整 SQL 表名。
func qualifiedTable(table TableRef) string {
	return quoteIdentifier(table.Schema) + "." + quoteIdentifier(table.Name)
}
