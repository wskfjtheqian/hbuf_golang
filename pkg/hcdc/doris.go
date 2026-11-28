package hcdc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const defaultDorisControlDatabase = "cdc_control"

// DorisConfig 配置 Doris 的 SQL 和 HTTP 连接。StreamLoadURL 指向可访问的 BE，
// FrontendURL 指向用于查询 Label 状态的 FE，ControlDatabase 专供 CDC 元数据使用。
type DorisConfig struct {
	SQLDSN                  string        `yaml:"SQLDSN"`                  // Doris SQL 连接串。
	StreamLoadURL           string        `yaml:"StreamLoadURL"`           // 提供 Stream Load 的 Doris BE 地址。
	FrontendURL             string        `yaml:"FrontendURL"`             // 提供导入状态查询的 Doris FE 地址。
	Username                string        `yaml:"Username"`                // HTTP 用户名，留空时沿用 SQL 凭据。
	Password                string        `yaml:"Password"`                // HTTP 密码。
	ControlDatabase         string        `yaml:"ControlDatabase"`         // CDC 检查点、回执与结构历史所在库。
	HTTPTimeout             time.Duration `yaml:"HTTPTimeout"`             // HTTP 请求超时，默认 60 秒。
	LoadRecheckTimeout      time.Duration `yaml:"LoadRecheckTimeout"`      // 导入结果未明时，仅核对原 Label 的总期限，默认 15 秒。
	DDLJobTimeout           time.Duration `yaml:"DDLJobTimeout"`           // 已识别的异步 DDL 作业跟踪期限，默认 1 小时。
	ControlRetentionRecords uint64        `yaml:"ControlRetentionRecords"` // 保留最近的已确认记录数，默认 4096。
}

// DorisTarget 向 Doris UNIQUE KEY 表交付变更，并按流独立维护单调推进的检查点。
type DorisTarget struct {
	db             *sql.DB
	backend        *url.URL
	frontend       *url.URL
	client         *http.Client
	ownedTransport *http.Transport
	config         DorisConfig
}

var (
	errDorisTargetUnavailable = herror.NewError("Doris target temporarily unavailable")
	dorisIdentifier           = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)
	dorisLabel                = regexp.MustCompile(`^[a-zA-Z0-9_]{1,128}$`)
	dorisType                 = regexp.MustCompile(`^(BOOLEAN|TINYINT|SMALLINT|INT|BIGINT|LARGEINT|FLOAT|DOUBLE|DATE|DATEV2|DATETIME|DATETIMEV2|STRING|TEXT|JSON|JSONB|BITMAP|CHAR|VARCHAR|DECIMAL|DECIMALV3)(\([0-9]+(,[0-9]+)?\))?$`)
	dorisReplicaUnavailable   = regexp.MustCompile(`^errCode = 2, detailMessage = tablet [0-9]+ has no queryable replicas\. err: replica [0-9]+'s backend [0-9]+ does not exist or not alive$`)
)

var _ Target = (*DorisTarget)(nil)

// wrapDorisRequestError 将连接异常和 BE 启动故障标记为暂时不可用。
// 重试发送前须重新读取检查点和已登记的请求意图。
func wrapDorisRequestError(err error, message string) error {
	var operation *net.OpError
	var network net.Error
	var serverError *mysql.MySQLError
	replicaUnavailable := errors.As(err, &serverError) && serverError.Number == 1105 && dorisReplicaUnavailable.MatchString(serverError.Message)
	if errors.As(err, &operation) || (errors.As(err, &network) && network.Timeout()) ||
		errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || replicaUnavailable {
		err = errors.Join(errDorisTargetUnavailable, err)
	}
	return herror.WrapMsg(err, message)
}

// OpenDorisTarget 连接 Doris 并创建 CDC 控制表。
func OpenDorisTarget(ctx context.Context, config DorisConfig) (*DorisTarget, error) {
	return openDorisTarget(ctx, config, nil)
}

// openDorisTarget 在初始化控制表之前验证流水线是否拥有目标写入权。
func openDorisTarget(ctx context.Context, config DorisConfig, owner *dorisWriterIdentity) (*DorisTarget, error) {
	backend, frontend, dsn, err := parseDorisConfig(config)
	if err != nil {
		return nil, err
	}
	if config.ControlDatabase == "" {
		config.ControlDatabase = defaultDorisControlDatabase
	}
	if config.Username == "" {
		config.Username, config.Password = dsn.User, dsn.Passwd
	}
	dsn.InterpolateParams = true
	dsn.DBName = ""
	connector, err := mysql.NewConnector(dsn)
	if err != nil {
		return nil, herror.NewError("invalid Doris SQL connector settings")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if config.HTTPTimeout <= 0 {
		config.HTTPTimeout = 60 * time.Second
	}
	target := &DorisTarget{
		db: db, backend: backend, frontend: frontend, config: config,
	}
	if owner != nil {
		if err := target.claimWriter(ctx, *owner); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := target.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	target.client, target.ownedTransport = newDorisHTTPClient(config.HTTPTimeout, http.DefaultTransport)
	return target, nil
}

// parseDorisConfig 校验控制库、FE 和 BE 地址，并解析 Doris SQL DSN。
func parseDorisConfig(config DorisConfig) (backend, frontend *url.URL, dsn *mysql.Config, err error) {
	if config.LoadRecheckTimeout < 0 || config.LoadRecheckTimeout > maxDorisLoadRecheckTimeout {
		return nil, nil, nil, herror.NewError("Doris load recheck timeout must be between zero and ten minutes")
	}
	if config.DDLJobTimeout < 0 {
		return nil, nil, nil, herror.NewError("Doris DDL job timeout cannot be negative")
	}
	if config.ControlRetentionRecords > math.MaxInt64 {
		return nil, nil, nil, herror.NewError("Doris control retention exceeds the checkpoint sequence range")
	}
	if strings.TrimSpace(config.SQLDSN) == "" {
		return nil, nil, nil, herror.NewError("Doris SQL DSN is required")
	}
	if config.ControlDatabase == "" {
		config.ControlDatabase = defaultDorisControlDatabase
	}
	if !dorisIdentifier.MatchString(config.ControlDatabase) || strings.EqualFold(config.ControlDatabase, dorisWriterRegistry) {
		return nil, nil, nil, herror.NewError("invalid Doris control database name")
	}
	backend, err = parseDorisEndpoint(config.StreamLoadURL)
	if err != nil {
		return nil, nil, nil, err
	}
	frontend, err = parseDorisEndpoint(config.FrontendURL)
	if err != nil {
		return nil, nil, nil, err
	}
	dsn, err = mysql.ParseDSN(config.SQLDSN)
	if err != nil {
		return nil, nil, nil, herror.NewError("invalid Doris SQL DSN")
	}
	return backend, frontend, dsn, nil
}

// newDorisHTTPClient 创建带超时的 Doris HTTP 客户端，并记录自身持有的连接池。
func newDorisHTTPClient(timeout time.Duration, transport http.RoundTripper) (*http.Client, *http.Transport) {
	var ownedTransport *http.Transport
	if standard, ok := transport.(*http.Transport); ok && standard != nil {
		ownedTransport = standard.Clone()
		transport = ownedTransport
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// 禁止重定向，以免将 Doris 凭据转发到其他地址。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, ownedTransport
}

// parseDorisEndpoint 校验并解析用于访问 Doris 的 HTTP 或 HTTPS 地址。
func parseDorisEndpoint(address string) (*url.URL, error) {
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, herror.NewError("Doris endpoint must be an HTTP(S) URL without credentials, query or fragment")
	}
	return endpoint, nil
}

// initialize 创建保存检查点和导入回执所需的 Doris 控制库与控制表。
func (d *DorisTarget) initialize(ctx context.Context) error {
	control := quoteDoris(d.config.ControlDatabase)
	if _, err := d.db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+control); err != nil {
		return herror.WrapMsg(err, "create Doris CDC control database")
	}
	statements := []string{
		"CREATE TABLE IF NOT EXISTS " + control + ".`checkpoints` (" +
			"stream_key CHAR(64) NOT NULL, sequence_no BIGINT NOT NULL, checkpoint_json STRING NOT NULL) " +
			"ENGINE=OLAP UNIQUE KEY(stream_key, sequence_no) DISTRIBUTED BY HASH(stream_key) BUCKETS 1",
		"CREATE TABLE IF NOT EXISTS " + control + ".`load_receipts` (" +
			"load_label VARCHAR(128) NOT NULL, target_table VARCHAR(129) NOT NULL, payload_checksum CHAR(64) NOT NULL, " +
			"expected_rows BIGINT NOT NULL, verified BOOLEAN NOT NULL) " +
			"ENGINE=OLAP UNIQUE KEY(load_label) DISTRIBUTED BY HASH(load_label) BUCKETS 1",
		"CREATE TABLE IF NOT EXISTS " + control + ".`cdc_load_history` (" +
			"stream_key CHAR(64) NOT NULL, sequence_no BIGINT NOT NULL, load_label VARCHAR(128) NOT NULL) " +
			"ENGINE=OLAP UNIQUE KEY(stream_key, sequence_no, load_label) DISTRIBUTED BY HASH(stream_key) BUCKETS 1",
		"CREATE TABLE IF NOT EXISTS " + control + ".`load_recheck_deadlines` (" +
			"load_label VARCHAR(128) NOT NULL, deadline_ms BIGINT NOT NULL) " +
			"ENGINE=OLAP UNIQUE KEY(load_label) DISTRIBUTED BY HASH(load_label) BUCKETS 1",
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement+dorisUniqueProperties); err != nil {
			return herror.WrapMsg(err, "create Doris CDC control table")
		}
	}
	return nil
}

const dorisUniqueProperties = ` PROPERTIES ("replication_num" = "1", "enable_unique_key_merge_on_write" = "true")`

// quoteDoris 用反引号安全引用 Doris SQL 标识符。
func quoteDoris(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

// splitDorisTable 拆分并校验包含库名与表名的 Doris 目标名称。
func splitDorisTable(name string) (string, string, error) {
	database, table, ok := strings.Cut(name, ".")
	if !ok || !dorisIdentifier.MatchString(database) || !dorisIdentifier.MatchString(table) {
		return "", "", herror.NewError("Doris load requires a qualified database.table identifier")
	}
	return database, table, nil
}

// controlTable 返回 Doris 控制库下指定控制表的完整引用。
func (d *DorisTarget) controlTable(table string) string {
	return quoteDoris(d.config.ControlDatabase) + "." + quoteDoris(table)
}

// dorisStreamKey 对流的流水线标识、源标识和代次生成稳定的存储键。
func dorisStreamKey(identity StreamIdentity) string {
	key := fmt.Sprintf("%d:%s%d:%s%d:%s",
		len(identity.PipelineID), identity.PipelineID, len(identity.SourceID), identity.SourceID,
		len(identity.Generation), identity.Generation)
	return digest([]byte(key))
}

// Close 释放目标持有的 SQL 连接池和空闲 HTTP 连接。
func (d *DorisTarget) Close() error {
	if d.ownedTransport != nil {
		d.ownedTransport.CloseIdleConnections()
	}
	if err := d.db.Close(); err != nil {
		return herror.Wrap(err)
	}
	return nil
}

// LoadCheckpoint 读取指定流的最新已提交检查点。
func (d *DorisTarget) LoadCheckpoint(ctx context.Context, identity StreamIdentity) (*Checkpoint, error) {
	var sequence uint64
	var stored string
	query := "SELECT sequence_no, checkpoint_json FROM " + d.controlTable("checkpoints") +
		" WHERE stream_key = ? ORDER BY sequence_no DESC LIMIT 1"
	err := d.db.QueryRowContext(ctx, query, dorisStreamKey(identity)).Scan(&sequence, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapDorisRequestError(err, "load Doris checkpoint")
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal([]byte(stored), &checkpoint); err != nil {
		return nil, herror.WrapMsg(err, "decode Doris checkpoint")
	}
	if checkpoint.Identity != identity || checkpoint.Sequence != sequence {
		return nil, herror.NewError("Doris checkpoint identity or sequence mismatch")
	}
	return &checkpoint, nil
}

// PersistCheckpoint 校验检查点的推进顺序后写入新检查点。
func (d *DorisTarget) PersistCheckpoint(ctx context.Context, checkpoint Checkpoint) error {
	latest, err := d.LoadCheckpoint(ctx, checkpoint.Identity)
	if err != nil {
		return err
	}
	if latest != nil && *latest == checkpoint {
		return nil
	}
	if checkpoint.Sequence > math.MaxInt64 ||
		(latest == nil && checkpoint.Sequence != 0) ||
		(latest != nil && checkpoint.Sequence != latest.Sequence+1) {
		return herror.NewError("Doris checkpoint would break contiguous delivery")
	}
	stored, err := json.Marshal(checkpoint)
	if err != nil {
		return herror.WrapMsg(err, "encode Doris checkpoint")
	}
	_, err = d.db.ExecContext(ctx, "INSERT INTO "+d.controlTable("checkpoints")+" VALUES (?, ?, ?)",
		dorisStreamKey(checkpoint.Identity), checkpoint.Sequence, string(stored))
	if err != nil {
		return wrapDorisRequestError(err, "write Doris checkpoint")
	}
	return nil
}

// DorisColumn 描述支持的目标列。
type DorisColumn struct {
	Name     string
	Type     string
	Nullable bool
}

// DorisTable 描述 OLAP UNIQUE KEY 表。
type DorisTable struct {
	Database   string
	Name       string
	Columns    []DorisColumn
	PrimaryKey []string
}

// EnsureTable 为源端快照准备 Doris 数据库和 UNIQUE KEY 表。
// 按 Doris 的要求将键列排在其他列之前。
func (d *DorisTarget) EnsureTable(ctx context.Context, table DorisTable) error {
	if strings.EqualFold(table.Database, d.config.ControlDatabase) {
		return herror.NewError("Doris target database conflicts with the CDC control database")
	}
	if _, _, err := splitDorisTable(table.Database + "." + table.Name); err != nil {
		return err
	}
	definitions, keys, err := dorisTableDefinitions(table)
	if err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteDoris(table.Database)); err != nil {
		return herror.WrapMsg(err, "create Doris target database")
	}
	keyList := strings.Join(keys, ",")
	qualified := quoteDoris(table.Database) + "." + quoteDoris(table.Name)
	query := "CREATE TABLE IF NOT EXISTS " + qualified +
		" (" + strings.Join(definitions, ",") + ") ENGINE=OLAP UNIQUE KEY(" + keyList + ")" +
		" DISTRIBUTED BY HASH(" + keyList + ") BUCKETS 1" + dorisUniqueProperties
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return herror.WrapMsg(err, "create Doris target table")
	}
	if err := d.verifyDorisUniqueTable(ctx, qualified); err != nil {
		return err
	}
	difference, err := d.dorisSchemaDifference(ctx, table)
	if err != nil {
		return err
	}
	if difference != "" {
		return herror.NewError("Doris target table has a different schema " + qualified + ": " + difference)
	}
	return nil
}

// VerifyEmptyTable 校验目标表为空，以便安全写入新快照。
// 调用方须确保 CDC 是目标表的唯一写入者。
func (d *DorisTarget) VerifyEmptyTable(ctx context.Context, database, table string) error {
	if _, _, err := splitDorisTable(database + "." + table); err != nil {
		return err
	}
	var marker int
	err := d.db.QueryRowContext(ctx, "SELECT 1 FROM "+quoteDoris(database)+"."+quoteDoris(table)+" LIMIT 1").Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return herror.WrapMsg(err, "verify empty Doris target table")
	}
	return herror.NewError("Doris target table already has data; use a fresh snapshot target")
}

// dorisTableDefinitions 校验 Doris 表定义并按键列优先的顺序生成列定义。
func dorisTableDefinitions(table DorisTable) ([]string, []string, error) {
	if len(table.PrimaryKey) == 0 {
		return nil, nil, herror.NewError("Doris target requires a primary key")
	}
	columns := make(map[string]DorisColumn, len(table.Columns))
	for _, column := range table.Columns {
		name := strings.ToLower(column.Name)
		column.Type = strings.ToUpper(strings.ReplaceAll(column.Type, " ", ""))
		if _, exists := columns[name]; exists || !dorisIdentifier.MatchString(column.Name) || !dorisType.MatchString(column.Type) {
			return nil, nil, herror.NewError("Doris target has an unsupported or duplicate column: " + column.Name)
		}
		columns[name] = column
	}
	definitions := make([]string, 0, len(columns))
	keys := make([]string, 0, len(table.PrimaryKey))
	appendColumn := func(column DorisColumn) {
		definition := quoteDoris(column.Name) + " " + column.Type
		if !column.Nullable {
			definition += " NOT NULL"
		} else {
			definition += " NULL"
		}
		definitions = append(definitions, definition)
		delete(columns, strings.ToLower(column.Name))
	}
	for _, key := range table.PrimaryKey {
		column, exists := columns[strings.ToLower(key)]
		if !exists || column.Nullable || column.Type == "BITMAP" {
			return nil, nil, herror.NewError("Doris target has an invalid primary key column: " + key)
		}
		keys = append(keys, quoteDoris(column.Name))
		appendColumn(column)
	}
	for _, column := range table.Columns {
		if remaining, exists := columns[strings.ToLower(column.Name)]; exists {
			appendColumn(remaining)
		}
	}
	return definitions, keys, nil
}
