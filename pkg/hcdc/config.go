package hcdc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// Config 描述管理器期望运行的全部同步源。
type Config struct {
	Doris  *CanalDorisConfig `yaml:"Doris"`  // 所有源共用的 Doris 配置。
	Canals []CanalConfig     `yaml:"Canals"` // 按库表规则选取同步源。
	// AutoDiscoverInterval 指定探测新增源库和源表的间隔，零值使用默认间隔。
	AutoDiscoverInterval time.Duration `yaml:"AutoDiscoverInterval,omitempty"`
}

// DefaultAutoDiscoverInterval 是默认的源库和源表探测间隔。
const DefaultAutoDiscoverInterval = 30 * time.Second

// ZeroDatePolicy 指定遇到 MySQL 或 MariaDB 零日期时的处理方式。
type ZeroDatePolicy string

const (
	// ZeroDateReject 阻塞含零日期的记录，是省略配置时的默认值。
	ZeroDateReject ZeroDatePolicy = "reject"
	// ZeroDateEpoch 沿用 hcdc，将零日期映射为 1970-01-01 并保留时间部分。
	ZeroDateEpoch ZeroDatePolicy = "epoch"
)

// CanalConfig 描述一个兼容旧版 YAML 的源实例。在同一实例上按源库划分流水线。
type CanalConfig struct {
	InstanceId     int64          `yaml:"InstanceId"`     // 实例 ID，在所有 Canal 中唯一且稳定。
	Host           string         `yaml:"Host"`           // 源数据库 TCP 地址，格式为主机:端口。
	Username       string         `yaml:"Username"`       // 源数据库用户名。
	Password       string         `yaml:"Password"`       // 源数据库密码。
	Schema         string         `yaml:"Schema"`         // 表发现连接的默认数据库。
	IncludeDBs     []string       `yaml:"IncludeDBs"`     // 源数据库名的允许正则列表。
	ExcludeDBs     []string       `yaml:"ExcludeDBs"`     // 源数据库名的排除正则列表，优先于允许列表。
	IncludeTables  []string       `yaml:"IncludeTables"`  // 裸表名的允许正则列表。
	ExcludeTables  []string       `yaml:"ExcludeTables"`  // 裸表名的排除正则列表，优先于允许列表。
	ServerID       *uint32        `yaml:"ServerID"`       // 默认数据库使用的复制客户端 ID，也是其他库派生 ID 的种子。
	Charset        string         `yaml:"Charset"`        // 表发现连接的字符集，默认 utf8mb4。
	Flavor         string         `yaml:"Flavor"`         // mysql 或 mariadb，默认 mysql。
	LogDir         string         `yaml:"LogDir"`         // 持久缓冲的根目录，默认 hcdc-data。
	Generation     string         `yaml:"Generation"`     // 生成的流水线代次，默认 1。
	ZeroDatePolicy ZeroDatePolicy `yaml:"ZeroDatePolicy"` // 零日期处理规则，默认 reject。
	// GTIDAutoPosition 启用 GTID 续传，切主时保持 InstanceId 和代次不变。
	GTIDAutoPosition bool `yaml:"GTIDAutoPosition"`
}

// CanalDorisConfig 描述所有 Canal 共用的 Doris 连接和库名映射。
type CanalDorisConfig struct {
	Host        string `yaml:"Host"`        // Doris SQL TCP 地址，格式为主机:端口。
	Username    string `yaml:"Username"`    // Doris SQL 和 HTTP 用户名。
	Password    string `yaml:"Password"`    // Doris SQL 和 HTTP 密码。
	Schema      string `yaml:"Schema"`      // 兼容旧版配置的默认数据库。
	LoadURL     string `yaml:"LoadURL"`     // Doris BE 的 Stream Load 地址。
	FrontendURL string `yaml:"FrontendURL"` // Doris FE 的导入确认地址。常用端口可由 LoadURL 推导。
	// SchemaPrefix 拼在源库名前，可用 {InstanceId} 区分不同源实例的同名库。
	SchemaPrefix string `yaml:"SchemaPrefix"`
}

// ResolvedSource 表示从共享 Doris 和 Canal 配置解析的一个同步源。
type ResolvedSource struct {
	config   pipelineConfig
	redactor *strings.Replacer
}

// Identity 返回该源的稳定 ID 和同步代次。
func (s ResolvedSource) Identity() StreamIdentity { return s.config.Identity }

// TargetDatabase 返回按 SchemaPrefix 生成的 Doris 库名。
func (s ResolvedSource) TargetDatabase() string { return s.config.TargetDatabase }

// RedactMessage 遮盖状态消息中可能出现的源库和 Doris 连接凭据。
func (s ResolvedSource) RedactMessage(message string) string {
	if s.redactor == nil {
		return message
	}
	return s.redactor.Replace(message)
}

// Validate 静态校验配置。Canal 的实际选表由 Resolve 完成。
func (c *Config) Validate(ctx context.Context) bool {
	if c == nil {
		hlog.Error(ctx, "CDC 配置为空")
		return false
	}
	if _, _, err := prepareCanalConfig(c); err != nil {
		hlog.Error(ctx, "CDC 配置校验失败: %v", err)
		return false
	}
	return true
}

// Equal 比较共享 Doris 与各 Canal 的配置，两个 nil 相等。
func (c *Config) Equal(other *Config) bool {
	if c == nil || other == nil {
		return c == other
	}
	return discoverInterval(c.AutoDiscoverInterval) == discoverInterval(other.AutoDiscoverInterval) &&
		reflect.DeepEqual(c.Doris, other.Doris) && (c.Canals == nil) == (other.Canals == nil) &&
		slices.EqualFunc(c.Canals, other.Canals, equalCanalConfig)
}

// discoverInterval 把省略的间隔归一化为默认值。
func discoverInterval(interval time.Duration) time.Duration {
	if interval == 0 {
		return DefaultAutoDiscoverInterval
	}
	return interval
}

const (
	canalPipelineID         = "hcdc-canal"
	defaultCanalGeneration  = "1"
	defaultCanalLogDir      = "hcdc-data"
	defaultCanalBacklogWarn = 10 << 30
	canalDiscoveryTimeout   = 15 * time.Second
	canalInstanceToken      = "{InstanceId}"
)

type canalSelection struct {
	config        CanalConfig
	databases     canalNameFilter
	tables        canalNameFilter
	tableInterval time.Duration
}

type canalNameFilter struct {
	include []*regexp.Regexp
	exclude []*regexp.Regexp
}

var (
	canalGenerationName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	canalCharsets       = regexp.MustCompile(`^[A-Za-z0-9_]+(,[A-Za-z0-9_]+)*$`)
)

// equalCanalConfig 比较 Canal 声明，并将省略规则和空规则视为等价。
func equalCanalConfig(a, b CanalConfig) bool {
	if !slices.Equal(a.IncludeDBs, b.IncludeDBs) || !slices.Equal(a.ExcludeDBs, b.ExcludeDBs) ||
		!slices.Equal(a.IncludeTables, b.IncludeTables) || !slices.Equal(a.ExcludeTables, b.ExcludeTables) {
		return false
	}
	a.IncludeDBs, a.ExcludeDBs, a.IncludeTables, a.ExcludeTables = nil, nil, nil, nil
	b.IncludeDBs, b.ExcludeDBs, b.IncludeTables, b.ExcludeTables = nil, nil, nil, nil
	return reflect.DeepEqual(a, b)
}

// Resolve 发现源库和表，返回可交给 Manager 启动的源清单。发现失败时不返回部分结果。
func (c *Config) Resolve(ctx context.Context) ([]ResolvedSource, error) {
	pipelines, err := c.resolvePipelines(ctx)
	if err != nil {
		return nil, err
	}
	sources := make([]ResolvedSource, 0, len(pipelines))
	for _, pipeline := range pipelines {
		sources = append(sources, newResolvedSource(pipeline))
	}
	return sources, nil
}

// newResolvedSource 为已解析的流水线配置准备状态消息脱敏器。
func newResolvedSource(pipeline pipelineConfig) ResolvedSource {
	secrets := []string{pipeline.Source.Password, pipeline.Doris.Password, pipeline.Doris.SQLDSN}
	slices.SortStableFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	var replacements []string
	for _, secret := range secrets {
		if secret != "" {
			replacements = append(replacements, secret, "[redacted]")
		}
	}
	return ResolvedSource{config: pipeline, redactor: strings.NewReplacer(replacements...)}
}

// resolvePipelines 生成当前选表结果，并在应用前校验目标库和持久目录所有权。
func (c *Config) resolvePipelines(ctx context.Context) ([]pipelineConfig, error) {
	doris, selections, err := prepareCanalConfig(c)
	if err != nil {
		return nil, err
	}
	var resolved []pipelineConfig
	for _, selection := range selections {
		catalog, err := selection.discover(ctx)
		if err != nil {
			return nil, err
		}
		pipelines, err := selection.pipelines(catalog, doris, c.Doris.SchemaPrefix)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, pipelines...)
	}
	prepared, err := prepareDesiredConfig(resolved)
	if err != nil {
		return nil, err
	}
	for i, pipeline := range resolved {
		resolved[i] = prepared[pipeline.Identity.SourceID].config
	}
	return resolved, nil
}

// prepareCanalConfig 校验共享 Doris 配置和源身份，并预编译选表条件。
func prepareCanalConfig(config *Config) (DorisConfig, []canalSelection, error) {
	if config == nil {
		return DorisConfig{}, nil, herror.NewError("CDC 配置为空")
	}
	if config.Doris == nil || config.Canals == nil {
		return DorisConfig{}, nil, herror.NewError("CDC 配置需要 Doris 和 Canals，可用 Canals: [] 清空源清单")
	}
	if config.AutoDiscoverInterval < 0 {
		return DorisConfig{}, nil, herror.NewError("CDC AutoDiscoverInterval 不能为负数")
	}
	doris, err := config.Doris.pipelineDoris()
	if err != nil {
		return DorisConfig{}, nil, err
	}
	seenIDs := make(map[int64]struct{}, len(config.Canals))
	selections := make([]canalSelection, 0, len(config.Canals))
	for index, source := range config.Canals {
		if _, exists := seenIDs[source.InstanceId]; exists {
			return DorisConfig{}, nil, herror.NewError(fmt.Sprintf("Canals[%d].InstanceId 重复", index))
		}
		seenIDs[source.InstanceId] = struct{}{}
		selection, err := prepareCanalSelection(source)
		if err != nil {
			return DorisConfig{}, nil, herror.Wrap(fmt.Errorf("Canals[%d]: %w", index, err))
		}
		selection.tableInterval = discoverInterval(config.AutoDiscoverInterval)
		selections = append(selections, selection)
	}
	return doris, selections, nil
}

// prepareCanalSelection 校验并补齐单个源的流水线配置和发现规则。
func prepareCanalSelection(config CanalConfig) (canalSelection, error) {
	if config.InstanceId <= 0 || strings.TrimSpace(config.Username) == "" {
		return canalSelection{}, herror.NewError("Canal 需要正数 InstanceId 和非空 Username")
	}
	if _, _, err := mysqlsource.ParseAddress(config.Host); err != nil {
		return canalSelection{}, herror.NewError("Canal Host 必须是有效的主机:端口")
	}
	if config.ServerID != nil && *config.ServerID == 0 {
		return canalSelection{}, herror.NewError("Canal ServerID 必须大于零")
	}
	if config.Charset == "" {
		config.Charset = "utf8mb4"
	}
	if !canalCharsets.MatchString(config.Charset) {
		return canalSelection{}, herror.NewError("Canal Charset 必须是有效的字符集名称或逗号分隔列表")
	}
	if config.Flavor == "" {
		config.Flavor = "mysql"
	}
	if config.Flavor != "mysql" && config.Flavor != "mariadb" {
		return canalSelection{}, herror.NewError("Canal Flavor 必须是 mysql 或 mariadb")
	}
	if config.LogDir == "" {
		config.LogDir = defaultCanalLogDir
	}
	if strings.TrimSpace(config.LogDir) == "" {
		return canalSelection{}, herror.NewError("Canal LogDir 必须是有效的持久目录")
	}
	if config.Generation == "" {
		config.Generation = defaultCanalGeneration
	}
	if !canalGenerationName.MatchString(config.Generation) {
		return canalSelection{}, herror.NewError("Canal Generation 必须以字母或数字开头，仅允许字母、数字、点、连字符和下划线")
	}
	if config.ZeroDatePolicy != "" && config.ZeroDatePolicy != ZeroDateReject && config.ZeroDatePolicy != ZeroDateEpoch {
		return canalSelection{}, herror.NewError("Canal ZeroDatePolicy 必须是 reject 或 epoch")
	}
	databases, err := compileCanalFilter(config.IncludeDBs, config.ExcludeDBs, "IncludeDBs", "ExcludeDBs")
	if err != nil {
		return canalSelection{}, err
	}
	tables, err := compileCanalFilter(config.IncludeTables, config.ExcludeTables, "IncludeTables", "ExcludeTables")
	if err != nil {
		return canalSelection{}, err
	}
	return canalSelection{config: config, databases: databases, tables: tables}, nil
}

// compileCanalFilter 编译配置的正则，诊断中只保留字段和下标。
func compileCanalFilter(include, exclude []string, includeField, excludeField string) (canalNameFilter, error) {
	var filter canalNameFilter
	for _, list := range []struct {
		field    string
		patterns []string
		compiled *[]*regexp.Regexp
	}{{includeField, include, &filter.include}, {excludeField, exclude, &filter.exclude}} {
		for index, pattern := range list.patterns {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				return canalNameFilter{}, herror.NewError(fmt.Sprintf("Canal %s[%d] 的正则表达式无效", list.field, index))
			}
			*list.compiled = append(*list.compiled, compiled)
		}
	}
	return filter, nil
}

// allows 优先匹配排除规则，允许列表为空时接受其余名称。
func (filter canalNameFilter) allows(name string) bool {
	for _, pattern := range filter.exclude {
		if pattern.MatchString(name) {
			return false
		}
	}
	for _, pattern := range filter.include {
		if pattern.MatchString(name) {
			return true
		}
	}
	return len(filter.include) == 0
}

// pipelineDoris 转换 SQL 与 HTTP 连接，并核验共享端点配置。
func (c CanalDorisConfig) pipelineDoris() (DorisConfig, error) {
	if strings.TrimSpace(c.Username) == "" {
		return DorisConfig{}, herror.NewError("Doris Username 不能为空")
	}
	if _, _, err := mysqlsource.ParseAddress(c.Host); err != nil {
		return DorisConfig{}, herror.NewError("Doris Host 必须是有效的主机:端口")
	}
	if c.Schema != "" && !validPipelineIdentifier(c.Schema) {
		return DorisConfig{}, herror.NewError("Doris Schema 必须是有效的数据库名")
	}
	if c.SchemaPrefix != "" && !validPipelineIdentifier(expandCanalPrefix(c.SchemaPrefix, 1)+"a") {
		return DorisConfig{}, herror.NewError("Doris SchemaPrefix 必须生成合法的目标库名前缀，可用 {InstanceId} 代表源实例编号")
	}
	backend, err := parseDorisEndpoint(c.LoadURL)
	if err != nil {
		return DorisConfig{}, err
	}
	if backend.Hostname() == "" {
		return DorisConfig{}, herror.NewError("Doris LoadURL 需要主机名")
	}
	frontend := c.FrontendURL
	if frontend == "" {
		var port string
		switch backend.Port() {
		case "8040":
			port = "8030"
		case "8140":
			port = "8130"
		default:
			return DorisConfig{}, herror.NewError("Doris 需要显式配置 FrontendURL，当前 LoadURL 端口无法推导")
		}
		frontend = backend.Scheme + "://" + net.JoinHostPort(backend.Hostname(), port)
	}
	dsn := mysql.NewConfig()
	dsn.Net, dsn.Addr, dsn.User, dsn.Passwd, dsn.DBName = "tcp", c.Host, c.Username, c.Password, c.Schema
	config := DorisConfig{
		SQLDSN: dsn.FormatDSN(), StreamLoadURL: c.LoadURL, FrontendURL: frontend,
		Username: c.Username, Password: c.Password,
	}
	if _, frontend, _, err := parseDorisConfig(config); err != nil {
		return DorisConfig{}, err
	} else if frontend.Hostname() == "" {
		return DorisConfig{}, herror.NewError("Doris FrontendURL 需要主机名")
	}
	return config, nil
}

// discover 只读查询源账号可见的基础表，按裸库名和裸表名筛选。
func (selection canalSelection) discover(ctx context.Context) (map[string][]string, error) {
	ctx, cancel := context.WithTimeout(ctx, canalDiscoveryTimeout)
	defer cancel()
	source := selection.config
	dsn := mysql.NewConfig()
	dsn.Net, dsn.Addr, dsn.User, dsn.Passwd, dsn.DBName = "tcp", source.Host, source.Username, source.Password, source.Schema
	dsn.Params = map[string]string{"charset": source.Charset}
	dsn.Timeout = 10 * time.Second
	connector, err := mysql.NewConnector(dsn)
	if err != nil {
		return nil, canalDiscoveryError(ctx, source.InstanceId, err)
	}
	database := sql.OpenDB(connector)
	defer database.Close()
	rows, err := database.QueryContext(ctx, `SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE = 'BASE TABLE'`)
	if err != nil {
		return nil, canalDiscoveryError(ctx, source.InstanceId, err)
	}
	defer rows.Close()
	catalog := make(map[string][]string)
	for rows.Next() {
		var schema, table string
		if err := rows.Scan(&schema, &table); err != nil {
			return nil, canalDiscoveryError(ctx, source.InstanceId, err)
		}
		if selection.selects(schema, table) {
			catalog[schema] = append(catalog[schema], table)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, canalDiscoveryError(ctx, source.InstanceId, err)
	}
	return catalog, nil
}

// selects 排除系统库，再应用库和表的正则选择条件。
func (selection canalSelection) selects(schema, table string) bool {
	switch strings.ToLower(schema) {
	case "information_schema", "performance_schema", "mysql", "sys":
		return false
	}
	return selection.databases.allows(schema) && selection.tables.allows(table)
}

// canalDiscoveryError 仅记录源实例、取消原因或服务器错误号，避免输出驱动连接详情。
func canalDiscoveryError(ctx context.Context, instance int64, err error) error {
	if cause := ctx.Err(); cause != nil {
		return herror.Wrap(fmt.Errorf("Canal %d 表发现失败: %w", instance, cause))
	}
	var server *mysql.MySQLError
	if errors.As(err, &server) {
		return herror.NewError(fmt.Sprintf("Canal %d 表发现失败，MySQL 错误号 %d", instance, server.Number))
	}
	return herror.NewError(fmt.Sprintf("Canal %d 表发现失败，请检查源端连接和权限", instance))
}

// expandCanalPrefix 将共享前缀中的实例占位符替换为稳定的实例编号。
func expandCanalPrefix(prefix string, instanceID int64) string {
	return strings.ReplaceAll(prefix, canalInstanceToken, strconv.FormatInt(instanceID, 10))
}

// pipelines 按源库生成稳定的流水线身份、目标库和持久目录。
func (selection canalSelection) pipelines(catalog map[string][]string, doris DorisConfig, prefix string) ([]pipelineConfig, error) {
	source := selection.config
	if len(catalog) == 0 {
		return nil, herror.NewError(fmt.Sprintf("Canal %d 未发现匹配的基础表，请检查选表条件、Schema 和权限", source.InstanceId))
	}
	libraries := make([]string, 0, len(catalog))
	for schema := range catalog {
		libraries = append(libraries, schema)
	}
	slices.Sort(libraries)
	serverIDs := make(map[uint32]struct{}, len(libraries))
	result := make([]pipelineConfig, 0, len(libraries))
	prefix = expandCanalPrefix(prefix, source.InstanceId)
	for _, schema := range libraries {
		pipeline := selection.schemaPipeline(schema, catalog[schema], doris, prefix)
		serverID := pipeline.Source.ServerID
		if _, exists := serverIDs[serverID]; exists {
			return nil, herror.NewError(fmt.Sprintf("Canal %d 存在复制客户端 ID 冲突，请为 Canal 调整 ServerID", source.InstanceId))
		}
		serverIDs[serverID] = struct{}{}
		result = append(result, pipeline)
	}
	return result, nil
}

// schemaSourceID 将实例编号和库名组合成稳定的源 ID。
func (selection canalSelection) schemaSourceID(schema string) string {
	return fmt.Sprintf("canal-%d-%s", selection.config.InstanceId, schema)
}

// schemaPipeline 为一个库生成带固定表清单的流水线配置。
func (selection canalSelection) schemaPipeline(schema string, catalogTables []string, doris DorisConfig, prefix string) pipelineConfig {
	source := selection.config
	id := selection.schemaSourceID(schema)
	tables := slices.Clone(catalogTables)
	slices.Sort(tables)
	return pipelineConfig{
		Identity:  StreamIdentity{PipelineID: canalPipelineID, SourceID: id, Generation: source.Generation},
		Directory: filepath.Join(source.LogDir, id, source.Generation),
		Source: mysqlConfig{
			Address: source.Host, User: source.Username, Password: source.Password, Flavor: source.Flavor,
			SourceIdentity: id, ServerID: source.replicationServerID(schema), Database: schema, Tables: tables,
			GTIDAutoPosition: source.GTIDAutoPosition,
		},
		Doris: doris, TargetDatabase: prefix + schema, ZeroDatePolicy: source.ZeroDatePolicy,
		BacklogWarningBytes: defaultCanalBacklogWarn,
		tableDiscovery:      &tableDiscoveryConfig{Interval: discoverInterval(selection.tableInterval), Filter: selection.tables},
	}
}

// replicationServerID 保留默认库的显式 ID，其他库从实例、种子和数据库名稳定派生。
func (c CanalConfig) replicationServerID(schema string) uint32 {
	var seed uint32
	if c.ServerID != nil {
		seed = *c.ServerID
		if schema == c.Schema {
			return seed
		}
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s/%d/%d/%s", canalPipelineID, c.InstanceId, seed, schema))
	id := binary.BigEndian.Uint32(sum[:4])
	if id == 0 {
		id = 1
	}
	if id == seed {
		id = id%(1<<32-1) + 1
	}
	return id
}

// equalPipelineConfig 判断已应用配置是否仍满足期望，自动发现的新表由运行中的流水线处理。
func equalPipelineConfig(current, desired pipelineConfig) bool {
	current, desired = withPipelineConfigDefaults(current), withPipelineConfigDefaults(desired)
	if current.Directory != desired.Directory {
		if strings.TrimSpace(current.Directory) == "" || strings.TrimSpace(desired.Directory) == "" {
			return false
		}
		var currentErr, desiredErr error
		current.Directory, currentErr = resolvePipelineDirectory(current.Directory)
		desired.Directory, desiredErr = resolvePipelineDirectory(desired.Directory)
		if currentErr != nil || desiredErr != nil {
			return false
		}
	}
	if !compatibleTableSelection(current, desired) ||
		(current.tableDiscovery != nil && current.tableDiscovery.Interval != desired.tableDiscovery.Interval) {
		return false
	}
	current.Source.Tables, desired.Source.Tables = nil, nil
	current.tableDiscovery, desired.tableDiscovery = nil, nil
	return reflect.DeepEqual(current, desired)
}

// tableFilterRules 保存同代次内固定的表名匹配规则。
type tableFilterRules struct {
	Include []string `json:",omitempty"`
	Exclude []string `json:",omitempty"`
}

// rules 规范化正则的顺序与重复项。匹配结果只取决于规则集合。
func (f canalNameFilter) rules() tableFilterRules {
	expressions := func(filters []*regexp.Regexp) []string {
		var values []string
		for _, filter := range filters {
			values = append(values, filter.String())
		}
		slices.Sort(values)
		return slices.Compact(values)
	}
	return tableFilterRules{Include: expressions(f.include), Exclude: expressions(f.exclude)}
}

// compatibleTableSelection 只允许同一组规则增加发现结果。移除订阅表仍要求新代次。
func compatibleTableSelection(current, desired pipelineConfig) bool {
	if current.tableDiscovery == nil || desired.tableDiscovery == nil {
		return current.tableDiscovery == desired.tableDiscovery &&
			slices.Equal(slices.Sorted(slices.Values(current.Source.Tables)), slices.Sorted(slices.Values(desired.Source.Tables)))
	}
	return reflect.DeepEqual(current.tableDiscovery.Filter.rules(), desired.tableDiscovery.Filter.rules()) &&
		containsSourceTables(desired.Source.Tables, current.Source.Tables)
}

// containsSourceTables 判断每个已选源表是否仍在发现结果中，保留库表的物理拼写。
func containsSourceTables(discovered, selected []string) bool {
	names := make(map[string]struct{}, len(discovered))
	for _, name := range discovered {
		names[name] = struct{}{}
	}
	for _, name := range selected {
		if _, ok := names[name]; !ok {
			return false
		}
	}
	return true
}

// withPipelineConfigDefaults 补齐用于配置比较和续传校验的默认值。
func withPipelineConfigDefaults(config pipelineConfig) pipelineConfig {
	if config.Source.Flavor == "" {
		config.Source.Flavor = "mysql"
	}
	if config.Doris.ControlDatabase == "" {
		config.Doris.ControlDatabase = defaultDorisControlDatabase
	}
	if config.Doris.ControlRetentionRecords == 0 {
		config.Doris.ControlRetentionRecords = defaultControlRetentionRecords
	}
	if config.Doris.DDLJobTimeout == 0 {
		config.Doris.DDLJobTimeout = defaultDorisDDLJobTimeout
	}
	if config.Doris.LoadRecheckTimeout == 0 {
		config.Doris.LoadRecheckTimeout = dorisLoadRecheckTimeout
	}
	if config.ZeroDatePolicy == "" {
		config.ZeroDatePolicy = ZeroDateReject
	}
	if config.MaxPendingBytes == 0 {
		config.MaxPendingBytes = defaultPendingBytes
	}
	if config.MaxTransactionBytes == 0 {
		config.MaxTransactionBytes = config.MaxPendingBytes
	}
	return config
}

// prepareDesiredConfig 校验完整的期望配置，并按源整理供应用阶段使用的配置。
func prepareDesiredConfig(pipelines []pipelineConfig) (map[string]managedConfig, error) {
	desired := make(map[string]managedConfig)
	for _, pipeline := range pipelines {
		id := pipeline.Identity.SourceID
		if _, duplicate := desired[id]; duplicate {
			return nil, herror.NewError("duplicate CDC source ID " + id)
		}
		next, err := prepareManagedConfig(pipeline)
		if err != nil {
			return nil, herror.Wrap(fmt.Errorf("validate CDC source %q: %w", id, err))
		}
		for _, earlier := range desired {
			if err := next.owner.conflict(earlier.owner); err != nil {
				return nil, herror.Wrap(fmt.Errorf("conflicting CDC sources %q and %q: %w", earlier.config.Identity.SourceID, id, err))
			}
			if next.config.Directory == earlier.config.Directory {
				return nil, herror.NewError("CDC sources " + earlier.config.Identity.SourceID + " and " + id + " share a buffer directory")
			}
		}
		desired[id] = next
	}
	return desired, nil
}
