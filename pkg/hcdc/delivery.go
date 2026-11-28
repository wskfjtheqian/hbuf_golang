package hcdc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

var (
	// ErrDeliveryClosed 表示缓冲已停止接收记录。
	ErrDeliveryClosed = herror.NewError("CDC delivery is closed")
	// ErrLoadPending 表示目标请求仍待确认。
	ErrLoadPending = herror.NewError("target load is pending confirmation")
	errBufferFull  = herror.NewError("CDC durable buffer reached its byte limit")
	// ErrDDLApprovalRequired 表示队首记录的 DDL 尚未具备执行条件。
	ErrDDLApprovalRequired = herror.NewError("CDC DDL barrier requires approval")
)

// Delivery 缓冲并确认单个源的有序记录。DeliverNext 等待目标时可调用 AppendRecord，
// 交付操作按序执行。
type Delivery struct {
	store          *spool.Store
	target         Target
	config         DeliveryConfig
	manifestHash   string
	ddlApproved    func(string) bool
	validateSchema func(OrderedRecord) error
	persistSchema  func(context.Context, OrderedRecord) error

	deliveryGate chan struct{}
	mu           sync.Mutex
	status       DeliveryStatus
	closing      atomic.Bool
	closeOnce    sync.Once
	closeDone    chan struct{}
	closeErr     error
}

type streamManifest struct {
	Version          int
	Identity         StreamIdentity
	Baseline         Cursor
	MappingVersion   string
	GTIDAutoPosition bool `json:",omitempty"`
}

type bufferedOrderedRecord struct {
	OrderedRecord
	ManifestHash string `json:",omitempty"`
}

// OpenDelivery 独占本地缓冲，校验远端检查点并恢复连续的待交付记录。
// 新代次先写入并校验初始检查点，再接收记录。
func OpenDelivery(ctx context.Context, config DeliveryConfig, target Target) (_ *Delivery, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, herror.Wrap(err)
	}
	if target == nil || config.Directory == "" || config.Identity.PipelineID == "" ||
		config.Identity.SourceID == "" || config.Identity.Generation == "" || config.Baseline.SourceIdentity == "" {
		return nil, herror.NewError("CDC delivery requires a directory, target, stream identity and source identity")
	}
	_, _, baselineSet, err := parseCursor(config.Baseline)
	if err != nil {
		return nil, err
	}
	if config.GTIDAutoPosition {
		if err := validateGTIDCursor(config.Baseline, baselineSet); err != nil {
			return nil, err
		}
	}

	store, err := spool.Open(config.Directory)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			if err := store.Close(); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()

	manifest, err := json.Marshal(streamManifest{
		Version: 1, Identity: config.Identity, Baseline: config.Baseline, MappingVersion: config.MappingVersion,
		GTIDAutoPosition: config.GTIDAutoPosition,
	})
	if err != nil {
		return nil, herror.Wrap(err)
	}
	storedManifest, err := store.Metadata()
	if err != nil {
		return nil, err
	}
	if storedManifest != nil && !bytes.Equal(storedManifest, manifest) {
		return nil, herror.NewError("CDC buffer belongs to a different stream, baseline, mapping version or cursor mode")
	}
	sequences, err := store.Sequences()
	if err != nil {
		return nil, err
	}
	checkpoint, err := target.LoadCheckpoint(ctx, config.Identity)
	if err != nil {
		return nil, herror.WrapMsg(err, "read CDC checkpoint")
	}

	baseline := Checkpoint{Identity: config.Identity, Cursor: config.Baseline}
	if storedManifest == nil {
		if checkpoint != nil || len(sequences) != 0 {
			return nil, herror.NewError("CDC buffer manifest is missing for an existing generation")
		}
		if _, err := store.Bind(manifest); err != nil {
			return nil, err
		}
	}
	// 初始检查点验证后才会发布记录，因此已绑定的空缓冲可以安全重试中断的初始化。
	if checkpoint == nil && len(sequences) == 0 {
		if err := confirmCheckpoint(ctx, target, baseline); err != nil {
			return nil, err
		}
		checkpoint = &baseline
	}

	if checkpoint == nil || checkpoint.Identity != config.Identity ||
		(checkpoint.Sequence == 0 && *checkpoint != baseline) {
		return nil, herror.NewError("CDC checkpoint is missing or belongs to a different generation")
	}
	if checkpoint.Sequence > 0 {
		if !slices.Contains(sequences, checkpoint.Sequence) {
			return nil, herror.NewError("CDC buffer lacks the last confirmed record required to verify recovery")
		}
		if err := validateCursorAdvance(baseline.Cursor, checkpoint.Cursor, config.GTIDAutoPosition); err != nil {
			return nil, err
		}
	}

	delivery := &Delivery{
		store: store, target: target, config: config, manifestHash: digest(manifest),
		deliveryGate: make(chan struct{}, 1),
		status:       DeliveryStatus{Durable: *checkpoint, Delivered: *checkpoint},
	}
	for _, sequence := range sequences {
		if err := ctx.Err(); err != nil {
			return nil, herror.Wrap(err)
		}
		record, size, err := delivery.readRecord(sequence, *checkpoint)
		if err != nil {
			return nil, err
		}
		if sequence < checkpoint.Sequence {
			continue
		}
		if sequence == checkpoint.Sequence {
			if delivery.checkpoint(record) != *checkpoint {
				return nil, herror.NewError("CDC checkpoint does not match the corresponding buffered record")
			}
			continue
		}
		if err := delivery.validateNext(record); err != nil {
			return nil, err
		}
		delivery.status.Durable = delivery.checkpoint(record)
		delivery.status.PendingBytes += size
	}
	if checkpoint.Sequence > 0 {
		if err := store.RemoveThrough(checkpoint.Sequence - 1); err != nil {
			return nil, err
		}
	}
	return delivery, nil
}

// AppendRecord 持久化下一条完整的已提交记录，拒绝序号缺口和游标倒退。
// 追加失败时可用同一记录重试，也可重新打开缓冲以恢复中断的发布。
func (d *Delivery) AppendRecord(record OrderedRecord) error {
	return d.appendRecord(record, 0)
}

// appendRecord 在容量限制内校验并持久化下一条已提交记录。
func (d *Delivery) appendRecord(record OrderedRecord, limit int64) error {
	if d.closing.Load() {
		return ErrDeliveryClosed
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closing.Load() {
		return ErrDeliveryClosed
	}
	if err := d.validateNext(record); err != nil {
		return err
	}
	if err := validateRecord(record); err != nil {
		return err
	}
	record.Batches = slices.Clone(record.Batches)
	for index := range record.Batches {
		batch := &record.Batches[index]
		batch.Checksum = digest(batch.Payload)
		label, err := d.batchLabel(record, batch.TargetTable, index)
		if err != nil {
			return err
		}
		batch.Label = label
	}
	payload, err := json.Marshal(bufferedOrderedRecord{OrderedRecord: record, ManifestHash: d.manifestHash})
	if err != nil {
		return herror.Wrap(err)
	}
	if limit > 0 && int64(len(payload)) > limit {
		return herror.Wrap(fmt.Errorf("%w: encoded durable record limit %d bytes", ErrTransactionTooLarge, limit))
	}
	if limit > 0 && d.status.PendingBytes > limit-int64(len(payload)) {
		return errBufferFull
	}
	if err := d.store.Append(record.Sequence, payload); err != nil {
		return err
	}
	d.status.Durable = d.checkpoint(record)
	d.status.PendingBytes += int64(len(payload))
	return nil
}

// DeliverNext 确认下一条完整记录并持久化其检查点。
// 目标已接收所有缓冲记录时返回 false 和 nil。
// 确认后清理失败时返回 true 和清理错误，已交付检查点继续有效。
func (d *Delivery) DeliverNext(ctx context.Context) (bool, error) {
	return d.deliverNext(ctx, nil)
}

// deliverNext 让流水线检查队首记录并申请派发，复用同一次读取结果。
// 调用 permit 时已释放缓冲锁。
func (d *Delivery) deliverNext(ctx context.Context, permit func(*OrderedRecord) bool) (bool, error) {
	if d.closing.Load() {
		return false, ErrDeliveryClosed
	}
	select {
	case d.deliveryGate <- struct{}{}:
		defer func() { <-d.deliveryGate }()
	case <-ctx.Done():
		return false, herror.Wrap(ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return false, herror.Wrap(err)
	}

	record, previous, size, err := d.nextRecord()
	if err != nil {
		return false, err
	}
	if (permit != nil && !permit(record)) || record == nil {
		return false, nil
	}
	if hlog.FromContext(ctx) == "" {
		ctx = hlog.WithContext(ctx, hlog.NewTraceId())
	}
	next := d.checkpoint(*record)
	ctx = withDorisLoadHistory(ctx, next.Identity, next.Sequence)
	known, err := d.target.LoadCheckpoint(ctx, d.config.Identity)
	if err != nil {
		return false, herror.WrapMsg(err, "read checkpoint before CDC delivery")
	}
	if known == nil || (*known != previous && *known != next) {
		return false, herror.NewError("CDC checkpoint changed outside the current delivery operation")
	}

	if *known != next {
		if record.DDL != nil {
			if err := ctx.Err(); err != nil {
				return false, herror.Wrap(err)
			}
			if record.DDL.Kind == DDLUnsupported ||
				(record.DDL.RequiresApproval && (d.ddlApproved == nil || !d.ddlApproved(record.DDL.ID))) {
				return false, ErrDDLApprovalRequired
			}
			target, ok := d.target.(interface {
				ApplyDDL(context.Context, DDLPlan) error
			})
			if !ok {
				return false, herror.NewError("CDC target does not support ordered DDL")
			}
			if d.validateSchema != nil {
				if err := d.validateSchema(*record); err != nil {
					return false, err
				}
			}
			if err := target.ApplyDDL(ctx, *record.DDL); err != nil {
				return false, err
			}
			if d.persistSchema != nil {
				if err := d.persistSchema(ctx, *record); err != nil {
					return false, err
				}
			}
		}
		if len(record.Batches) > 0 {
			if err := d.replayRecordBatches(ctx, next.Sequence); err != nil {
				return false, err
			}
		}
		if err := confirmCheckpoint(ctx, d.target, next); err != nil {
			return false, err
		}
	}

	d.mu.Lock()
	d.status.Delivered = next
	d.status.PendingBytes -= size
	// 保留最近确认的记录，以便重新打开缓冲时校验远端检查点。
	var cleanupErr error
	if previous.Sequence != 0 {
		cleanupErr = d.store.Remove(previous.Sequence)
	}
	d.mu.Unlock()
	return true, cleanupErr
}

// nextRecord 读取下一条待交付记录，并校验它与当前检查点的连续性。
func (d *Delivery) nextRecord() (*OrderedRecord, Checkpoint, int64, error) {
	d.mu.Lock()
	previous := d.status.Delivered
	closed, durable := d.closing.Load(), d.status.Durable.Sequence
	d.mu.Unlock()
	if closed {
		return nil, previous, 0, ErrDeliveryClosed
	}
	if durable == previous.Sequence {
		return nil, previous, 0, nil
	}
	record, size, err := d.readRecord(previous.Sequence+1, previous)
	if err != nil {
		return nil, previous, 0, err
	}
	if err := validateCursorProgress(previous.Cursor, record.Cursor, d.config.GTIDAutoPosition, record.TransactionID); err != nil {
		return nil, previous, 0, err
	}
	if err := validateZeroDateProgress(previous.ZeroDateRows, record); err != nil {
		return nil, previous, 0, err
	}
	return &record, previous, size, nil
}

// deliverBatch 查询单批导入的状态，确认可安全提交时使用原 Label 发送。
func deliverBatch(ctx context.Context, target Target, batch LoadBatch) error {
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	confirmed, err := target.ConfirmLoad(ctx, batch)
	if err != nil {
		return herror.WrapMsg(err, "verify CDC load outcome")
	}
	if confirmed.State == LoadAbsent {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		confirmed, err = target.SubmitLoad(ctx, batch)
		if err != nil {
			return herror.WrapMsg(err, "send CDC load batch")
		}
	}
	switch confirmed.State {
	case LoadPending:
		return ErrLoadPending
	case LoadSucceeded:
		if confirmed.Label == batch.Label && confirmed.Checksum == batch.Checksum &&
			confirmed.LoadedRows == batch.RowCount && confirmed.FilteredRows == 0 {
			return nil
		}
		return herror.NewError(fmt.Sprintf("CDC load confirmation did not match the batch load_label=%s expected_rows=%d loaded_rows=%d filtered_rows=%d",
			batch.Label, batch.RowCount, confirmed.LoadedRows, confirmed.FilteredRows))
	default:
		return herror.NewError("CDC target returned an invalid load confirmation state")
	}
}

// confirmCheckpoint 写入检查点并读回校验，在写入结果不明时以读回结果为准。
func confirmCheckpoint(ctx context.Context, target Target, expected Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	writeErr := target.PersistCheckpoint(ctx, expected)
	actual, readErr := target.LoadCheckpoint(ctx, expected.Identity)
	if readErr == nil && actual != nil && *actual == expected {
		if doris, ok := target.(*DorisTarget); ok {
			doris.collectControlHistory(ctx, expected)
		}
		return nil
	}
	if err := errors.Join(writeErr, readErr); err != nil {
		return herror.WrapMsg(err, "persist and verify CDC checkpoint")
	}
	return herror.NewError("CDC checkpoint was not verified after writing")
}

// Status 返回已落盘和已交付进度的一致快照。
func (d *Delivery) Status() DeliveryStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

// Close 等待在途交付结束，再关闭缓冲。
// 若需中断目标请求，请先取消交付上下文。
func (d *Delivery) Close() error {
	return d.CloseContext(context.Background())
}

// CloseContext 发起缓冲关闭，并按调用期限等待。超时后同一关闭操作继续执行。
// 若需中断目标请求，请先取消交付上下文。
func (d *Delivery) CloseContext(ctx context.Context) error {
	d.closeOnce.Do(func() {
		d.closing.Store(true)
		d.closeDone = make(chan struct{})
		go d.closeBuffer()
	})
	select {
	case <-d.closeDone:
		return d.closeErr
	case <-ctx.Done():
		select {
		case <-d.closeDone:
			return d.closeErr
		default:
			return herror.Wrap(ctx.Err())
		}
	}
}

// closeBuffer 释放缓冲并通知所有正在等待的调用者。
func (d *Delivery) closeBuffer() {
	defer close(d.closeDone)
	d.deliveryGate <- struct{}{}
	defer func() { <-d.deliveryGate }()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeErr = d.store.Close()
}

// readRecord 读取指定序号的缓冲记录并校验清单、Label 与校验和。
func (d *Delivery) readRecord(sequence uint64, confirmed Checkpoint) (OrderedRecord, int64, error) {
	file, size, err := d.store.OpenRecord(sequence)
	if err != nil {
		return OrderedRecord{}, 0, err
	}
	defer file.Close()
	buffered, err := decodeBufferedRecord(file, nil)
	if err != nil {
		return OrderedRecord{}, 0, err
	}
	record := buffered.OrderedRecord
	if record.Sequence != sequence {
		return record, 0, herror.NewError("buffered CDC record does not match its sequence")
	}
	if buffered.ManifestHash != d.manifestHash {
		if buffered.ManifestHash != "" || d.config.GTIDAutoPosition {
			return record, 0, herror.NewError("buffered CDC record belongs to a different stream, baseline or mapping version")
		}
		// 原物理位点格式缺少摘要，只核对或回收已经确认的记录。旧待交付记录须先由旧版排空。
		if sequence > confirmed.Sequence {
			return record, 0, herror.NewError("unconfirmed legacy CDC record has no manifest digest; drain it with the compatible version before upgrading")
		}
		if sequence == confirmed.Sequence && d.checkpoint(record) != confirmed {
			return record, 0, herror.NewError("legacy CDC record does not match the verified checkpoint")
		}
	}
	if err := validateRecordHeader(record); err != nil {
		return record, 0, err
	}
	for index, batch := range record.Batches {
		label, err := d.batchLabel(record, batch.TargetTable, index)
		if err != nil {
			return record, 0, err
		}
		if batch.Label != label {
			return record, 0, herror.NewError("buffered CDC load batch has an invalid identity")
		}
	}
	return record, size, nil
}

// replayRecordBatches 在校验完整记录后逐批向目标发送已持久化的导入负载。
func (d *Delivery) replayRecordBatches(ctx context.Context, sequence uint64) error {
	file, _, err := d.store.OpenRecord(sequence)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = decodeBufferedRecord(file, func(batch LoadBatch) error {
		if err := deliverBatch(ctx, d.target, batch); err != nil {
			hlog.Warn(ctx, "CDC load awaiting confirmation source_id=%s sequence=%d load_label=%s",
				d.config.Identity.SourceID, sequence, batch.Label)
			return err
		}
		return nil
	})
	return err
}

// validateRecordHeader 校验有序记录的事务和 DDL 屏障。
func validateRecordHeader(record OrderedRecord) error {
	if strings.TrimSpace(record.TransactionID) == "" {
		return herror.NewError("CDC record requires a transaction ID")
	}
	if plan := record.DDL; plan != nil {
		if plan.ID == "" || plan.Sequence != record.Sequence || plan.Cursor != record.Cursor ||
			record.StructureRevision == "" || len(record.Batches) != 0 {
			return herror.NewError("CDC DDL record requires a matching sequence, cursor and structure revision without data batches")
		}
		switch plan.Kind {
		case DDLAddColumns, DDLUnsupported:
		case DDLTruncate, DDLDropColumn, DDLWidenInteger, DDLRenameColumn:
			if !plan.RequiresApproval {
				return herror.NewError("CDC " + string(plan.Kind) + " record requires an approval barrier")
			}
		default:
			return herror.NewError("CDC record has an unsupported DDL kind")
		}
	}
	return nil
}

// validateRecord 校验有序记录的元数据和导入批次。
func validateRecord(record OrderedRecord) error {
	if err := validateRecordHeader(record); err != nil {
		return err
	}
	for _, batch := range record.Batches {
		if err := validateBatch(batch); err != nil {
			return err
		}
	}
	return nil
}

// validateBatch 校验一批导入的目标表、行数和负载。
func validateBatch(batch LoadBatch) error {
	if strings.TrimSpace(batch.TargetTable) == "" || batch.RowCount == 0 || len(batch.Payload) == 0 {
		return herror.NewError("CDC load batch requires a target table, positive row count and payload")
	}
	return nil
}

// batchLabel 根据流身份、记录和批次序号生成稳定的导入 Label。
func (d *Delivery) batchLabel(record OrderedRecord, targetTable string, index int) (string, error) {
	// 为身份字段划分明确的编码边界。
	labelSeed, err := json.Marshal([]string{
		d.config.Identity.PipelineID, d.config.Identity.SourceID, d.config.Identity.Generation,
		d.config.MappingVersion, strconv.FormatUint(record.Sequence, 10), record.TransactionID,
		targetTable, strconv.Itoa(index),
	})
	if err != nil {
		return "", herror.Wrap(err)
	}
	return "cdc_" + digest(labelSeed), nil
}

// validateNext 校验待追加记录的序号及游标是否连续推进。
func (d *Delivery) validateNext(record OrderedRecord) error {
	durable := d.status.Durable
	if durable.Sequence == math.MaxUint64 || record.Sequence != durable.Sequence+1 {
		return herror.NewError(fmt.Sprintf("CDC sequence %d cannot follow %d", record.Sequence, durable.Sequence))
	}
	if err := validateZeroDateProgress(durable.ZeroDateRows, record); err != nil {
		return err
	}
	return validateCursorProgress(durable.Cursor, record.Cursor, d.config.GTIDAutoPosition, record.TransactionID)
}

// validateZeroDateProgress 核对累计行数单调推进，且增量不超过本记录的导入行数。
func validateZeroDateProgress(previous uint64, record OrderedRecord) error {
	if record.ZeroDateRows < previous {
		return herror.NewError("CDC zero date row count cannot decrease")
	}
	remaining := record.ZeroDateRows - previous
	if remaining == 0 {
		return nil
	}
	for _, batch := range record.Batches {
		if remaining <= batch.RowCount {
			return nil
		}
		remaining -= batch.RowCount
	}
	return herror.NewError("CDC zero date rows exceed the data rows in the record")
}

// checkpoint 从有序记录生成对应的交付检查点。
func (d *Delivery) checkpoint(record OrderedRecord) Checkpoint {
	return Checkpoint{
		Identity: d.config.Identity, Sequence: record.Sequence, Cursor: record.Cursor,
		StructureRevision: record.StructureRevision, ZeroDateRows: record.ZeroDateRows,
	}
}

// digest 返回数据的 SHA-256 十六进制摘要。
func digest(payload []byte) string {
	checksum := sha256.Sum256(payload)
	return hex.EncodeToString(checksum[:])
}
