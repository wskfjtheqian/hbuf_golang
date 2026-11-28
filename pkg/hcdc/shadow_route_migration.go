package hcdc

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/google/uuid"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// ErrShadowRouteMigrationPending 表示源正在更换物理成员，发布操作受到围栏限制。
// 消费方仍可读取已提交的路由。
var ErrShadowRouteMigrationPending = herror.NewError("CDC read route source migration is pending")

// ShadowSourceMigrationStatus 记录已注册的源在注册表中的权威迁移状态。
// Pending 为真时，Route.Revision 标识已准备的迁移操作。
// LastCanceledRevision 记录最近一次取消操作消耗的准备修订号。
type ShadowSourceMigrationStatus struct {
	Route                ReadRoute
	SourceIdentity       string
	Pending              bool
	LastCanceledRevision uint64
}

// SourceMigrationStatus 从注册表读取当前迁移准备状态及已发布的读取目标。
// 可在进程重启或提交结果不明时调用。
func (r *ShadowRouter) SourceMigrationStatus(ctx context.Context, sourceID string) (ShadowSourceMigrationStatus, error) {
	if r.store == nil {
		return ShadowSourceMigrationStatus{}, herror.NewError("CDC read route source migration requires a persistent router")
	}
	state, err := r.routeState(ctx, sourceID)
	if err != nil {
		return ShadowSourceMigrationStatus{}, err
	}
	return ShadowSourceMigrationStatus{
		Route: state.record.ReadRoute, SourceIdentity: state.record.sourceIdentity(state.config),
		Pending: state.record.MigrationPending, LastCanceledRevision: state.record.LastCanceledRevision,
	}, nil
}

// PrepareSourceMigration 在指定修订号下持久化当前源的发布围栏，保留消费路由和修订号。
// 迁移待完成期间，可用同一修订号安全地重复调用。
// 准备迁移前，先部署能够识别迁移状态的发布方。
func (r *ShadowRouter) PrepareSourceMigration(ctx context.Context, sourceID string, expectedRevision uint64) error {
	if r.store == nil {
		return herror.NewError("CDC read route source migration requires a persistent router")
	}
	state, err := r.routeState(ctx, sourceID)
	if err != nil {
		return err
	}
	if state.record.Revision != expectedRevision || expectedRevision == math.MaxUint64 {
		return ErrShadowRouteRevisionChanged
	}
	if state.record.MigrationPending {
		return nil
	}
	next := state.record.shadowRoutePublication
	next.MigrationPending = true
	if err := r.store.compareAndSwap(ctx, state.config, state.record, next); err != nil {
		return err
	}
	hlog.Info(ctx, "CDC read route source migration prepared source_id=%s revision=%d", sourceID, next.Revision)
	return nil
}

// MigrateSource 把已准备的路由绑定到同一逻辑拓扑中的另一个 GTID 成员。
// oldCheckpoint 是在旧成员观察到的交付检查点。
// 调用方必须隔离旧成员，并从观察到 watermark 起至方法返回期间维持新成员的写入围栏。
// 两路写入都必须达到 watermark，且所有选定表的数据必须一致。
// 保留当前已提交的读取目标，并发布新的路由修订号。
// 该入口用于首次迁移。取消或迁移之后，通过 ResumeSourceMigration 为请求指定准备修订号。
func (r *ShadowRouter) MigrateSource(ctx context.Context, sourceID string, oldCheckpoint Cursor, watermark ShadowCutoverWatermark, comparisons []ShadowVerificationConfig) (ReadRoute, []ShadowVerification, error) {
	return r.migrateSource(ctx, sourceID, 0, oldCheckpoint, watermark, comparisons)
}

// ResumeSourceMigration 完成指定准备修订号下的迁移。
// 提交结果不明时，先调用 SourceMigrationStatus 查询当前状态，再重新收集核对证据。
func (r *ShadowRouter) ResumeSourceMigration(ctx context.Context, sourceID string, expectedRevision uint64, oldCheckpoint Cursor, watermark ShadowCutoverWatermark, comparisons []ShadowVerificationConfig) (ReadRoute, []ShadowVerification, error) {
	if expectedRevision == 0 {
		return ReadRoute{}, nil, ErrShadowRouteRevisionChanged
	}
	return r.migrateSource(ctx, sourceID, expectedRevision, oldCheckpoint, watermark, comparisons)
}

// migrateSource 核对已准备的迁移及新成员证据，然后提交物理成员的重新绑定。
func (r *ShadowRouter) migrateSource(ctx context.Context, sourceID string, expectedRevision uint64, oldCheckpoint Cursor, watermark ShadowCutoverWatermark, comparisons []ShadowVerificationConfig) (ReadRoute, []ShadowVerification, error) {
	state, err := r.preparedSourceMigration(ctx, sourceID, expectedRevision)
	if err != nil {
		return ReadRoute{}, nil, err
	}
	if err := validateSourceMigration(state, oldCheckpoint, watermark); err != nil {
		return ReadRoute{}, nil, err
	}
	reports, err := verifySourceMigrationEvidence(ctx, state.config, watermark.Source, comparisons)
	if err != nil {
		return ReadRoute{}, reports, err
	}
	next := state.record.shadowRoutePublication
	next.ReboundSourceIdentity = watermark.Source.SourceIdentity
	route, err := r.finishSourceMigration(ctx, state, next, watermark.Source)
	if err != nil {
		return ReadRoute{}, reports, err
	}
	hlog.Info(ctx, "CDC read route source migrated source_id=%s database=%s revision=%d file=%s position=%d",
		sourceID, route.Database, route.Revision, route.Watermark.File, route.Watermark.Position)
	return route, reports, nil
}

// CancelSourceMigration 在新的写入围栏下核对当前绑定成员和两路 Doris 写入，并解除指定准备修订号下的发布围栏。
// 调用方必须保证当前绑定成员是唯一的可写主库。
// 取消操作保留当前读取目标与绑定成员，并推进路由修订号。
func (r *ShadowRouter) CancelSourceMigration(ctx context.Context, sourceID string, expectedRevision uint64, watermark ShadowCutoverWatermark, comparisons []ShadowVerificationConfig) (ReadRoute, []ShadowVerification, error) {
	if expectedRevision == 0 {
		return ReadRoute{}, nil, ErrShadowRouteRevisionChanged
	}
	state, err := r.preparedSourceMigration(ctx, sourceID, expectedRevision)
	if err != nil {
		return ReadRoute{}, nil, err
	}
	if err := validateCutoverWatermark(state, watermark); err != nil {
		return ReadRoute{}, nil, err
	}
	reports, err := verifySourceMigrationEvidence(ctx, state.config, watermark.Source, comparisons)
	if err != nil {
		return ReadRoute{}, reports, err
	}
	next := state.record.shadowRoutePublication
	next.LastCanceledRevision = expectedRevision
	route, err := r.finishSourceMigration(ctx, state, next, watermark.Source)
	if err != nil {
		return ReadRoute{}, reports, err
	}
	hlog.Info(ctx, "CDC read route source migration canceled source_id=%s prepared_revision=%d revision=%d",
		sourceID, expectedRevision, route.Revision)
	return route, reports, nil
}

// preparedSourceMigration 读取待完成的迁移，并验证请求对应的准备修订号。
func (r *ShadowRouter) preparedSourceMigration(ctx context.Context, sourceID string, expectedRevision uint64) (shadowRouteState, error) {
	if r.store == nil {
		return shadowRouteState{}, herror.NewError("CDC read route source migration requires a persistent router")
	}
	state, err := r.routeState(ctx, sourceID)
	if err != nil {
		return shadowRouteState{}, err
	}
	if expectedRevision != 0 && state.record.Revision != expectedRevision || state.record.Revision == math.MaxUint64 {
		return shadowRouteState{}, ErrShadowRouteRevisionChanged
	}
	if !state.record.MigrationPending {
		return shadowRouteState{}, herror.NewError("prepare the CDC read route before resolving its source migration")
	}
	if expectedRevision == 0 && (state.record.ReboundSourceIdentity != "" || state.record.LastCanceledRevision != 0) {
		return shadowRouteState{}, herror.NewError("resuming this CDC read route migration requires its prepared revision")
	}
	return state, nil
}

// verifySourceMigrationEvidence 核对选定表的数据与 GTID 位点，并确认新成员可写。
func verifySourceMigrationEvidence(ctx context.Context, config ShadowRoute, watermark Cursor, comparisons []ShadowVerificationConfig) ([]ShadowVerification, error) {
	_, _, expected, err := parseCursor(watermark)
	if err != nil {
		return nil, err
	}
	if err := validateGTIDCursor(watermark, expected); err != nil {
		return nil, err
	}
	reports, matched, err := verifyCutoverData(ctx, config, comparisons, &watermark)
	if err != nil {
		return reports, err
	}
	if !matched {
		return reports, herror.NewError("CDC read route migration requires all selected tables to match")
	}
	return reports, verifyMigrationPrimary(ctx, comparisons[0].Source.DB, watermark, expected)
}

// verifyMigrationPrimary 通过源连接验证物理成员身份、可写状态和当前 GTID 集合。
func verifyMigrationPrimary(ctx context.Context, db *sql.DB, watermark Cursor, expected mysql.GTIDSet) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return herror.WrapMsg(err, "open CDC route migration source connection")
	}
	defer conn.Close()
	if err := verifyShadowSourceMember(ctx, conn, watermark); err != nil {
		return err
	}
	flavor := cursorFlavor(watermark)
	query := "SELECT @@GLOBAL.gtid_executed, (@@GLOBAL.read_only OR @@GLOBAL.super_read_only)"
	if flavor == mysql.MariaDBFlavor {
		query = "SELECT @@GLOBAL.gtid_binlog_pos, @@GLOBAL.read_only"
	}
	var rawGTID string
	var readOnly bool
	if err := conn.QueryRowContext(ctx, query).Scan(&rawGTID, &readOnly); err != nil {
		return herror.WrapMsg(err, "read CDC route migration source state")
	}
	if readOnly {
		return herror.NewError("CDC read route migration requires a writable source member")
	}
	actual, err := mysql.ParseGTIDSet(flavor, rawGTID)
	if err != nil {
		return herror.WrapMsg(err, "parse CDC route migration source GTIDs")
	}
	if !expected.Equal(actual) {
		return herror.NewError("CDC read route migration watermark differs from the source GTID set")
	}
	return nil
}

// finishSourceMigration 提交新的位点及路由修订号，并清除迁移待完成状态。
func (r *ShadowRouter) finishSourceMigration(ctx context.Context, state shadowRouteState, next shadowRoutePublication, watermark Cursor) (ReadRoute, error) {
	next.Revision++
	next.SwitchedAt = time.Now().UTC()
	next.Watermark = watermark
	next.MigrationPending = false
	if err := r.store.compareAndSwap(ctx, state.config, state.record, next); err != nil {
		return ReadRoute{}, err
	}
	return next.ReadRoute, nil
}

// validateSourceMigration 校验旧检查点、同拓扑成员更换和两路写入的交付证据。
func validateSourceMigration(state shadowRouteState, oldCheckpoint Cursor, watermark ShadowCutoverWatermark) error {
	if oldCheckpoint.SourceIdentity != state.record.sourceIdentity(state.config) {
		return herror.NewError("old CDC checkpoint does not belong to the bound source member")
	}
	if err := validateCursorAdvance(oldCheckpoint, watermark.Source, true); err != nil {
		return herror.WrapMsg(err, "verify new CDC member GTID continuity")
	}
	oldTopology, oldMember, err := shadowMigrationMember(oldCheckpoint)
	if err != nil {
		return err
	}
	newTopology, newMember, err := shadowMigrationMember(watermark.Source)
	if err != nil {
		return err
	}
	if oldTopology != newTopology || oldMember == newMember {
		return herror.NewError("CDC read route migration requires a different member of the bound topology")
	}
	if state.record.Revision > 1 {
		published := state.record.Watermark
		check := validateAdvance
		if published.File == oldCheckpoint.File && published.Position == oldCheckpoint.Position {
			check = verifyAtWatermark
		}
		if err := check(published, oldCheckpoint); err != nil {
			return herror.WrapMsg(err, "verify old CDC checkpoint covers the published route watermark")
		}
	}
	return verifyDeliveredWatermarks(watermark)
}

// shadowMigrationMember 从源标识解析逻辑拓扑及 MySQL 或 MariaDB 的物理成员标识。
func shadowMigrationMember(cursor Cursor) (topology, member string, err error) {
	parent, name, found := strings.CutLast(cursor.SourceIdentity, "/")
	if found && parent != "" && name != "" {
		if cursorFlavor(cursor) == mysql.MariaDBFlavor {
			logical, endpoint, found := strings.CutLast(parent, "/")
			serverID, parseErr := strconv.ParseUint(name, 10, 32)
			if found && logical != "" && endpoint != "" && parseErr == nil && serverID != 0 {
				return logical, strconv.FormatUint(serverID, 10), nil
			}
		} else if serverUUID, parseErr := uuid.Parse(name); parseErr == nil && serverUUID != uuid.Nil {
			return parent, serverUUID.String(), nil
		}
	}
	return "", "", herror.NewError("CDC read route migration requires a valid physical source identity")
}
