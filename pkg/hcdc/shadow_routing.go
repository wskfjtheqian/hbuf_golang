package cdc

import (
	"context"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// ShadowRoute 将数据源及其选定的表绑定到两个 Doris 数据库。
// 目标数据库使用与源数据库相同的表名。SourceIdentity 记录初始注册的源标识。
// 持久化路由的源迁移完成后，以已发布路由中的物理成员为准。
type ShadowRoute struct {
	SourceID       string                           `yaml:"SourceID"`
	SourceIdentity string                           `yaml:"SourceIdentity"`
	SourceDatabase string                           `yaml:"SourceDatabase"`
	LegacyDatabase string                           `yaml:"LegacyDatabase"`
	ShadowDatabase string                           `yaml:"ShadowDatabase"`
	Tables         map[string]ShadowTableProjection `yaml:"Tables"`
}

// ShadowTableProjection 指定单张表参与核对的列和完整主键。
// 调用方为所有写入方配置相同的列投影。
type ShadowTableProjection struct {
	Columns    []string `yaml:"Columns"`
	KeyColumns []string `yaml:"KeyColumns"`
}

// ReadRoute 记录指定源选定的读取数据库及路由修订号。
// 消费方在一次完整的读取操作中使用同一份路由快照。
// SwitchedAt 记录最近一次已提交的目标切换、源迁移或迁移取消的时间。
type ReadRoute struct {
	SourceID    string
	Database    string
	UsingShadow bool
	Revision    uint64
	Watermark   Cursor
	SwitchedAt  time.Time
}

// ShadowCutoverWatermark 记录源写入围栏及围栏生效期间两路写入的交付位点。
// 仅比较物理位点时，Source.GTIDSet 可以为空。
type ShadowCutoverWatermark struct {
	Source          Cursor
	LegacyDelivered Cursor
	ShadowDelivered Cursor
}

// ShadowCutoverProof 是 ShadowRouter.Verify 在数据一致时签发的核对凭证。
// 凭证仅适用于签发它的路由器、对应数据源及核对时的路由修订号。
type ShadowCutoverProof struct {
	router     *ShadowRouter
	sourceID   string
	revision   uint64
	watermark  Cursor
	verifiedAt time.Time
}

type shadowRouteState struct {
	config ShadowRoute
	record shadowRouteRecord
}

// ShadowRouter 在两路 CDC 持续写入期间核对数据并逐源调整读取目标。
// 路由调整可发布到共享 SQL 存储。
type ShadowRouter struct {
	mu     sync.RWMutex
	routes map[string]shadowRouteState
	store  *SQLShadowRouteStore
}

// NewShadowRouter 校验各数据源的路由配置，并将初始读取目标设为旧库。
func NewShadowRouter(configs []ShadowRoute) (*ShadowRouter, error) {
	if len(configs) == 0 {
		return nil, herror.NewError("at least one shadow read route is required")
	}

	router := &ShadowRouter{routes: make(map[string]shadowRouteState, len(configs))}
	targets := make(map[string]struct{}, 2*len(configs))
	for _, config := range configs {
		if config.SourceID == "" || config.SourceIdentity == "" || !validPipelineIdentifier(config.SourceDatabase) ||
			!validPipelineIdentifier(config.LegacyDatabase) || !validPipelineIdentifier(config.ShadowDatabase) ||
			strings.EqualFold(config.LegacyDatabase, config.ShadowDatabase) {
			return nil, herror.NewError("a shadow read route requires source identity, a source database and distinct valid target databases")
		}
		if _, exists := router.routes[config.SourceID]; exists {
			return nil, herror.NewError("duplicate shadow read route for source " + config.SourceID)
		}
		for _, database := range []string{config.LegacyDatabase, config.ShadowDatabase} {
			database = strings.ToLower(database)
			if _, exists := targets[database]; exists {
				return nil, herror.NewError("Doris database belongs to multiple shadow routes: " + database)
			}
			targets[database] = struct{}{}
		}
		if len(config.Tables) == 0 {
			return nil, herror.NewError("a shadow read route requires the selected source tables")
		}
		tables := make(map[string]ShadowTableProjection, len(config.Tables))
		for table, projection := range config.Tables {
			if !validPipelineIdentifier(table) {
				return nil, herror.NewError("shadow read route table names must be valid")
			}
			if _, err := shadowKeyIndices(projection.Columns, projection.KeyColumns); err != nil {
				return nil, herror.WrapMsg(err, "validate shadow read route table "+table)
			}
			tables[table] = ShadowTableProjection{Columns: slices.Clone(projection.Columns), KeyColumns: slices.Clone(projection.KeyColumns)}
		}
		config.Tables = tables
		initial := ReadRoute{SourceID: config.SourceID, Database: config.LegacyDatabase, Revision: 1}
		router.routes[config.SourceID] = shadowRouteState{config: config, record: shadowRouteRecord{
			shadowRoutePublication: shadowRoutePublication{ReadRoute: initial},
		}}
	}
	return router, nil
}

// OpenPersistentShadowRouter 打开已初始化的持久化路由。
// 它将存储中的路由与调用方提供的配置核对，并使用现有路由记录。
func OpenPersistentShadowRouter(ctx context.Context, store *SQLShadowRouteStore, configs []ShadowRoute) (*ShadowRouter, error) {
	if store == nil {
		return nil, herror.NewError("persistent CDC read router requires a SQL route store")
	}
	router, err := NewShadowRouter(configs)
	if err != nil {
		return nil, err
	}
	if err := store.verifyEngine(ctx); err != nil {
		return nil, err
	}
	for _, state := range router.routes {
		if _, err := store.load(ctx, state.config); err != nil {
			return nil, err
		}
	}
	router.store = store
	return router, nil
}

// Route 查询指定源当前选定的读取目标。
// 查询持久化路由的默认超时为五秒，可通过 RouteContext 指定期限。
func (r *ShadowRouter) Route(sourceID string) (ReadRoute, error) {
	if r.store == nil {
		return r.RouteContext(context.Background(), sourceID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.RouteContext(ctx, sourceID)
}

// RouteContext 查询指定源的读取目标，持久化路由每次读取已提交的记录。
func (r *ShadowRouter) RouteContext(ctx context.Context, sourceID string) (ReadRoute, error) {
	state, err := r.routeState(ctx, sourceID)
	if err != nil {
		return ReadRoute{}, err
	}
	return state.record.ReadRoute, nil
}

// routeState 根据源 ID 获取路由状态，并在启用持久化时读取注册表中的最新记录。
func (r *ShadowRouter) routeState(ctx context.Context, sourceID string) (shadowRouteState, error) {
	if err := ctx.Err(); err != nil {
		return shadowRouteState{}, herror.Wrap(err)
	}
	r.mu.RLock()
	state, exists := r.routes[sourceID]
	r.mu.RUnlock()
	if !exists {
		return shadowRouteState{}, herror.NewError("no shadow read route for source " + sourceID)
	}
	if r.store != nil {
		var err error
		state.record, err = r.store.loadRecord(ctx, state.config)
		return state, err
	}
	return state, nil
}

// Verify 在同一个源写入围栏下核对所有选定的表。
// 调用方提供已观察到的交付检查点，并保持围栏直到随后的 Cutover 或 Rollback 完成。
// 发现差异时返回报告及不可用于切换的凭证，核对期间允许并行读取和更新路由。
// 持久化路由会在每条源端读取连接上核对已记录的源成员，MariaDB 部署必须为各成员分配唯一的 server_id。
func (r *ShadowRouter) Verify(ctx context.Context, sourceID string, watermark ShadowCutoverWatermark, comparisons []ShadowVerificationConfig) (ShadowCutoverProof, []ShadowVerification, error) {
	state, err := r.routeState(ctx, sourceID)
	if err != nil {
		return ShadowCutoverProof{}, nil, err
	}
	if state.record.MigrationPending {
		return ShadowCutoverProof{}, nil, ErrShadowRouteMigrationPending
	}
	if err := validateCutoverWatermark(state, watermark); err != nil {
		return ShadowCutoverProof{}, nil, err
	}
	var sourceMember *Cursor
	if r.store != nil {
		sourceMember = &watermark.Source
	}
	reports, matched, err := verifyCutoverData(ctx, state.config, comparisons, sourceMember)
	if err != nil || !matched {
		return ShadowCutoverProof{}, reports, err
	}
	return ShadowCutoverProof{
		router: r, sourceID: sourceID, revision: state.record.Revision,
		watermark: watermark.Source, verifiedAt: time.Now(),
	}, reports, nil
}

// verifyCutoverData 按完整主键范围核对路由选定的每张表，汇总报告及一致性结果。
func verifyCutoverData(ctx context.Context, config ShadowRoute, comparisons []ShadowVerificationConfig, member *Cursor) ([]ShadowVerification, bool, error) {
	if err := validateCutoverComparisons(config, comparisons); err != nil {
		return nil, false, err
	}
	reports := make([]ShadowVerification, 0, len(comparisons))
	matched := true
	for _, comparison := range comparisons {
		report, err := verifyShadow(ctx, comparison, true, member)
		if err != nil {
			return reports, false, herror.WrapMsg(err, "verify shadow table "+comparison.Source.Table)
		}
		reports = append(reports, report)
		matched = matched && report.Equal
	}
	return reports, matched, nil
}

// validateCutoverComparisons 检查核对请求中的数据库、表、列投影和主键配置是否与路由完全一致。
func validateCutoverComparisons(route ShadowRoute, comparisons []ShadowVerificationConfig) error {
	if len(comparisons) != len(route.Tables) {
		return herror.NewError("comparison must cover every table selected for the shadow read route")
	}
	verifiedTables := make(map[string]struct{}, len(comparisons))
	for _, comparison := range comparisons {
		table := comparison.Source.Table
		projection, selected := route.Tables[table]
		if !selected || comparison.Source.Schema != route.SourceDatabase ||
			comparison.Legacy.Schema != route.LegacyDatabase || comparison.Shadow.Schema != route.ShadowDatabase ||
			comparison.Legacy.Table != table || comparison.Shadow.Table != table {
			return herror.NewError("comparison must use a selected table in the configured source, legacy and shadow databases")
		}
		if _, exists := verifiedTables[table]; exists {
			return herror.NewError("comparison contains the same source table more than once")
		}
		if !slices.Equal(projection.Columns, comparison.Columns) || !slices.Equal(projection.KeyColumns, comparison.KeyColumns) {
			return herror.NewError("comparison must use the configured projection and primary key for " + table)
		}
		verifiedTables[table] = struct{}{}
	}
	return nil
}

// Cutover 使用当前写入围栏与路由修订号下签发的凭证，把指定源的读取目标切换到影子库。
func (r *ShadowRouter) Cutover(ctx context.Context, proof ShadowCutoverProof) (ReadRoute, error) {
	return r.switchReadRoute(ctx, proof, true)
}

// Rollback 使用当前写入围栏与路由修订号下重新签发的凭证，把指定源的读取目标切回旧库。
func (r *ShadowRouter) Rollback(ctx context.Context, proof ShadowCutoverProof) (ReadRoute, error) {
	return r.switchReadRoute(ctx, proof, false)
}

// switchReadRoute 切换读取目标，并记录已提交的路由修订号和位点。
func (r *ShadowRouter) switchReadRoute(ctx context.Context, proof ShadowCutoverProof, useShadow bool) (ReadRoute, error) {
	if err := ctx.Err(); err != nil {
		return ReadRoute{}, herror.Wrap(err)
	}

	route, err := r.updateReadRoute(ctx, proof, useShadow)
	if err != nil {
		return ReadRoute{}, err
	}
	hlog.Info(ctx, "CDC read route source_id=%s database=%s revision=%d file=%s position=%d",
		route.SourceID, route.Database, route.Revision, route.Watermark.File, route.Watermark.Position)
	return route, nil
}

// updateReadRoute 校验切换凭证，并通过持久化比较交换或内存锁更新读取路由。
func (r *ShadowRouter) updateReadRoute(ctx context.Context, proof ShadowCutoverProof, useShadow bool) (ReadRoute, error) {
	if proof.router != r {
		return ReadRoute{}, herror.NewError("a matching comparison from this CDC read router is required")
	}
	if r.store != nil {
		state, err := r.routeState(ctx, proof.sourceID)
		if err != nil {
			return ReadRoute{}, err
		}
		if state.record.MigrationPending {
			return ReadRoute{}, ErrShadowRouteMigrationPending
		}
		next, err := nextReadRoute(state, proof, useShadow, true)
		if err != nil {
			return ReadRoute{}, err
		}
		publication := state.record.shadowRoutePublication
		publication.ReadRoute = next
		if err := r.store.compareAndSwap(ctx, state.config, state.record, publication); err != nil {
			return ReadRoute{}, err
		}
		return next, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ReadRoute{}, herror.Wrap(err)
	}

	state, exists := r.routes[proof.sourceID]
	if !exists {
		return ReadRoute{}, herror.NewError("no shadow read route for source " + proof.sourceID)
	}
	next, err := nextReadRoute(state, proof, useShadow, false)
	if err != nil {
		return ReadRoute{}, err
	}
	state.record.ReadRoute = next
	r.routes[proof.sourceID] = state
	return next, nil
}

// nextReadRoute 校验修订号和凭证的时效性，并生成读取目标切换后的路由。
func nextReadRoute(state shadowRouteState, proof ShadowCutoverProof, useShadow, persistent bool) (ReadRoute, error) {
	active := state.record.ReadRoute
	if proof.revision != active.Revision || active.Revision == math.MaxUint64 {
		return ReadRoute{}, ErrShadowRouteRevisionChanged
	}
	if active.UsingShadow == useShadow {
		return ReadRoute{}, herror.NewError("CDC read route already selects the requested database")
	}

	now := time.Now()
	if proof.verifiedAt.IsZero() || proof.verifiedAt.After(now) || (!persistent && !proof.verifiedAt.After(active.SwitchedAt)) {
		return ReadRoute{}, herror.NewError("a fresh matching comparison is required for the CDC read switch")
	}

	active.Database = state.config.LegacyDatabase
	if useShadow {
		active.Database = state.config.ShadowDatabase
	}
	active.UsingShadow = useShadow
	active.Revision++
	active.Watermark = proof.watermark
	active.SwitchedAt = now.UTC()
	return active, nil
}

// validateCutoverWatermark 校验源成员身份、切换位点的单调性和两路写入的交付位点。
func validateCutoverWatermark(state shadowRouteState, watermark ShadowCutoverWatermark) error {
	if watermark.Source.SourceIdentity != state.record.sourceIdentity(state.config) {
		return herror.NewError("comparison watermark belongs to a different binlog source")
	}
	_, _, set, err := parseCursor(watermark.Source)
	if err != nil {
		return herror.WrapMsg(err, "validate CDC read-switch watermark")
	}
	if state.record.ReboundSourceIdentity != "" {
		if err := validateGTIDCursor(watermark.Source, set); err != nil {
			return herror.WrapMsg(err, "validate CDC read-switch GTID watermark after migration")
		}
	}
	previous := state.record.Watermark
	if state.record.Revision > 1 {
		check := validateAdvance
		if previous.File == watermark.Source.File && previous.Position == watermark.Source.Position {
			check = verifyAtWatermark
		}
		if err := check(previous, watermark.Source); err != nil {
			return herror.WrapMsg(err, "verify monotonic CDC read-switch watermark")
		}
	}
	return verifyDeliveredWatermarks(watermark)
}

// verifyDeliveredWatermarks 核对旧库和影子库的交付检查点是否都到达同一个源位点。
func verifyDeliveredWatermarks(watermark ShadowCutoverWatermark) error {
	if err := verifyAtWatermark(watermark.Source, watermark.LegacyDelivered); err != nil {
		return herror.WrapMsg(err, "verify legacy checkpoint at the comparison watermark")
	}
	if err := verifyAtWatermark(watermark.Source, watermark.ShadowDelivered); err != nil {
		return herror.WrapMsg(err, "verify shadow checkpoint at the comparison watermark")
	}
	return nil
}

// verifyAtWatermark 逐项核对检查点与核对位点的源身份、类型、物理位点及可选 GTID 集合。
func verifyAtWatermark(watermark, delivered Cursor) error {
	if delivered.SourceIdentity != watermark.SourceIdentity || cursorFlavor(delivered) != cursorFlavor(watermark) {
		return herror.NewError("CDC checkpoint belongs to a different binlog source or flavor")
	}
	if delivered.File != watermark.File || delivered.Position != watermark.Position {
		return herror.NewError("CDC checkpoint is outside the exact physical comparison watermark")
	}
	if err := validateCursor(delivered); err != nil {
		return err
	}
	if watermark.GTIDSet == "" {
		return nil
	}
	flavor := cursorFlavor(watermark)
	expected, err := mysql.ParseGTIDSet(flavor, watermark.GTIDSet)
	if err != nil {
		return herror.Wrap(err)
	}
	actual, err := mysql.ParseGTIDSet(flavor, delivered.GTIDSet)
	if err != nil {
		return herror.Wrap(err)
	}
	if !actual.Equal(expected) {
		return herror.NewError("CDC checkpoint differs from the GTID watermark")
	}
	return nil
}
