package hcdc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const (
	tableSnapshotSpoolDirectory = "snapshot-batches"
	legacyTableSnapshotFormat   = 1
	tableSnapshotFormat         = 2
	tableSnapshotBatchHeaderMax = 4096
	// CSV 写入 JSON 时每字节最多扩展六倍，另留空间给列映射和批次头。
	tableSnapshotRecordMax = 6*maxDorisCDCLoadBytes + (16 << 20)
)

type tableSnapshotManifest struct {
	Format         int
	BootstrapHash  string
	TargetEndpoint string
	ControlDB      string
	FrontendURL    string
	StreamLoadURL  string
	State          bootstrapState
	Batches        uint64
	// BatchesByTable 按 State.Tables 顺序记录各表的批次数，空表为零。
	BatchesByTable []uint64 `json:",omitempty"`
}

type tableSnapshotBatchHeader struct {
	TargetTable string
	RowCount    uint64
	Label       string
	Checksum    string
}

type tableSnapshotSpool struct {
	store        *spool.Store
	config       pipelineConfig
	bootstrap    bootstrapManifest
	sourceTables []mysqlsource.TableRef
	manifest     tableSnapshotManifest
}

// openTableSnapshotSpool 打开快照暂存区并核对已发布的快照清单。
func openTableSnapshotSpool(config pipelineConfig) (_ *tableSnapshotSpool, resultErr error) {
	bootstrap, tables, err := validatePipelineConfig(config)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(bootstrap)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	owner, err := pipelineOwner(config)
	if err != nil {
		return nil, err
	}
	store, err := spool.Open(filepath.Join(config.Directory, tableSnapshotSpoolDirectory))
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, store.Close())
		}
	}()
	s := &tableSnapshotSpool{
		store: store, config: config, bootstrap: bootstrap, sourceTables: tables,
		manifest: tableSnapshotManifest{
			BootstrapHash: digest(encoded), TargetEndpoint: owner.targetEndpoint, ControlDB: owner.controlDatabase,
			FrontendURL: config.Doris.FrontendURL, StreamLoadURL: config.Doris.StreamLoadURL,
		},
	}
	metadata, err := store.Metadata()
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		return s, nil
	}
	if err := s.loadManifest(metadata); err != nil {
		return nil, err
	}
	return s, nil
}

// Close 释放暂存区的排他锁，保留已发布的快照批次。
func (s *tableSnapshotSpool) Close() error { return s.store.Close() }

// Prepared 表示完整读视图及全部批次的清单已经持久化。
func (s *tableSnapshotSpool) Prepared() bool {
	return s.manifest.Format == tableSnapshotFormat || s.manifest.Format == legacyTableSnapshotFormat
}

// Prepare 重用已封存的快照，或在核验目标为空之前采集完整的新快照。
// verifyFresh 可以创建并校验空表；业务导入必须等 Prepare 成功之后才开始。
func (s *tableSnapshotSpool) Prepare(ctx context.Context, source *mysqlsource.Source, verifyFresh func(bootstrapState) error) (bootstrapState, error) {
	if err := ctx.Err(); err != nil {
		return bootstrapState{}, herror.Wrap(err)
	}
	if s.Prepared() {
		return s.manifest.State, nil
	}
	if err := s.startCapture(); err != nil {
		return bootstrapState{}, err
	}
	if s.Prepared() {
		return s.manifest.State, nil
	}
	if verifyFresh == nil {
		return bootstrapState{}, herror.NewError("CDC staged snapshot requires fresh target verification")
	}
	manifest := s.manifest
	state, err := s.stage(ctx, source, &manifest)
	if err != nil {
		return bootstrapState{}, err
	}
	if err := verifyFresh(state); err != nil {
		return bootstrapState{}, err
	}
	if err := ctx.Err(); err != nil {
		return bootstrapState{}, herror.Wrap(err)
	}
	manifest.Format, manifest.State = tableSnapshotFormat, state
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return bootstrapState{}, herror.Wrap(err)
	}
	if _, err := s.store.Bind(encoded); err != nil {
		return bootstrapState{}, err
	}
	s.manifest = manifest
	return state, nil
}

// startCapture 核对上次封存结果，只回收尚未封存、从未获准发送的批次。
func (s *tableSnapshotSpool) startCapture() error {
	metadata, err := s.store.Metadata()
	if err != nil {
		return err
	}
	if metadata == nil {
		return s.store.RemoveThrough(math.MaxUint64)
	}
	// 同一实例的上一次 Bind 可能已发布清单，但未能确认目录同步。
	if _, err := s.store.Bind(metadata); err != nil {
		return err
	}
	return s.loadManifest(metadata)
}

// loadManifest 校验清单属于当前流、源表和目标数据库，再装载持久读视图。
func (s *tableSnapshotSpool) loadManifest(metadata []byte) error {
	var prepared tableSnapshotManifest
	if err := json.Unmarshal(metadata, &prepared); err != nil {
		return herror.WrapMsg(err, "decode staged CDC snapshot manifest")
	}
	if prepared.BootstrapHash != s.manifest.BootstrapHash ||
		prepared.TargetEndpoint != s.manifest.TargetEndpoint || prepared.ControlDB != s.manifest.ControlDB ||
		prepared.FrontendURL != s.manifest.FrontendURL || prepared.StreamLoadURL != s.manifest.StreamLoadURL {
		return herror.NewError("CDC staged snapshot belongs to a different stream, mapping or Doris target")
	}
	if err := s.validateState(prepared.State); err != nil {
		return err
	}
	if _, err := s.batchCounts(prepared); err != nil {
		return err
	}
	s.manifest = prepared
	return nil
}

// stage 在同一个读视图内按表保存原始批次，扫描结束后释放读视图。
func (s *tableSnapshotSpool) stage(ctx context.Context, source *mysqlsource.Source, manifest *tableSnapshotManifest) (_ bootstrapState, resultErr error) {
	snapshot, err := source.BeginSnapshot(ctx)
	if err != nil {
		return bootstrapState{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, snapshot.Close()) }()
	state := bootstrapState{Cursor: Cursor(snapshot.Cursor), Tables: snapshot.Tables}
	if err := s.validateState(state); err != nil {
		return bootstrapState{}, err
	}
	layouts, err := buildTableLayouts(s.config, snapshot.Tables)
	if err != nil {
		return bootstrapState{}, err
	}
	manifest.BatchesByTable = make([]uint64, len(layouts))
	for i, layout := range layouts {
		targetTable := s.targetTable(i)
		mapped, err := scanSnapshotTable(ctx, snapshot, s.config, layout, func(batch LoadBatch) error {
			if manifest.Batches == math.MaxUint64 {
				return herror.NewError("CDC staged snapshot has too many batches")
			}
			if err := s.validateBatch(batch, targetTable, manifest.BatchesByTable[i]); err != nil {
				return err
			}
			payload, err := encodeTableSnapshotBatch(batch)
			if err != nil {
				return err
			}
			manifest.Batches++
			manifest.BatchesByTable[i]++
			return s.store.Append(manifest.Batches, payload)
		})
		if err != nil {
			return bootstrapState{}, err
		}
		if state.ZeroDateRows > math.MaxUint64-mapped {
			return bootstrapState{}, herror.NewError("CDC staged snapshot zero-date row count overflow")
		}
		state.ZeroDateRows += mapped
	}
	return state, nil
}

// validateState 核对完整源表清单及其稳定顺序、Binlog 位点和定位模式。
func (s *tableSnapshotSpool) validateState(state bootstrapState) error {
	if len(state.Tables) != len(s.sourceTables) || state.Cursor.Flavor != s.bootstrap.Flavor ||
		!strings.HasPrefix(state.Cursor.SourceIdentity, s.bootstrap.SourceIdentity+"/") {
		return herror.NewError("CDC staged snapshot has an unexpected source identity or table set")
	}
	for i, table := range state.Tables {
		if table.Ref != s.sourceTables[i] {
			return herror.NewError("CDC staged snapshot does not match its authorized table order")
		}
	}
	_, _, set, err := parseCursor(state.Cursor)
	if err != nil {
		return err
	}
	if s.bootstrap.GTIDAutoPosition {
		return validateGTIDCursor(state.Cursor, set)
	}
	return nil
}

// batchCounts 按清单顺序返回各表的批次数，兼容旧格式的单表记录。
func (s *tableSnapshotSpool) batchCounts(manifest tableSnapshotManifest) ([]uint64, error) {
	if manifest.Format == legacyTableSnapshotFormat && len(s.sourceTables) == 1 && len(manifest.BatchesByTable) == 0 {
		return []uint64{manifest.Batches}, nil
	}
	if manifest.Format != tableSnapshotFormat || len(manifest.BatchesByTable) != len(s.sourceTables) {
		return nil, herror.NewError("CDC staged snapshot has an invalid per-table batch manifest")
	}
	remaining := manifest.Batches
	for _, count := range manifest.BatchesByTable {
		if count > remaining {
			return nil, herror.NewError("CDC staged snapshot per-table batch counts differ from the total")
		}
		remaining -= count
	}
	if remaining != 0 {
		return nil, herror.NewError("CDC staged snapshot per-table batch counts differ from the total")
	}
	return manifest.BatchesByTable, nil
}

// targetTable 根据清单顺序生成 Doris 目标表名。
func (s *tableSnapshotSpool) targetTable(index int) string {
	return s.config.TargetDatabase + "." + s.sourceTables[index].Name
}

// Replay 顺序读回并验证不可变批次，随后调用 Doris 交付逻辑核对并提交。
func (s *tableSnapshotSpool) Replay(ctx context.Context, submit func(LoadBatch) error) error {
	if !s.Prepared() {
		return herror.NewError("CDC staged snapshot is not prepared")
	}
	counts, err := s.batchCounts(s.manifest)
	if err != nil {
		return err
	}
	sequences, err := s.store.Sequences()
	if err != nil {
		return err
	}
	if uint64(len(sequences)) != s.manifest.Batches {
		return herror.NewError("CDC staged snapshot batch count does not match its manifest")
	}
	for i, sequence := range sequences {
		if sequence != uint64(i)+1 {
			return herror.NewError("CDC staged snapshot has a batch sequence gap")
		}
	}
	var sequence uint64
	for i, count := range counts {
		targetTable := s.targetTable(i)
		for ordinal := uint64(0); ordinal < count; ordinal++ {
			if err := ctx.Err(); err != nil {
				return herror.Wrap(err)
			}
			sequence++
			batch, err := s.readBatch(sequence)
			if err != nil {
				return herror.Wrap(fmt.Errorf("read staged CDC snapshot batch %d: %w", sequence, err))
			}
			if err := s.validateBatch(batch, targetTable, ordinal); err != nil {
				return err
			}
			if err := submit(batch); err != nil {
				return herror.Wrap(fmt.Errorf("deliver staged CDC snapshot batch %d: %w", sequence, err))
			}
		}
	}
	return nil
}

// readBatch 校验文件封装后读取一个有大小上限的请求。
func (s *tableSnapshotSpool) readBatch(sequence uint64) (_ LoadBatch, resultErr error) {
	file, size, err := s.store.OpenRecord(sequence)
	if err != nil {
		return LoadBatch{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if size < 4 || size > tableSnapshotRecordMax {
		return LoadBatch{}, herror.NewError("CDC staged snapshot batch size is invalid")
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(file, payload); err != nil {
		return LoadBatch{}, herror.Wrap(err)
	}
	return decodeTableSnapshotBatch(payload)
}

// validateBatch 绑定原始批次内容、目标表和稳定的标签。
func (s *tableSnapshotSpool) validateBatch(batch LoadBatch, targetTable string, ordinal uint64) error {
	if err := validateDorisBatch(batch); err != nil {
		return err
	}
	expected, err := snapshotBatchLabel(s.config, targetTable, ordinal)
	if err != nil {
		return err
	}
	if batch.TargetTable != targetTable || batch.Label != expected {
		return herror.NewError("CDC staged snapshot batch does not match its target or ordinal")
	}
	return nil
}

// Cleanup 回收批次文件。调用方必须已经确认并持久化完整快照检查点。
func (s *tableSnapshotSpool) Cleanup() error {
	return s.store.RemoveThrough(math.MaxUint64)
}

// snapshotBatchLabel 用稳定的流标识、目标和批次序号生成重试标签。
func snapshotBatchLabel(config pipelineConfig, targetTable string, ordinal uint64) (string, error) {
	seed, err := json.Marshal([]any{config.Identity, config.mappingVersion(), "snapshot", targetTable, ordinal})
	if err != nil {
		return "", herror.Wrap(err)
	}
	return "cdc_snapshot_" + digest(seed), nil
}

// encodeTableSnapshotBatch 将短 JSON 头和原始请求字节合并存储，避免重新编码请求。
func encodeTableSnapshotBatch(batch LoadBatch) ([]byte, error) {
	header, err := json.Marshal(tableSnapshotBatchHeader{
		TargetTable: batch.TargetTable, RowCount: batch.RowCount, Label: batch.Label, Checksum: batch.Checksum,
	})
	if err != nil {
		return nil, herror.Wrap(err)
	}
	if len(header) > tableSnapshotBatchHeaderMax || len(batch.Payload) > tableSnapshotRecordMax-4-len(header) {
		return nil, herror.NewError("CDC staged snapshot batch exceeds the record size limit")
	}
	result := make([]byte, 4, 4+len(header)+len(batch.Payload))
	binary.BigEndian.PutUint32(result, uint32(len(header)))
	result = append(result, header...)
	return append(result, batch.Payload...), nil
}

// decodeTableSnapshotBatch 分离已验证的记录头和不可变请求字节。
func decodeTableSnapshotBatch(payload []byte) (LoadBatch, error) {
	if len(payload) < 4 {
		return LoadBatch{}, herror.NewError("CDC staged snapshot batch header is missing")
	}
	size := int(binary.BigEndian.Uint32(payload[:4]))
	if size <= 0 || size > tableSnapshotBatchHeaderMax || size >= len(payload)-4 {
		return LoadBatch{}, herror.NewError("CDC staged snapshot batch header length is invalid")
	}
	var header tableSnapshotBatchHeader
	if err := json.Unmarshal(payload[4:4+size], &header); err != nil {
		return LoadBatch{}, herror.WrapMsg(err, "decode staged CDC snapshot batch header")
	}
	return LoadBatch{
		TargetTable: header.TargetTable, RowCount: header.RowCount, Label: header.Label, Checksum: header.Checksum,
		Payload: payload[4+size:],
	}, nil
}
