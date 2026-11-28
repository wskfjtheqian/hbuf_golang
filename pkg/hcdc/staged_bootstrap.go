package hcdc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// prepareStagedBootstrap 恢复已完成的基线，或从完整落盘的读视图按原 Label 重放全量。
func prepareStagedBootstrap(ctx context.Context, store *spool.Store, source *mysqlsource.Source, target *DorisTarget, config pipelineConfig, manifest bootstrapManifest) (_ bootstrapState, resultErr error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return bootstrapState{}, herror.Wrap(err)
	}
	stored, err := store.Metadata()
	if err != nil {
		return bootstrapState{}, err
	}
	if stored != nil && !bytes.Equal(stored, encoded) {
		return bootstrapState{}, herror.NewError("CDC bootstrap belongs to a different stream or table mapping")
	}
	sequences, err := store.Sequences()
	if err != nil {
		return bootstrapState{}, err
	}
	if stored == nil && len(sequences) != 0 {
		return bootstrapState{}, herror.NewError("CDC bootstrap manifest is missing for an existing snapshot record")
	}
	if len(sequences) != 0 {
		state, err := restoreBootstrap(ctx, store, target, config)
		if err == nil {
			cleanupTableSnapshotSpool(ctx, config)
		}
		return state, err
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

	staged, err := openTableSnapshotSpool(config)
	if err != nil {
		return bootstrapState{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, staged.Close()) }()
	recovering := staged.Prepared()
	if !recovering {
		if err := target.verifyNoSnapshotLoads(ctx, config.Identity); err != nil {
			return bootstrapState{}, err
		}
	}
	state, err := staged.Prepare(ctx, source, func(state bootstrapState) error {
		// 封存前只允许创建和校验空表，业务行在封存成功后才可发送。
		layouts, err := stagedSnapshotLayouts(config, state)
		if err != nil {
			return err
		}
		for _, layout := range layouts {
			if err := target.EnsureTable(ctx, layout.target); err != nil {
				return err
			}
			if err := target.VerifyEmptyTable(ctx, layout.target.Database, layout.target.Name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return bootstrapState{}, err
	}
	if !config.Source.GTIDAutoPosition && state.Cursor.SourceIdentity != source.Identity() {
		return bootstrapState{}, herror.NewError("CDC staged snapshot belongs to a different physical source member")
	}
	layouts, err := stagedSnapshotLayouts(config, state)
	if err != nil {
		return bootstrapState{}, err
	}
	if recovering {
		for _, layout := range layouts {
			if err := target.verifyStagedSnapshotTable(ctx, layout.target); err != nil {
				return bootstrapState{}, err
			}
		}
		hlog.Info(ctx, "CDC 正在恢复已封存的快照 source_id=%s tables=%d", config.Identity.SourceID, len(layouts))
	}

	loadCtx := withDorisLoadHistory(ctx, config.Identity, 0)
	expectedRows := make(map[string]uint64, len(layouts))
	for _, layout := range layouts {
		expectedRows[layout.target.Database+"."+layout.target.Name] = 0
	}
	if err := staged.Replay(loadCtx, func(batch LoadBatch) error {
		count, exists := expectedRows[batch.TargetTable]
		if !exists {
			return herror.NewError("CDC staged snapshot batch is outside its target tables")
		}
		if batch.RowCount > math.MaxUint64-count {
			return herror.NewError("CDC staged snapshot row count overflow")
		}
		expectedRows[batch.TargetTable] = count + batch.RowCount
		return deliverSnapshotBatch(loadCtx, recoverableSnapshotTarget{target}, batch)
	}); err != nil {
		return bootstrapState{}, err
	}
	for _, layout := range layouts {
		if err := target.verifyStagedSnapshotRows(ctx, layout.target, expectedRows[layout.target.Database+"."+layout.target.Name]); err != nil {
			return bootstrapState{}, err
		}
	}
	if err := publishBootstrap(ctx, store, target, config, state); err != nil {
		return bootstrapState{}, err
	}
	if err := staged.Cleanup(); err != nil {
		hlog.Warn(ctx, "CDC 快照缓冲回收待重试 source_id=%s error=%v", config.Identity.SourceID, err)
	}
	return state, nil
}

// stagedSnapshotLayouts 核对快照表与启动清单的源库、名称和顺序一致。
func stagedSnapshotLayouts(config pipelineConfig, state bootstrapState) ([]tableLayout, error) {
	selected := slices.Sorted(slices.Values(config.Source.Tables))
	if len(state.Tables) != len(selected) {
		return nil, herror.NewError("CDC staged snapshot does not match its source tables")
	}
	for i, table := range state.Tables {
		if table.Ref.Schema != config.Source.Database || table.Ref.Name != selected[i] {
			return nil, herror.NewError("CDC staged snapshot does not match its source tables")
		}
	}
	return buildTableLayouts(config, state.Tables)
}

// verifyNoSnapshotLoads 防止把旧格式留下的部分全量当成未开始的新快照。
func (d *DorisTarget) verifyNoSnapshotLoads(ctx context.Context, identity StreamIdentity) error {
	query := "SELECT 1 FROM " + d.controlTable("cdc_load_history") + " WHERE stream_key = ? AND sequence_no = 0 LIMIT 1"
	var found int
	err := d.db.QueryRowContext(ctx, query, dorisStreamKey(identity)).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return wrapDorisRequestError(err, "read CDC snapshot load intents")
	}
	return herror.NewError("CDC snapshot load intents exist without a sealed local snapshot")
}

// verifyStagedSnapshotTable 只读核对已有目标表，避免复用已确认回执时把丢失的表重建为空表。
func (d *DorisTarget) verifyStagedSnapshotTable(ctx context.Context, table DorisTable) error {
	qualified := quoteDoris(table.Database) + "." + quoteDoris(table.Name)
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

// verifyStagedSnapshotRows 在发布基线前确认该独占目标表的实际行数与封存批次一致。
func (d *DorisTarget) verifyStagedSnapshotRows(ctx context.Context, table DorisTable, expected uint64) error {
	qualified := quoteDoris(table.Database) + "." + quoteDoris(table.Name)
	var actual uint64
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&actual); err != nil {
		return wrapDorisRequestError(err, "count restored CDC snapshot rows")
	}
	if actual != expected {
		return herror.NewError(fmt.Sprintf("CDC snapshot row count differs in %s: expected %d, observed %d", qualified, expected, actual))
	}
	return nil
}

// cleanupTableSnapshotSpool 补做已确认基线的批次文件回收，兼容未使用暂存的旧快照。
func cleanupTableSnapshotSpool(ctx context.Context, config pipelineConfig) {
	if _, err := os.Stat(filepath.Join(config.Directory, tableSnapshotSpoolDirectory)); errors.Is(err, fs.ErrNotExist) {
		return
	}
	staged, err := openTableSnapshotSpool(config)
	if err == nil {
		err = errors.Join(staged.Cleanup(), staged.Close())
	}
	if err != nil {
		hlog.Warn(ctx, "CDC 快照缓冲回收待重试 source_id=%s error=%v", config.Identity.SourceID, err)
	}
}
