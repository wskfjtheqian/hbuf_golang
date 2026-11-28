package hcdc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const shadowRouteTable = "cdc_read_routes"

var (
	// ErrShadowRouteRevisionChanged 表示已核对的路由修订号已被其他发布操作覆盖。
	// 调用方应读取当前修订号并重新核对。
	ErrShadowRouteRevisionChanged = herror.NewError("CDC read route revision changed since verification")
	// ErrShadowRoutePublicationUnknown 表示发布结果不确定，回读未能确认本次发布已提交。
	ErrShadowRoutePublicationUnknown = herror.NewError("CDC read route publication outcome is uncertain")
)

// SQLShadowRouteStore 使用 MySQL 或 MariaDB InnoDB 表作为共享路由注册表。
// 调用方负责连接权威元数据库、提供数据库和连接池，并配置持久化提交。
type SQLShadowRouteStore struct {
	db       *sql.DB
	database string
	table    string
}

// NewSQLShadowRouteStore 将 SQL 连接池绑定到现有的控制数据库。
// 创建句柄时无需执行 SQL，支持只读账号。
func NewSQLShadowRouteStore(db *sql.DB, database string) (*SQLShadowRouteStore, error) {
	if db == nil || !validPipelineIdentifier(database) {
		return nil, herror.NewError("CDC read route store requires a SQL pool and valid control database name")
	}
	return &SQLShadowRouteStore{
		db: db, database: database, table: quoteDoris(database) + "." + quoteDoris(shadowRouteTable),
	}, nil
}

// Initialize 在空注册表中初始化整组路由。
// 重复调用会核对整组配置并保留既有修订号。
// 注册表缺少记录时需要修复，读取路由与正常重启始终使用已存记录。
func (s *SQLShadowRouteStore) Initialize(ctx context.Context, configs []ShadowRoute) error {
	router, err := NewShadowRouter(configs)
	if err != nil {
		return err
	}
	query := "CREATE TABLE IF NOT EXISTS " + s.table + " (" +
		"source_key BINARY(32) NOT NULL PRIMARY KEY," +
		"config_digest BINARY(32) NOT NULL," +
		"revision BIGINT UNSIGNED NOT NULL," +
		"route_json MEDIUMBLOB NOT NULL," +
		"publication_token BINARY(16) NOT NULL" +
		") ENGINE=InnoDB"
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return herror.WrapMsg(err, "create CDC read route registry")
	}
	if err := s.verifyEngine(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return herror.WrapMsg(err, "begin CDC read route initialization")
	}
	defer tx.Rollback()
	sourceIDs := make([]string, 0, len(router.routes))
	for sourceID := range router.routes {
		sourceIDs = append(sourceIDs, sourceID)
	}
	slices.Sort(sourceIDs)
	configs = make([]ShadowRoute, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		configs = append(configs, router.routes[sourceID].config)
	}
	manifest, err := json.Marshal(configs)
	if err != nil {
		return herror.WrapMsg(err, "encode CDC read route registry bindings")
	}
	manifestDigest := sha256.Sum256(manifest)
	seed, err := s.beginInitialization(ctx, tx, manifestDigest, len(sourceIDs))
	if err != nil {
		return err
	}
	insert := "INSERT INTO " + s.table +
		" (source_key, config_digest, revision, route_json, publication_token) VALUES (?, ?, 1, ?, ?)"
	for _, sourceID := range sourceIDs {
		state := router.routes[sourceID]
		key, configDigest, err := shadowRouteKeys(state.config)
		if err != nil {
			return err
		}
		if seed {
			data, err := json.Marshal(state.record.shadowRoutePublication)
			if err != nil {
				return herror.WrapMsg(err, "encode initial CDC read route")
			}
			var token [16]byte
			if _, err := tx.ExecContext(ctx, insert, key[:], configDigest[:], data, token[:]); err != nil {
				return herror.WrapMsg(err, "initialize CDC read route "+sourceID)
			}
		}
		if _, err := s.read(ctx, tx, state.config, key, configDigest); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return herror.WrapMsg(err, "commit CDC read route initialization")
	}
	return nil
}

// beginInitialization 通过注册表保留行协调初始化，并核对现有路由组的绑定摘要和记录数。
func (s *SQLShadowRouteStore) beginInitialization(ctx context.Context, tx *sql.Tx, binding [32]byte, routeCount int) (bool, error) {
	// 保留行使初始化操作串行执行，并在与初始路由相同的事务提交后可见。
	// 日常发布路由只锁定对应的路由记录。
	var key [32]byte
	var token [16]byte
	insert := "INSERT INTO " + s.table +
		" (source_key, config_digest, revision, route_json, publication_token) VALUES (?, ?, 0, ?, ?)" +
		" ON DUPLICATE KEY UPDATE source_key = source_key"
	if _, err := tx.ExecContext(ctx, insert, key[:], binding[:], []byte("initializing"), token[:]); err != nil {
		return false, herror.WrapMsg(err, "reserve CDC read route initialization")
	}
	var storedBinding []byte
	var state string
	query := "SELECT config_digest, route_json FROM " + s.table + " WHERE source_key = ? AND revision = 0 FOR UPDATE"
	if err := tx.QueryRowContext(ctx, query, key[:]).Scan(&storedBinding, &state); err != nil {
		return false, herror.WrapMsg(err, "read CDC route registry initialization binding")
	}
	if !bytes.Equal(storedBinding, binding[:]) {
		return false, herror.NewError("CDC read route registry bindings differ from initialization")
	}
	var count int
	query = "SELECT COUNT(*) FROM " + s.table + " WHERE source_key <> ?"
	if err := tx.QueryRowContext(ctx, query, key[:]).Scan(&count); err != nil {
		return false, herror.WrapMsg(err, "count CDC read route registry rows")
	}
	switch {
	case state == "initialized" && count == routeCount:
		return false, nil
	case state == "initializing" && count == 0:
		query = "UPDATE " + s.table + " SET route_json = ? WHERE source_key = ?"
		if _, err := tx.ExecContext(ctx, query, []byte("initialized"), key[:]); err != nil {
			return false, herror.WrapMsg(err, "mark CDC read route registry initialized")
		}
		return true, nil
	default:
		return false, herror.NewError("CDC read route registry is incomplete or contains unbound routes")
	}
}

// verifyEngine 校验路由注册表使用支持事务的 InnoDB 引擎。
func (s *SQLShadowRouteStore) verifyEngine(ctx context.Context) error {
	var engine string
	err := s.db.QueryRowContext(ctx, "SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		s.database, shadowRouteTable).Scan(&engine)
	if err != nil {
		return herror.WrapMsg(err, "verify CDC read route registry storage engine")
	}
	if !strings.EqualFold(engine, "InnoDB") {
		return herror.NewError("CDC read route registry requires InnoDB")
	}
	return nil
}

// shadowRouteKeys 从源 ID 和原始配置分别计算路由主键及配置摘要。
func shadowRouteKeys(config ShadowRoute) ([32]byte, [32]byte, error) {
	if !utf8.ValidString(config.SourceID) || !utf8.ValidString(config.SourceIdentity) {
		return [32]byte{}, [32]byte{}, herror.NewError("CDC read route source identifiers must be valid UTF-8")
	}
	data, err := json.Marshal(config)
	if err != nil {
		return [32]byte{}, [32]byte{}, herror.WrapMsg(err, "encode CDC read route binding")
	}
	key := sha256.Sum256([]byte(config.SourceID))
	if key == ([32]byte{}) {
		return [32]byte{}, [32]byte{}, herror.NewError("CDC read route source key is reserved")
	}
	return key, sha256.Sum256(data), nil
}

// shadowRouteQueryer 统一连接池和事务中的路由查询入口。
type shadowRouteQueryer interface {
	// QueryRowContext 在指定上下文中查询一条路由记录或相关元数据。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// shadowRoutePublication 保存已发布的路由及物理成员的重新绑定信息。
// 注册表配置摘要仍绑定包含初始源和目标的原始配置。
type shadowRoutePublication struct {
	ReadRoute
	ReboundSourceIdentity string `json:",omitempty"`
	MigrationPending      bool   `json:",omitempty"`
	LastCanceledRevision  uint64 `json:",omitempty"`
}

// sourceIdentity 返回已发布路由绑定的物理源标识，未迁移时使用注册配置中的标识。
func (p shadowRoutePublication) sourceIdentity(config ShadowRoute) string {
	if p.ReboundSourceIdentity != "" {
		return p.ReboundSourceIdentity
	}
	return config.SourceIdentity
}

type shadowRouteRecord struct {
	shadowRoutePublication
	data  []byte
	token []byte
}

// read 读回路由记录并核对配置摘要、修订号、发布令牌和路由内容。
func (s *SQLShadowRouteStore) read(ctx context.Context, queryer shadowRouteQueryer, config ShadowRoute, key, configDigest [32]byte) (shadowRouteRecord, error) {
	var result shadowRouteRecord
	var storedDigest []byte
	var revision uint64
	query := "SELECT config_digest, revision, route_json, publication_token FROM " + s.table + " WHERE source_key = ?"
	err := queryer.QueryRowContext(ctx, query, key[:]).Scan(&storedDigest, &revision, &result.data, &result.token)
	if errors.Is(err, sql.ErrNoRows) {
		return shadowRouteRecord{}, herror.NewError("CDC read route is not provisioned for source " + config.SourceID)
	}
	if err != nil {
		return shadowRouteRecord{}, herror.WrapMsg(err, "read published CDC route "+config.SourceID)
	}
	if !bytes.Equal(storedDigest, configDigest[:]) {
		return shadowRouteRecord{}, herror.NewError("CDC read route configuration differs from the persisted binding for source " + config.SourceID)
	}
	if err := json.Unmarshal(result.data, &result.shadowRoutePublication); err != nil {
		return shadowRouteRecord{}, herror.WrapMsg(err, "decode published CDC read route")
	}
	if len(result.token) != 16 || revision != result.Revision {
		return shadowRouteRecord{}, herror.NewError("CDC read route revision or publication identity is invalid")
	}
	if err := validateShadowRoutePublication(config, result.shadowRoutePublication); err != nil {
		return shadowRouteRecord{}, err
	}
	return result, nil
}

// validateShadowRoutePublication 校验发布记录的迁移状态、物理成员归属及已提交的读取目标。
func validateShadowRoutePublication(config ShadowRoute, publication shadowRoutePublication) error {
	if publication.LastCanceledRevision != 0 && publication.LastCanceledRevision >= publication.Revision {
		return herror.NewError("published CDC read route has an invalid canceled migration revision")
	}
	if publication.ReboundSourceIdentity != "" {
		watermark := publication.Watermark
		_, _, set, err := parseCursor(watermark)
		if err != nil {
			return err
		}
		if err := validateGTIDCursor(watermark, set); err != nil {
			return err
		}
		originalTopology, _, err := shadowMigrationMember(Cursor{SourceIdentity: config.SourceIdentity, Flavor: watermark.Flavor})
		if err != nil {
			return err
		}
		activeTopology, _, err := shadowMigrationMember(watermark)
		if err != nil {
			return err
		}
		if originalTopology != activeTopology {
			return herror.NewError("published CDC read route member is outside the bound topology")
		}
		config.SourceIdentity = publication.ReboundSourceIdentity
	}
	return validatePublishedShadowRoute(config, publication.ReadRoute)
}

// validatePublishedShadowRoute 核对已发布路由的读取目标、修订号、切换时间和核对位点。
func validatePublishedShadowRoute(config ShadowRoute, route ReadRoute) error {
	database := config.LegacyDatabase
	if route.UsingShadow {
		database = config.ShadowDatabase
	}
	if route.SourceID != config.SourceID || route.Database != database || route.Revision == 0 {
		return herror.NewError("published CDC read route does not match its source, database or revision")
	}
	if route.Revision == 1 {
		if route.UsingShadow || !route.SwitchedAt.IsZero() || route.Watermark != (Cursor{}) {
			return herror.NewError("initial CDC read route has invalid publication metadata")
		}
		return nil
	}
	if route.SwitchedAt.IsZero() || route.Watermark.SourceIdentity != config.SourceIdentity {
		return herror.NewError("published CDC read route has invalid switch time or watermark identity")
	}
	return validateCursor(route.Watermark)
}

// load 查询当前源已发布的读取路由。
func (s *SQLShadowRouteStore) load(ctx context.Context, config ShadowRoute) (ReadRoute, error) {
	record, err := s.loadRecord(ctx, config)
	return record.ReadRoute, err
}

// loadRecord 查询当前源已发布的路由，并附带供比较交换使用的记录。
func (s *SQLShadowRouteStore) loadRecord(ctx context.Context, config ShadowRoute) (shadowRouteRecord, error) {
	key, configDigest, err := shadowRouteKeys(config)
	if err != nil {
		return shadowRouteRecord{}, err
	}
	return s.read(ctx, s.db, config, key, configDigest)
}

// compareAndSwap 依据原修订号和发布令牌提交路由，提交结果不明时回读确认。
func (s *SQLShadowRouteStore) compareAndSwap(ctx context.Context, config ShadowRoute, previous shadowRouteRecord, next shadowRoutePublication) error {
	if err := validateShadowRoutePublication(config, next); err != nil {
		return err
	}
	key, configDigest, err := shadowRouteKeys(config)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return herror.WrapMsg(err, "encode CDC read route publication")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return herror.WrapMsg(err, "create CDC read route publication identity")
	}
	// 显式转换为无符号整数，避免超出驱动有符号整数范围的修订号从十进制字符串转换时被转为浮点数。
	// 发布令牌还用于校验公开修订号保持不变的状态更新。
	query := "UPDATE " + s.table + " SET revision = CAST(? AS UNSIGNED), route_json = ?, publication_token = ?" +
		" WHERE source_key = ? AND config_digest = ? AND revision = CAST(? AS UNSIGNED) AND publication_token = ?"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return herror.WrapMsg(err, "begin CDC read route publication")
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query, strconv.FormatUint(next.Revision, 10), payload, token[:],
		key[:], configDigest[:], strconv.FormatUint(previous.Revision, 10), previous.token)
	if err != nil {
		return herror.WrapMsg(err, "update CDC read route revision")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return herror.WrapMsg(err, "read CDC route publication result")
	}
	if count == 0 {
		return ErrShadowRouteRevisionChanged
	}
	if count != 1 {
		return herror.NewError("CDC read route publication affected more than one row")
	}
	commitErr := tx.Commit()
	if commitErr == nil {
		return nil
	}
	actual, readErr := s.read(ctx, s.db, config, key, configDigest)
	if readErr == nil && bytes.Equal(actual.data, payload) && bytes.Equal(actual.token, token[:]) {
		return nil
	}
	return errors.Join(ErrShadowRoutePublicationUnknown, commitErr, readErr)
}
