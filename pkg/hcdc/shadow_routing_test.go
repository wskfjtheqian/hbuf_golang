package hcdc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// TestShadowRouterSwitchAndRollbackArePerSource 验证切换和回退只作用于指定数据源，旧凭证不能用于新的修订号。
func TestShadowRouterSwitchAndRollbackArePerSource(t *testing.T) {
	mysqlRoute := ShadowRoute{
		SourceID: "mysql", SourceIdentity: "mysql-1", SourceDatabase: "mysql_src",
		LegacyDatabase: "mysql_legacy", ShadowDatabase: "mysql_shadow", Tables: routingTables("accounts"),
	}
	mariaRoute := ShadowRoute{
		SourceID: "mariadb", SourceIdentity: "mariadb-1", SourceDatabase: "mariadb_src",
		LegacyDatabase: "mariadb_legacy", ShadowDatabase: "mariadb_shadow", Tables: routingTables("accounts"),
	}
	router, err := NewShadowRouter([]ShadowRoute{mysqlRoute, mariaRoute})
	require.NoError(t, err)
	first, err := router.Route(mysqlRoute.SourceID)
	require.NoError(t, err)
	require.Equal(t, mysqlRoute.LegacyDatabase, first.Database)

	verification := routingComparison(t, mysqlRoute)
	watermark := routingWatermark(mysqlRoute, 125)
	proof, _, err := router.Verify(t.Context(), mysqlRoute.SourceID, watermark, []ShadowVerificationConfig{verification})
	require.NoError(t, err)
	cut, err := router.Cutover(t.Context(), proof)
	require.NoError(t, err)
	require.True(t, cut.UsingShadow)
	require.Equal(t, mysqlRoute.ShadowDatabase, cut.Database)
	require.EqualValues(t, 2, cut.Revision)
	require.Equal(t, watermark.Source, cut.Watermark)

	other, err := router.Route(mariaRoute.SourceID)
	require.NoError(t, err)
	require.Equal(t, mariaRoute.LegacyDatabase, other.Database)
	require.EqualValues(t, 1, other.Revision)

	_, err = router.Rollback(t.Context(), proof)
	require.Error(t, err, "a comparison cannot be replayed in a later revision")
	_, _, err = router.Verify(t.Context(), mysqlRoute.SourceID, routingWatermark(mysqlRoute, 124), []ShadowVerificationConfig{verification})
	require.Error(t, err, "the rollback must cover the prior verified cursor")
	newProof, _, err := router.Verify(t.Context(), mysqlRoute.SourceID, routingWatermark(mysqlRoute, 176), []ShadowVerificationConfig{verification})
	require.NoError(t, err)
	rolledBack, err := router.Rollback(t.Context(), newProof)
	require.NoError(t, err)
	require.False(t, rolledBack.UsingShadow)
	require.Equal(t, mysqlRoute.LegacyDatabase, rolledBack.Database)
	require.EqualValues(t, 3, rolledBack.Revision)
	otherAfterRollback, err := router.Route(mariaRoute.SourceID)
	require.NoError(t, err)
	require.Equal(t, other, otherAfterRollback)
}

// routingWatermark 构造源及两路写入使用同一位点的测试数据。
func routingWatermark(route ShadowRoute, position uint32) ShadowCutoverWatermark {
	cursor := Cursor{SourceIdentity: route.SourceIdentity, File: "mysql-bin.000001", Position: position, Flavor: route.SourceID}
	if route.SourceID != "mariadb" {
		cursor.Flavor = "mysql"
	}
	return ShadowCutoverWatermark{Source: cursor, LegacyDelivered: cursor, ShadowDelivered: cursor}
}

// routingComparison 为三个数据库构造包含同一条记录的核对配置。
func routingComparison(t *testing.T, route ShadowRoute) ShadowVerificationConfig {
	t.Helper()
	row := []driver.Value{int64(1), "row"}
	return ShadowVerificationConfig{
		Source:  openShadowTestTable(t, route.SourceDatabase, row),
		Legacy:  openShadowTestTable(t, route.LegacyDatabase, row),
		Shadow:  openShadowTestTable(t, route.ShadowDatabase, row),
		Columns: []string{"id", "note"}, KeyColumns: []string{"id"}, SampleLimit: 1,
	}
}

// routingTables 为指定表名生成测试用的列投影和主键配置。
func routingTables(names ...string) map[string]ShadowTableProjection {
	tables := make(map[string]ShadowTableProjection, len(names))
	for _, name := range names {
		tables[name] = ShadowTableProjection{Columns: []string{"id", "note"}, KeyColumns: []string{"id"}}
	}
	return tables
}

type shadowTestConnector struct {
	records [][]driver.Value
}

// Connect 创建读取预置测试记录的驱动连接。
func (c *shadowTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &shadowTestConnection{records: c.records}, nil
}

// Driver 返回空驱动引用，模拟连接由 Connect 创建。
func (c *shadowTestConnector) Driver() driver.Driver { return nil }

type shadowTestConnection struct {
	records [][]driver.Value
}

// Prepare 返回模拟读取连接无法预处理语句的错误。
func (c *shadowTestConnection) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepared statement unavailable in shadow fixture")
}

// Close 结束模拟读取连接的生命周期。
func (c *shadowTestConnection) Close() error { return nil }

// Begin 返回模拟读取连接无法开启事务的错误。
func (c *shadowTestConnection) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("transaction unavailable in shadow fixture")
}

// QueryContext 将预置记录作为影子表的查询结果返回。
func (c *shadowTestConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &shadowTestRows{records: c.records}, nil
}

type shadowTestRows struct {
	records [][]driver.Value
}

// Columns 返回模拟核对结果的投影列名。
func (r *shadowTestRows) Columns() []string { return []string{"id", "note"} }

// Close 结束模拟行迭代器的生命周期。
func (r *shadowTestRows) Close() error { return nil }

// Next 向驱动缓冲区复制下一条预置记录，耗尽时返回 EOF。
func (r *shadowTestRows) Next(dest []driver.Value) error {
	if len(r.records) == 0 {
		return io.EOF
	}
	copy(dest, r.records[0])
	r.records = r.records[1:]
	return nil
}

// ColumnTypeDatabaseTypeName 返回指定投影列在模拟数据库中的列类型。
func (r *shadowTestRows) ColumnTypeDatabaseTypeName(index int) string {
	if index == 0 {
		return "BIGINT"
	}
	return "VARCHAR"
}

// openShadowTestTable 创建可读取预置数据的影子表连接池，并注册测试清理。
func openShadowTestTable(t *testing.T, schema string, rows ...[]driver.Value) ShadowTable {
	t.Helper()
	pool := sql.OpenDB(&shadowTestConnector{records: rows})
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	return ShadowTable{DB: pool, Schema: schema, Table: "accounts"}
}

// TestPrepareShadowSourceMigrationKeepsReadersAndBlocksPublishers 验证迁移准备的幂等性，以及待完成期间读路由可用并阻断发布。
func TestPrepareShadowSourceMigrationKeepsReadersAndBlocksPublishers(t *testing.T) {
	ctx := t.Context()
	config := shadowStoreTestConfig()
	configs := []ShadowRoute{config}
	initial := ReadRoute{SourceID: config.SourceID, Database: config.LegacyDatabase, Revision: 1}
	_, digest, err := shadowRouteKeys(config)
	require.NoError(t, err)
	payload, err := json.Marshal(initial)
	require.NoError(t, err)
	token := make([]byte, 16)
	conn := &shadowStoreTestConn{query: func() []driver.Value {
		return []driver.Value{digest[:], int64(1), payload, token}
	}}
	conn.updateRoute = func(data, nextToken []byte) {
		payload, token = data, nextToken
	}
	store := openShadowRouteTestStore(t, conn)
	publisher, err := NewShadowRouter(configs)
	require.NoError(t, err)
	publisher.store = store
	reader, err := NewShadowRouteReader(store, configs)
	require.NoError(t, err)
	oldProof := ShadowCutoverProof{router: publisher, sourceID: config.SourceID, revision: 1}

	require.NoError(t, publisher.PrepareSourceMigration(ctx, config.SourceID, 1))
	require.NoError(t, publisher.PrepareSourceMigration(ctx, config.SourceID, 1))
	reopened, err := NewShadowRouter(configs)
	require.NoError(t, err)
	reopened.store = store
	status, err := reopened.SourceMigrationStatus(ctx, config.SourceID)
	require.NoError(t, err)
	require.Equal(t, ShadowSourceMigrationStatus{Route: initial, SourceIdentity: config.SourceIdentity, Pending: true}, status)
	_, _, err = reopened.CancelSourceMigration(ctx, config.SourceID, 2, ShadowCutoverWatermark{}, nil)
	require.ErrorIs(t, err, ErrShadowRouteRevisionChanged)
	active, err := reader.Route(ctx, config.SourceID)
	require.NoError(t, err)
	require.Equal(t, initial, active)
	_, err = publisher.Cutover(ctx, oldProof)
	require.ErrorIs(t, err, ErrShadowRouteMigrationPending)
	_, _, err = publisher.Verify(ctx, config.SourceID, ShadowCutoverWatermark{}, nil)
	require.ErrorIs(t, err, ErrShadowRouteMigrationPending)
	require.ErrorIs(t, publisher.PrepareSourceMigration(ctx, config.SourceID, 2), ErrShadowRouteRevisionChanged)
	require.Equal(t, 1, conn.commits)
}

// TestShadowRouteReaderFallbackUsesOnlyRecentConfirmedRevision 验证读路由缓存降级的时间和错误边界。
func TestShadowRouteReaderFallbackUsesOnlyRecentConfirmedRevision(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		config := shadowStoreTestConfig()
		other := config
		other.SourceID, other.LegacyDatabase, other.ShadowDatabase = "other", "other_legacy", "other_shadow"
		configs := []ShadowRoute{config, other}
		_, digest, err := shadowRouteKeys(config)
		require.NoError(t, err)
		initial := ReadRoute{SourceID: config.SourceID, Database: config.LegacyDatabase, Revision: 1}
		current := ReadRoute{SourceID: config.SourceID, Database: config.ShadowDatabase, Revision: 2,
			UsingShadow: true, Watermark: routingWatermark(config, 120).Source, SwitchedAt: time.Now().UTC()}
		revision := current.Revision
		payload, err := json.Marshal(current)
		require.NoError(t, err)
		conn := &shadowStoreTestConn{query: func() []driver.Value {
			return []driver.Value{digest[:], int64(revision), payload, make([]byte, 16)}
		}}
		store := openShadowRouteTestStore(t, conn)
		strict, err := NewShadowRouteReader(store, configs)
		require.NoError(t, err)
		cached, err := NewShadowRouteReaderWithCache(store, configs, 5*time.Second)
		require.NoError(t, err)
		publisher, err := NewShadowRouter(configs)
		require.NoError(t, err)
		publisher.store = store
		for _, reader := range []*ShadowRouteReader{strict, cached} {
			got, err := reader.Route(ctx, config.SourceID)
			require.NoError(t, err)
			require.Equal(t, current, got)
		}

		outage := &net.OpError{Op: "dial", Net: "tcp", Err: io.ErrUnexpectedEOF}
		conn.queryError = outage
		got, usedCache, err := cached.RouteWithStatus(ctx, config.SourceID)
		require.NoError(t, err)
		require.Equal(t, current, got)
		require.True(t, usedCache)
		_, err = cached.Route(ctx, other.SourceID)
		require.ErrorIs(t, err, outage)
		_, err = strict.Route(ctx, config.SourceID)
		require.ErrorIs(t, err, outage)
		_, err = publisher.RouteContext(ctx, config.SourceID)
		require.ErrorIs(t, err, outage)
		reopened, err := NewShadowRouteReaderWithCache(store, configs, 5*time.Second)
		require.NoError(t, err)
		_, err = reopened.Route(ctx, config.SourceID)
		require.ErrorIs(t, err, outage)

		conn.waitForCancellation = true
		deadline, cancel := context.WithTimeout(ctx, time.Second)
		_, err = cached.Route(deadline, config.SourceID)
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		conn.waitForCancellation = false
		conn.queryError = nil
		revision = initial.Revision
		payload, err = json.Marshal(initial)
		require.NoError(t, err)
		_, err = cached.Route(ctx, config.SourceID)
		require.ErrorIs(t, err, ErrShadowRouteRevisionChanged)
		conn.queryError = outage
		_, err = cached.Route(ctx, config.SourceID)
		require.ErrorIs(t, err, outage)

		conn.queryError = nil
		revision = current.Revision
		payload, err = json.Marshal(current)
		require.NoError(t, err)
		_, err = cached.Route(ctx, config.SourceID)
		require.NoError(t, err)
		payload = []byte(`{"invalid"`)
		_, err = cached.Route(ctx, config.SourceID)
		var invalidJSON *json.SyntaxError
		require.ErrorAs(t, err, &invalidJSON)
		conn.queryError = outage
		_, err = cached.Route(ctx, config.SourceID)
		require.ErrorIs(t, err, outage)

		conn.queryError = nil
		payload, err = json.Marshal(current)
		require.NoError(t, err)
		_, err = cached.Route(ctx, config.SourceID)
		require.NoError(t, err)
		busy := &mysql.MySQLError{Number: 1053, Message: "server shutdown"}
		conn.queryError = busy
		time.Sleep(4 * time.Second)
		_, err = cached.Route(ctx, config.SourceID)
		require.NoError(t, err)
		time.Sleep(time.Second)
		_, err = cached.Route(ctx, config.SourceID)
		require.ErrorIs(t, err, busy)
	})
}

// TestMigrateShadowSourceRejectsIncompleteLineageEvidence 验证 GTID 历史或交付证据不完整时拒绝迁移并保留原路由。
func TestMigrateShadowSourceRejectsIncompleteLineageEvidence(t *testing.T) {
	const oldUUID = "1656a622-a170-4d00-b2f3-05d07c2441d8"
	const newUUID = "48c3bedd-8a7f-48f1-9dca-a80f57aa4d02"
	ctx := t.Context()
	config := shadowStoreTestConfig()
	config.SourceIdentity = "orders/" + oldUUID
	previous := Cursor{SourceIdentity: config.SourceIdentity, File: "mysql-bin.000001", Position: 120,
		Flavor: "mysql", GTIDSet: oldUUID + ":1-10"}
	newCursor := Cursor{SourceIdentity: "orders/" + newUUID, File: "mysql-promoted.000001", Position: 128,
		Flavor: "mysql", GTIDSet: oldUUID + ":1-10," + newUUID + ":1"}
	active := ReadRoute{SourceID: config.SourceID, Database: config.ShadowDatabase,
		UsingShadow: true, Revision: 2, Watermark: previous, SwitchedAt: time.Now().UTC()}
	_, digest, err := shadowRouteKeys(config)
	require.NoError(t, err)
	payload, err := json.Marshal(shadowRoutePublication{ReadRoute: active, MigrationPending: true})
	require.NoError(t, err)
	for _, scenario := range []struct {
		name, wantError string
		change          func(*ShadowCutoverWatermark)
	}{
		{"candidate with missing history", "does not include previously committed transactions", func(w *ShadowCutoverWatermark) {
			w.Source.GTIDSet = newUUID + ":1"
		}},
		{"legacy writer without full GTID", "checkpoint differs from the GTID watermark", func(w *ShadowCutoverWatermark) {
			w.LegacyDelivered.GTIDSet = ""
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			conn := &shadowStoreTestConn{query: func() []driver.Value {
				return []driver.Value{digest[:], int64(2), payload, make([]byte, 16)}
			}}
			store := openShadowRouteTestStore(t, conn)
			router, err := NewShadowRouter([]ShadowRoute{config})
			require.NoError(t, err)
			router.store = store
			watermark := ShadowCutoverWatermark{Source: newCursor, LegacyDelivered: newCursor, ShadowDelivered: newCursor}
			scenario.change(&watermark)
			_, _, err = router.MigrateSource(ctx, config.SourceID, previous, watermark, nil)
			require.ErrorContains(t, err, scenario.wantError)
			got, err := router.RouteContext(ctx, config.SourceID)
			require.NoError(t, err)
			require.Equal(t, active, got)
			require.Zero(t, conn.commits)
		})
	}
}

// TestSQLShadowRouteStorePublication 验证提交应答丢失后，仅在已保存的发布令牌一致时确认成功。
func TestSQLShadowRouteStorePublication(t *testing.T) {
	route := shadowStoreTestConfig()
	initial := ReadRoute{SourceID: route.SourceID, Database: route.LegacyDatabase, Revision: 1}
	next := ReadRoute{
		SourceID: route.SourceID, Database: route.ShadowDatabase, UsingShadow: true, Revision: 2,
		Watermark: routingWatermark(route, 125).Source, SwitchedAt: time.Date(2026, 11, 27, 2, 30, 0, 0, time.UTC),
	}
	_, binding, err := shadowRouteKeys(route)
	require.NoError(t, err)
	nextJSON, err := json.Marshal(next)
	require.NoError(t, err)
	for _, tc := range []struct {
		name                    string
		matchesPublicationToken bool
		wantError               error
	}{
		{name: "lost acknowledgement with matching token", matchesPublicationToken: true},
		{name: "equal payload from another attempt", wantError: ErrShadowRoutePublicationUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &shadowStoreTestConn{commitError: io.ErrUnexpectedEOF}
			conn.query = func() []driver.Value {
				token := conn.attempt
				if !tc.matchesPublicationToken {
					token = append([]byte(nil), token...)
					token[0] ^= 1
				}
				return []driver.Value{binding[:], int64(2), nextJSON, token}
			}
			store := openShadowRouteTestStore(t, conn)
			previous := shadowRouteRecord{shadowRoutePublication: shadowRoutePublication{ReadRoute: initial}, token: make([]byte, 16)}
			err := store.compareAndSwap(t.Context(), route, previous, shadowRoutePublication{ReadRoute: next})
			require.ErrorIs(t, err, tc.wantError)
			require.Equal(t, 1, conn.queries)
			require.Equal(t, 1, conn.commits)
		})
	}
}

// shadowStoreTestConfig 返回供路由注册表测试复用的源、目标和表投影配置。
func shadowStoreTestConfig() ShadowRoute {
	return ShadowRoute{
		SourceID: "mysql", SourceIdentity: "source-member", SourceDatabase: "source_db", LegacyDatabase: "legacy_db",
		ShadowDatabase: "shadow_db", Tables: routingTables("accounts"),
	}
}

// openShadowRouteTestStore 用模拟连接创建路由注册表，并在测试结束时关闭连接池。
func openShadowRouteTestStore(t *testing.T, conn *shadowStoreTestConn) *SQLShadowRouteStore {
	t.Helper()
	db := sql.OpenDB(conn)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	store, err := NewSQLShadowRouteStore(db, "registry")
	require.NoError(t, err)
	return store
}

type shadowStoreTestConn struct {
	commitError         error
	queryError          error
	waitForCancellation bool
	attempt             []byte
	query               func() []driver.Value
	updateRoute         func(payload, token []byte)
	queries, commits    int
}

// Connect 返回测试连接器持有的模拟注册表连接。
func (c *shadowStoreTestConn) Connect(context.Context) (driver.Conn, error) { return c, nil }

// Driver 返回空驱动引用，模拟注册表连接由 Connect 提供。
func (c *shadowStoreTestConn) Driver() driver.Driver { return nil }

// Close 结束模拟注册表连接的生命周期。
func (c *shadowStoreTestConn) Close() error { return nil }

// Prepare 返回 driver.ErrSkip，表示模拟连接跳过预处理语句。
func (c *shadowStoreTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

// Begin 复用当前模拟连接作为事务实现。
func (c *shadowStoreTestConn) Begin() (driver.Tx, error) { return c, nil }

// Commit 累加提交次数并返回预设的提交错误。
func (c *shadowStoreTestConn) Commit() error {
	c.commits++
	return c.commitError
}

// Rollback 作为模拟事务的回滚入口，直接报告成功。
func (c *shadowStoreTestConn) Rollback() error { return nil }

// ExecContext 校验比较交换的 SQL 和参数，并记录模拟发布的内容与令牌。
func (c *shadowStoreTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(query, "UPDATE `registry`.`cdc_read_routes`") ||
		!strings.Contains(query, "WHERE source_key = ? AND config_digest = ? AND revision = CAST(? AS UNSIGNED) AND publication_token = ?") || len(args) != 7 {
		return nil, fmt.Errorf("unexpected CDC route update: %s", query)
	}
	c.attempt = append([]byte(nil), args[2].Value.([]byte)...)
	if c.updateRoute != nil {
		c.updateRoute(append([]byte(nil), args[1].Value.([]byte)...), c.attempt)
	}
	return driver.RowsAffected(1), nil
}

// QueryContext 验证路由查找语句，统计查询次数并返回预设记录。
func (c *shadowStoreTestConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !strings.HasPrefix(query, "SELECT config_digest, revision, route_json, publication_token") {
		return nil, fmt.Errorf("unexpected CDC read route lookup: %s", query)
	}
	c.queries++
	if c.waitForCancellation {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.queryError != nil {
		return nil, c.queryError
	}
	return &dorisTestRows{fields: []string{"config_digest", "revision", "route_json", "publication_token"}, values: c.query()}, nil
}
