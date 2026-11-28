package cdc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// MySQLConfig 指定一个 MySQL 或 MariaDB 源库及其同步表。
type MySQLConfig struct {
	Address        string   `yaml:"Address"`        // 源库的 TCP 地址，格式为主机:端口。
	User           string   `yaml:"User"`           // 源库用户名。
	Password       string   `yaml:"Password"`       // 源库密码。
	Flavor         string   `yaml:"Flavor"`         // 源库类型，默认为 mysql。
	SourceIdentity string   `yaml:"SourceIdentity"` // 跨主切换时保持稳定的逻辑源标识。
	ServerID       uint32   `yaml:"ServerID"`       // 复制客户端的唯一服务器 ID。
	Database       string   `yaml:"Database"`       // 源数据库名。
	Tables         []string `yaml:"Tables"`         // 源库中显式选定的表名。
	// GTIDAutoPosition 将代次绑定到 GTID 谱系。隔离旧主后可用新主地址恢复同一代次。
	GTIDAutoPosition bool `yaml:"GTIDAutoPosition"`
}

// PipelineConfig 将单个源、持久缓冲目录和独立的 Doris 目标库绑定到同一代次。
// 新代次从一致性全量快照开始。
type PipelineConfig struct {
	Identity       StreamIdentity `yaml:"Identity"`       // 流水线、源和代次标识。
	Directory      string         `yaml:"Directory"`      // 快照、缓冲和控制记录的本地持久目录。
	Source         MySQLConfig    `yaml:"Source"`         // 源库连接及同步表。
	Doris          DorisConfig    `yaml:"Doris"`          // Doris 连接及控制库配置。
	TargetDatabase string         `yaml:"TargetDatabase"` // 承接同步业务数据的 Doris 库。
	// ZeroDatePolicy 默认拒绝零日期，配置为 epoch 时按 hcdc 规则转换。
	ZeroDatePolicy ZeroDatePolicy `yaml:"ZeroDatePolicy"`
	// SnapshotBatchRows 指定每次快照导入的行数上限。为零时按 10 MiB 或 5 秒分批。
	SnapshotBatchRows int `yaml:"SnapshotBatchRows"`
	// MaxPendingBytes 限制未确认记录的编码字节数。为零时使用 256 MiB。
	MaxPendingBytes int64 `yaml:"MaxPendingBytes"`
	// MaxTransactionBytes 限制单笔事务归并前选表变更的暂存编码量。为零时沿用 MaxPendingBytes。
	MaxTransactionBytes int64 `yaml:"MaxTransactionBytes"`
}

type bootstrapManifest struct {
	Identity         StreamIdentity
	SourceIdentity   string
	Flavor           string
	SourceDatabase   string
	SourceTables     []string
	TargetDatabase   string
	MappingVersion   string
	GTIDAutoPosition bool `json:",omitempty"`
}

type bootstrapState struct {
	Cursor       Cursor
	Tables       []mysqlsource.Table
	ZeroDateRows uint64 `json:",omitempty"`
}

type bootstrapRecord struct {
	bootstrapState
	Prepared bool `json:",omitempty"`
}

type bootstrapTarget interface {
	Target
	verifySchemaRevision(context.Context, Checkpoint) error
}

const (
	pipelineMappingVersion        = "mysql-doris-v1"
	defaultDorisLoadBatchInterval = 5 * time.Second
	bootstrapPreparedSequence     = 1
	bootstrapCompletedSequence    = 2
)

// mappingVersion 返回与零日期转换规则绑定的持久映射版本。
func (c PipelineConfig) mappingVersion() string {
	if c.ZeroDatePolicy == ZeroDateEpoch {
		return pipelineMappingVersion + "-zero-date-epoch"
	}
	return pipelineMappingVersion
}

// RunPipeline 运行一个源的同步代次，直到上下文取消或发生需要恢复的错误。
// 重启后按已落盘记录和恢复游标继续处理。初始快照期间应冻结所选表的 DDL。
func RunPipeline(ctx context.Context, config PipelineConfig) error {
	pipeline, err := StartPipeline(ctx, config)
	if err != nil {
		return err
	}
	return pipeline.Wait()
}

// run 打开源库、Doris 和持久缓冲，恢复快照与结构状态后运行采集和交付。
func (p *Pipeline) run(ctx, captureCtx context.Context, config PipelineConfig, options pipelineStartOptions) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	if hlog.FromContext(ctx) == "" {
		ctx = hlog.WithContext(ctx, hlog.NewTraceId())
	}
	manifest, tables, err := validatePipelineConfig(config)
	if err != nil {
		return err
	}
	bootstrap, err := spool.Open(filepath.Join(config.Directory, "bootstrap"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, bootstrap.Close()) }()

	source, err := mysqlsource.Open(ctx, mysqlsource.Config{
		Address: config.Source.Address, User: config.Source.User, Password: config.Source.Password,
		Flavor: manifest.Flavor, SourceIdentity: config.Source.SourceIdentity,
		ServerID: config.Source.ServerID, Tables: tables, GTIDAutoPosition: config.Source.GTIDAutoPosition,
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.Close()) }()

	owner, err := loadDorisWriterIdentity(config.Directory, config.TargetDatabase, config.Identity)
	if err != nil {
		return err
	}
	target, err := openDorisTarget(ctx, config.Doris, &owner)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, target.Close()) }()

	state, err := prepareBootstrap(ctx, bootstrap, source, target, config, manifest, options.resumeOnly)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.status.SnapshotZeroDateRows != state.ZeroDateRows {
		p.status.SnapshotZeroDateRows = state.ZeroDateRows
		p.signalStatusLocked()
	}
	p.mu.Unlock()
	if options.activate != nil {
		if err := target.verifyHTTPAccess(ctx, config.TargetDatabase); err != nil {
			return err
		}
	}
	delivery, err := openPipelineDelivery(ctx, config, target, state.Cursor)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, delivery.Close()) }()

	history, err := spool.Open(filepath.Join(config.Directory, "schemas"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, history.Close()) }()
	identityPayload, err := json.Marshal(config.Identity)
	if err != nil {
		return herror.Wrap(err)
	}
	if _, err := history.Bind(identityPayload); err != nil {
		return err
	}
	version, revision, err := readSchemaVersion(history, state, delivery.Status().Durable, config.Source.GTIDAutoPosition)
	if err != nil {
		return err
	}
	if err := prepareDorisSchemaHistory(ctx, target, history, delivery, state); err != nil {
		return err
	}
	if err := reclaimSchemaHistory(history, delivery.Status().Delivered); err != nil {
		hlog.Warn(ctx, "CDC schema history cleanup deferred source_id=%s error=%v", config.Identity.SourceID, err)
	}
	control, err := spool.Open(filepath.Join(config.Directory, "control"))
	if err != nil {
		return err
	}
	defer func() {
		p.waitForControlWrite()
		resultErr = errors.Join(resultErr, control.Close())
	}()
	if err := p.restoreControl(ctx, control, config.Identity); err != nil {
		return err
	}
	return p.runWorkers(ctx, captureCtx, source, delivery, history, version, revision, config, options.activate)
}

// prepareDorisSchemaHistory 初始化目标的结构基线，核对已交付版本并配置 DDL 校验与持久化。
func prepareDorisSchemaHistory(ctx context.Context, target *DorisTarget, history *spool.Store, delivery *Delivery, state bootstrapState) error {
	if err := target.ensureSchemaHistory(ctx); err != nil {
		return err
	}
	original, err := snapshotCatalog(state)
	if err != nil {
		return herror.Wrap(err)
	}
	identity := delivery.config.Identity
	if err := target.persistSchemaVersion(ctx, identity, 0, digest(original), original); err != nil {
		return err
	}
	if err := target.verifySchemaRevision(ctx, delivery.Status().Delivered); err != nil {
		return err
	}
	delivery.validateSchema = func(record OrderedRecord) error {
		_, err := readBarrierCatalog(history, record)
		return err
	}
	delivery.persistSchema = func(ctx context.Context, record OrderedRecord) error {
		catalog, err := readBarrierCatalog(history, record)
		if err != nil {
			return err
		}
		return target.persistSchemaVersion(ctx, identity, record.Sequence, record.StructureRevision, catalog)
	}
	return nil
}

// snapshotCatalog 编码基线结构，使快照恢复与模式历史使用同一份证据。
func snapshotCatalog(state bootstrapState) ([]byte, error) {
	catalog, err := json.Marshal(schemaVersion{Cursor: state.Cursor, Tables: state.Tables})
	if err != nil {
		return nil, herror.Wrap(err)
	}
	return catalog, nil
}

// validatePipelineConfig 校验单源配置并生成稳定的快照清单及源表引用。
func validatePipelineConfig(config PipelineConfig) (bootstrapManifest, []mysqlsource.TableRef, error) {
	if strings.TrimSpace(config.Directory) == "" || config.Identity.PipelineID == "" ||
		config.Identity.SourceID == "" || config.Identity.Generation == "" ||
		config.Source.SourceIdentity == "" || config.Source.Address == "" ||
		config.Source.User == "" || config.Source.ServerID == 0 || !validPipelineIdentifier(config.Source.Database) ||
		!validPipelineIdentifier(config.TargetDatabase) || len(config.Source.Tables) == 0 ||
		config.SnapshotBatchRows < 0 || config.MaxPendingBytes < 0 || config.MaxTransactionBytes < 0 {
		return bootstrapManifest{}, nil, herror.NewError("CDC pipeline requires stream identity, directory, source configuration, target database and explicit source tables")
	}
	config = withPipelineConfigDefaults(config)
	if config.ZeroDatePolicy != ZeroDateReject && config.ZeroDatePolicy != ZeroDateEpoch {
		return bootstrapManifest{}, nil, herror.NewError("CDC zero date policy must be reject or epoch")
	}
	flavor := config.Source.Flavor
	if flavor != "mysql" && flavor != "mariadb" {
		return bootstrapManifest{}, nil, herror.NewError("CDC source flavor must be mysql or mariadb")
	}
	if strings.EqualFold(config.TargetDatabase, config.Doris.ControlDatabase) {
		return bootstrapManifest{}, nil, herror.NewError("CDC target and control databases must be separate")
	}
	if strings.EqualFold(config.TargetDatabase, dorisWriterRegistry) {
		return bootstrapManifest{}, nil, herror.NewError("CDC target database conflicts with the writer registry")
	}
	names := slices.Clone(config.Source.Tables)
	slices.Sort(names)
	tables := make([]mysqlsource.TableRef, 0, len(names))
	targetTables := make(map[string]bool, len(names))
	for _, name := range names {
		normalized := strings.ToLower(name)
		if !validPipelineIdentifier(name) || targetTables[normalized] {
			return bootstrapManifest{}, nil, herror.NewError("CDC source table names must be valid and unique")
		}
		targetTables[normalized] = true
		tables = append(tables, mysqlsource.TableRef{Schema: config.Source.Database, Name: name})
	}
	return bootstrapManifest{
		Identity: config.Identity, SourceIdentity: config.Source.SourceIdentity, Flavor: flavor,
		SourceDatabase: config.Source.Database, SourceTables: names, TargetDatabase: config.TargetDatabase,
		MappingVersion: config.mappingVersion(), GTIDAutoPosition: config.Source.GTIDAutoPosition,
	}, tables, nil
}

// validPipelineIdentifier 判断名称是否符合流水线使用的库表标识符格式。
func validPipelineIdentifier(name string) bool {
	return dorisIdentifier.MatchString(name)
}

// openPipelineDelivery 按当前代次和快照基线打开持久交付队列。
func openPipelineDelivery(ctx context.Context, config PipelineConfig, target Target, baseline Cursor) (*Delivery, error) {
	return OpenDelivery(ctx, DeliveryConfig{
		Directory: filepath.Join(config.Directory, "delivery"), Identity: config.Identity,
		Baseline: baseline, MappingVersion: config.mappingVersion(), GTIDAutoPosition: config.Source.GTIDAutoPosition,
	}, target)
}

// prepareBootstrap 核对已完成的快照，或在允许初始化时建立新的全量基线。
func prepareBootstrap(ctx context.Context, store *spool.Store, source *mysqlsource.Source, target *DorisTarget, config PipelineConfig, manifest bootstrapManifest, resumeOnly bool) (bootstrapState, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return bootstrapState{}, herror.Wrap(err)
	}
	stored, err := store.Metadata()
	if err != nil {
		return bootstrapState{}, err
	}
	if stored != nil {
		if !bytes.Equal(stored, encoded) {
			return bootstrapState{}, herror.NewError("CDC bootstrap belongs to a different stream or table mapping")
		}
		return restoreBootstrap(ctx, store, target, config)
	}
	if resumeOnly {
		return bootstrapState{}, herror.NewError("CDC recovery requires the original completed snapshot in the buffer directory")
	}
	sequences, err := store.Sequences()
	if err != nil {
		return bootstrapState{}, err
	}
	if len(sequences) != 0 {
		return bootstrapState{}, herror.NewError("CDC bootstrap manifest is missing for an existing snapshot record")
	}
	checkpoint, err := target.LoadCheckpoint(ctx, config.Identity)
	if err != nil {
		return bootstrapState{}, err
	}
	if checkpoint != nil {
		return bootstrapState{}, herror.NewError("CDC local snapshot state is missing for a generation already present in Doris")
	}
	if _, err := store.Bind(encoded); err != nil {
		return bootstrapState{}, err
	}

	hlog.Info(ctx, "CDC consistent snapshot starting source_id=%s", config.Identity.SourceID)
	state, err := runSnapshot(ctx, source, target, config)
	if err != nil {
		return bootstrapState{}, err
	}
	// 全量导入已全部确认。将原读视图的证据绑定到这份 Doris 目标和本地目录。
	catalog, err := snapshotCatalog(state)
	if err != nil {
		return bootstrapState{}, err
	}
	if err := target.ensureSchemaHistory(ctx); err != nil {
		return bootstrapState{}, err
	}
	if err := target.persistSchemaVersion(ctx, config.Identity, 0, digest(catalog), catalog); err != nil {
		return bootstrapState{}, err
	}
	payload, err := json.Marshal(bootstrapRecord{bootstrapState: state, Prepared: true})
	if err != nil {
		return bootstrapState{}, herror.Wrap(err)
	}
	if err := store.Append(bootstrapPreparedSequence, payload); err != nil {
		return bootstrapState{}, err
	}
	if err := completeBootstrap(ctx, store, target, config, state, payload); err != nil {
		return bootstrapState{}, err
	}
	return state, nil
}

// restoreBootstrap 读取已完成的记录，或在隔离校验后补齐仅待发布基线的快照。
func restoreBootstrap(ctx context.Context, store *spool.Store, target bootstrapTarget, config PipelineConfig) (bootstrapState, error) {
	sequences, err := store.Sequences()
	if err != nil {
		return bootstrapState{}, err
	}
	if !slices.Equal(sequences, []uint64{bootstrapPreparedSequence}) &&
		!slices.Equal(sequences, []uint64{bootstrapPreparedSequence, bootstrapCompletedSequence}) {
		return bootstrapState{}, herror.NewError("CDC snapshot was interrupted, start a new generation with a clean Doris database and buffer directory")
	}
	payload, err := store.Read(bootstrapPreparedSequence)
	if err != nil {
		return bootstrapState{}, err
	}
	var record bootstrapRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return bootstrapState{}, herror.WrapMsg(err, "decode CDC snapshot checkpoint")
	}
	if len(sequences) == 2 {
		completed, err := store.Read(bootstrapCompletedSequence)
		if err != nil {
			return bootstrapState{}, err
		}
		if !record.Prepared || !bytes.Equal(completed, payload) {
			return bootstrapState{}, herror.NewError("CDC completed snapshot does not match its prepared state")
		}
	} else if record.Prepared {
		if err := completeBootstrap(ctx, store, target, config, record.bootstrapState, payload); err != nil {
			return bootstrapState{}, err
		}
		return record.bootstrapState, nil
	}
	// 原格式只保存序号 1，存在该记录即表示完成。
	checkpoint, err := target.LoadCheckpoint(ctx, config.Identity)
	if err != nil {
		return bootstrapState{}, err
	}
	if checkpoint == nil {
		return bootstrapState{}, herror.NewError("Doris has no checkpoint for the completed local snapshot")
	}
	return record.bootstrapState, nil
}

// completeBootstrap 仅接受未发布基线，或属于原快照的精确序号零检查点。
func completeBootstrap(ctx context.Context, store *spool.Store, target bootstrapTarget, config PipelineConfig, state bootstrapState, payload []byte) error {
	expected := Checkpoint{Identity: config.Identity, Cursor: state.Cursor}
	checkpoint, err := target.LoadCheckpoint(ctx, config.Identity)
	if err != nil {
		return err
	}
	if checkpoint != nil && *checkpoint != expected {
		return herror.NewError("Doris checkpoint conflicts with the prepared CDC snapshot baseline")
	}
	catalog, err := snapshotCatalog(state)
	if err != nil {
		return err
	}
	proof := expected
	proof.StructureRevision = digest(catalog)
	if err := target.verifySchemaRevision(ctx, proof); err != nil {
		return herror.Wrap(fmt.Errorf("verify prepared CDC snapshot in its original Doris target: %w", err))
	}
	delivery, err := openPipelineDelivery(ctx, config, target, state.Cursor)
	if err != nil {
		return err
	}
	progress := delivery.Status()
	if err := delivery.Close(); err != nil {
		return err
	}
	if progress.Delivered != expected || progress.Durable != expected {
		return herror.NewError("CDC delivery progressed beyond the prepared snapshot baseline")
	}
	if err := store.Append(bootstrapCompletedSequence, payload); err != nil {
		return err
	}
	hlog.Info(ctx, "CDC consistent snapshot delivered source_id=%s file=%s position=%d zero_date_rows=%d",
		config.Identity.SourceID, state.Cursor.File, state.Cursor.Position, state.ZeroDateRows)
	return nil
}

// runSnapshot 取得一致性读视图，检查目标为空后依次导入所选表。
func runSnapshot(ctx context.Context, source *mysqlsource.Source, target *DorisTarget, config PipelineConfig) (_ bootstrapState, resultErr error) {
	ctx = withDorisLoadHistory(ctx, config.Identity, 0)
	snapshot, err := source.BeginSnapshot(ctx)
	if err != nil {
		return bootstrapState{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, snapshot.Close()) }()

	layouts, err := buildTableLayouts(config, snapshot.Tables)
	if err != nil {
		return bootstrapState{}, err
	}
	for _, layout := range layouts {
		if err := target.EnsureTable(ctx, layout.target); err != nil {
			return bootstrapState{}, err
		}
		if err := target.VerifyEmptyTable(ctx, layout.target.Database, layout.target.Name); err != nil {
			return bootstrapState{}, err
		}
	}

	state := bootstrapState{Cursor: Cursor(snapshot.Cursor), Tables: snapshot.Tables}
	for _, layout := range layouts {
		mapped, err := snapshotTable(ctx, snapshot, target, config, layout)
		if err != nil {
			return bootstrapState{}, err
		}
		state.ZeroDateRows += mapped
	}
	return state, nil
}

// snapshotTable 按序确认源表的全部导入，返回成功快照中映射了零日期的行数。
func snapshotTable(ctx context.Context, snapshot *mysqlsource.Snapshot, target *DorisTarget, config PipelineConfig, layout tableLayout) (uint64, error) {
	var ordinal uint64
	var zeroDateRows uint64
	scan := func(scanCtx context.Context, consume func(DorisRow) error) error {
		return snapshot.Scan(scanCtx, layout.source, func(values []any) error {
			row, mapped, err := layout.encodeRow(values, false)
			if err != nil {
				return err
			}
			if mapped {
				zeroDateRows++
			}
			return consume(row)
		})
	}
	err := streamSnapshotBatches(ctx, layout, config.SnapshotBatchRows, scan, func(rows []DorisRow) error {
		batch, err := layout.batch(rows)
		if err != nil {
			return err
		}
		labelSeed, err := json.Marshal([]any{config.Identity, config.mappingVersion(), "snapshot", batch.TargetTable, ordinal})
		if err != nil {
			return herror.Wrap(err)
		}
		batch.Checksum = digest(batch.Payload)
		batch.Label = "cdc_snapshot_" + digest(labelSeed)
		if err := deliverSnapshotBatch(ctx, target, batch); err != nil {
			return herror.WrapMsg(err, fmt.Sprintf("snapshot target %s batch %d", batch.TargetTable, ordinal))
		}
		ordinal++
		return nil
	})
	if err != nil {
		return 0, err
	}
	return zeroDateRows, nil
}

// deliverSnapshotBatch 用原批次等待可核对的导入结果，避免暂态故障中断一致性视图。
func deliverSnapshotBatch(ctx context.Context, target Target, batch LoadBatch) error {
	for {
		err := deliverBatch(ctx, target, batch)
		if !errors.Is(err, ErrLoadPending) && !errors.Is(err, errDorisTargetUnavailable) {
			return err
		}
		delay := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			delay.Stop()
			return herror.Wrap(ctx.Err())
		case <-delay.C:
		}
	}
}

// streamSnapshotBatches 按指定行数或默认大小和时限切分扫描结果，并顺序提交所有批次。
func streamSnapshotBatches(ctx context.Context, layout tableLayout, batchRows int,
	scan func(context.Context, func(DorisRow) error) error, submit func([]DorisRow) error,
) error {
	var timer *time.Timer
	var expiry <-chan time.Time
	batchBytes := 0
	maxBytes := defaultDorisLoadBatchBytes
	capacity := 500
	if batchRows > 0 {
		maxBytes = maxDorisCDCLoadBytes
		capacity = min(batchRows, capacity)
	} else {
		timer = time.NewTimer(defaultDorisLoadBatchInterval)
		timer.Stop()
		defer timer.Stop()
	}
	rows := make([]DorisRow, 0, capacity)
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		if timer != nil {
			timer.Stop()
			expiry = nil
		}
		if err := submit(rows); err != nil {
			return err
		}
		clear(rows)
		rows = rows[:0]
		batchBytes = 0
		return nil
	}
	consume := func(row DorisRow) error {
		rowBytes := encodedDorisRowSize(row)
		if err := layout.checkLoadRowSize(rowBytes); err != nil {
			return err
		}
		if batchBytes+rowBytes > maxBytes {
			if err := flush(); err != nil {
				return err
			}
		}
		rows = append(rows, row)
		batchBytes += rowBytes
		if len(rows) == batchRows || (timer != nil && batchBytes >= maxBytes) {
			return flush()
		}
		if timer != nil && len(rows) == 1 {
			timer.Reset(defaultDorisLoadBatchInterval)
			expiry = timer.C
		}
		return nil
	}
	if timer == nil {
		if err := scan(ctx, consume); err != nil {
			return err
		}
		return flush()
	}

	// 源读取可能阻塞。独立计时提交可保留导入顺序和原有读视图。
	// 关闭快照之前等待扫描退出。
	scanCtx, cancel := context.WithCancel(ctx)
	scanned := make(chan DorisRow)
	var scanErr error
	go func() {
		defer close(scanned)
		scanErr = scan(scanCtx, func(row DorisRow) error {
			select {
			case scanned <- row:
				return nil
			case <-scanCtx.Done():
				return herror.Wrap(scanCtx.Err())
			}
		})
	}()
	defer func() {
		cancel()
		for range scanned {
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return herror.Wrap(ctx.Err())
		case <-expiry:
			if err := flush(); err != nil {
				return err
			}
		case row, ok := <-scanned:
			if !ok {
				if scanErr != nil {
					return scanErr
				}
				return flush()
			}
			if err := consume(row); err != nil {
				return err
			}
		}
	}
}
