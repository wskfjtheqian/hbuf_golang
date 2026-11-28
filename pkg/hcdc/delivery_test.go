package hcdc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

type deliveryTestTarget struct {
	checkpoint        *Checkpoint
	persistCheckpoint func(Checkpoint) error
	submitLoad        func(LoadBatch) (LoadConfirmation, error)
	confirmations     map[string]LoadConfirmation
	submitted         []LoadBatch
}

// LoadCheckpoint 从测试目标中读取指定流的检查点。
func (target *deliveryTestTarget) LoadCheckpoint(_ context.Context, identity StreamIdentity) (*Checkpoint, error) {
	if target.checkpoint == nil || target.checkpoint.Identity != identity {
		return nil, nil
	}
	checkpoint := *target.checkpoint
	return &checkpoint, nil
}

// PersistCheckpoint 在测试目标中保存检查点并模拟写入结果。
func (target *deliveryTestTarget) PersistCheckpoint(_ context.Context, checkpoint Checkpoint) error {
	if target.persistCheckpoint != nil {
		return target.persistCheckpoint(checkpoint)
	}
	target.checkpoint = &checkpoint
	return nil
}

// ConfirmLoad 返回测试目标已观测到的批次状态。
func (target *deliveryTestTarget) ConfirmLoad(_ context.Context, batch LoadBatch) (LoadConfirmation, error) {
	if confirmed, exists := target.confirmations[batch.Label]; exists {
		return confirmed, nil
	}
	return LoadConfirmation{State: LoadAbsent, Label: batch.Label, Checksum: batch.Checksum}, nil
}

// SubmitLoad 记录测试提交，并按夹具配置返回导入结果。
func (target *deliveryTestTarget) SubmitLoad(_ context.Context, batch LoadBatch) (LoadConfirmation, error) {
	target.submitted = append(target.submitted, batch)
	if target.submitLoad != nil {
		confirmation, err := target.submitLoad(batch)
		if err == nil && confirmation.State == LoadSucceeded {
			target.confirmations[batch.Label] = confirmation
		}
		return confirmation, err
	}
	confirmation := successfulDeliveryTestLoad(batch)
	target.confirmations[batch.Label] = confirmation
	return confirmation, nil
}

// successfulDeliveryTestLoad 构造与批次身份及行数一致的成功确认。
func successfulDeliveryTestLoad(batch LoadBatch) LoadConfirmation {
	return LoadConfirmation{
		State:      LoadSucceeded,
		Label:      batch.Label,
		Checksum:   batch.Checksum,
		LoadedRows: batch.RowCount,
	}
}

// openDeliveryTest 打开使用临时缓冲目录和测试目标的交付实例。
func openDeliveryTest(t *testing.T) (*Delivery, DeliveryConfig, *deliveryTestTarget) {
	t.Helper()
	config := deliveryTestConfig(t)
	target := &deliveryTestTarget{confirmations: make(map[string]LoadConfirmation)}
	delivery, err := OpenDelivery(t.Context(), config, target)
	require.NoError(t, err)
	t.Cleanup(func() { _ = delivery.Close() })
	return delivery, config, target
}

// deliveryTestConfig 为交付测试构造流身份、映射版本和初始游标。
func deliveryTestConfig(t *testing.T) DeliveryConfig {
	t.Helper()
	return DeliveryConfig{
		Directory: t.TempDir(),
		Identity: StreamIdentity{
			PipelineID: "reports",
			SourceID:   "mysql-a",
			Generation: "initial",
		},
		Baseline: Cursor{
			SourceIdentity: "mysql-a-uuid",
			File:           "mysql-bin.000001",
			Position:       99,
			Flavor:         "mysql",
			GTIDSet:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:1-2,bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb:1",
		},
	}
}

// TestDeliveryDoesNotAdvancePastPartiallyConfirmedTransaction 验证事务的全部批次确认前，交付检查点保持在前一事务。
func TestDeliveryDoesNotAdvancePastPartiallyConfirmedTransaction(t *testing.T) {
	delivery, config, target := openDeliveryTest(t)
	firstCursor, secondCursor := config.Baseline, config.Baseline
	firstCursor.Position = 100
	secondCursor.Position = 200
	first := OrderedRecord{
		Sequence:      1,
		TransactionID: "txn-1",
		Cursor:        firstCursor,
		Batches: []LoadBatch{
			{TargetTable: "report.accounts", RowCount: 1, Payload: []byte("12,open\n")},
			{TargetTable: "report.balances", RowCount: 1, Payload: []byte("12,100\n")},
		},
	}
	second := OrderedRecord{
		Sequence:      2,
		TransactionID: "txn-2",
		Cursor:        secondCursor,
		Batches:       []LoadBatch{{TargetTable: "report.balances", RowCount: 1, Payload: []byte("12,200\n")}},
	}
	require.NoError(t, delivery.AppendRecord(first))
	require.NoError(t, delivery.AppendRecord(second))
	targetFailure := herror.NewError("target unavailable")
	target.submitLoad = func(batch LoadBatch) (LoadConfirmation, error) {
		if batch.TargetTable == "report.balances" {
			return LoadConfirmation{}, targetFailure
		}
		return successfulDeliveryTestLoad(batch), nil
	}

	_, err := delivery.DeliverNext(t.Context())
	require.Error(t, err)
	require.Len(t, target.submitted, 2)
	require.Equal(t, "report.accounts", target.submitted[0].TargetTable)
	require.Equal(t, "report.balances", target.submitted[1].TargetTable)
	require.Equal(t, config.Baseline, target.checkpoint.Cursor)
	require.Equal(t, config.Baseline, delivery.Status().Delivered.Cursor)
	require.Equal(t, secondCursor, delivery.Status().Durable.Cursor)

	target.submitLoad = nil
	progress, err := delivery.DeliverNext(t.Context())
	require.NoError(t, err)
	require.True(t, progress)
	require.Len(t, target.submitted, 3)
	require.NotEmpty(t, target.submitted[1].Label)
	require.NotEmpty(t, target.submitted[1].Checksum)
	require.Equal(t, target.submitted[1], target.submitted[2])
	require.Equal(t, firstCursor, target.checkpoint.Cursor)

	progress, err = delivery.DeliverNext(t.Context())
	require.NoError(t, err)
	require.True(t, progress)
	require.Len(t, target.submitted, 4)
	require.Equal(t, secondCursor, target.checkpoint.Cursor)
	require.NotEqual(t, target.submitted[2].Label, target.submitted[3].Label)
}

// TestDeliveryRetriesCheckpointAfterReopenWithoutResubmitting 验证检查点写入重试保留原导入结果和零日期计数。
func TestDeliveryRetriesCheckpointAfterReopenWithoutResubmitting(t *testing.T) {
	delivery, config, target := openDeliveryTest(t)
	cursor := config.Baseline
	cursor.Position = 210
	cursor.GTIDSet = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:1-4,bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb:1-3"
	require.NoError(t, delivery.AppendRecord(OrderedRecord{
		Sequence:      1,
		TransactionID: "txn-1",
		Cursor:        cursor,
		ZeroDateRows:  1,
		Batches:       []LoadBatch{{TargetTable: "report.balances", RowCount: 1, Payload: []byte("12,210\n")}},
	}))
	target.persistCheckpoint = func(Checkpoint) error {
		return herror.NewError("checkpoint write failed")
	}

	_, err := delivery.DeliverNext(t.Context())
	require.Error(t, err)
	require.Len(t, target.submitted, 1)
	require.Equal(t, config.Baseline, target.checkpoint.Cursor)
	require.Zero(t, target.checkpoint.ZeroDateRows)
	require.Zero(t, delivery.Status().Delivered.ZeroDateRows)
	require.EqualValues(t, 1, delivery.Status().Durable.ZeroDateRows)
	require.NoError(t, delivery.Close())

	target.persistCheckpoint = nil
	reopened, err := OpenDelivery(t.Context(), config, target)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	require.Equal(t, cursor, reopened.Status().Durable.Cursor)
	require.Equal(t, config.Baseline, reopened.Status().Delivered.Cursor)
	require.EqualValues(t, 1, reopened.Status().Durable.ZeroDateRows)
	require.Zero(t, reopened.Status().Delivered.ZeroDateRows)

	progress, err := reopened.DeliverNext(t.Context())
	require.NoError(t, err)
	require.True(t, progress)
	require.Len(t, target.submitted, 1)
	require.Equal(t, cursor, target.checkpoint.Cursor)
	require.Equal(t, cursor, reopened.Status().Delivered.Cursor)
	require.EqualValues(t, 1, target.checkpoint.ZeroDateRows)
	require.NoError(t, reopened.Close())

	confirmed, err := OpenDelivery(t.Context(), config, target)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, confirmed.Close()) })
	require.EqualValues(t, 1, confirmed.Status().Durable.ZeroDateRows)
	require.EqualValues(t, 1, confirmed.Status().Delivered.ZeroDateRows)

	next := OrderedRecord{Sequence: 2, TransactionID: "txn-2", Cursor: cursor}
	next.Cursor.Position++
	require.Error(t, confirmed.AppendRecord(next))
	require.Equal(t, cursor, confirmed.Status().Durable.Cursor)
	next.ZeroDateRows = 1
	require.NoError(t, confirmed.AppendRecord(next))
	progress, err = confirmed.DeliverNext(t.Context())
	require.NoError(t, err)
	require.True(t, progress)
	require.Equal(t, next.Cursor, target.checkpoint.Cursor)
	require.EqualValues(t, 1, target.checkpoint.ZeroDateRows)
	require.Len(t, target.submitted, 1)
}

// TestDeliveryReadsBackCheckpointAfterResponseLost 验证检查点写入响应丢失后可读回确认位点和零日期计数。
func TestDeliveryReadsBackCheckpointAfterResponseLost(t *testing.T) {
	delivery, config, target := openDeliveryTest(t)
	cursor := config.Baseline
	cursor.Position = 150
	require.NoError(t, delivery.AppendRecord(OrderedRecord{
		Sequence:      1,
		TransactionID: "txn-1",
		Cursor:        cursor,
		ZeroDateRows:  1,
		Batches:       []LoadBatch{{TargetTable: "report.accounts", RowCount: 1, Payload: []byte("12,open\n")}},
	}))
	target.persistCheckpoint = func(checkpoint Checkpoint) error {
		target.checkpoint = &checkpoint
		return herror.NewError("checkpoint response lost")
	}

	progress, err := delivery.DeliverNext(t.Context())
	require.NoError(t, err)
	require.True(t, progress)
	require.Len(t, target.submitted, 1)
	require.Equal(t, cursor, delivery.Status().Delivered.Cursor)
	require.EqualValues(t, 1, delivery.Status().Delivered.ZeroDateRows)

	progress, err = delivery.DeliverNext(t.Context())
	require.NoError(t, err)
	require.False(t, progress)
	require.Len(t, target.submitted, 1)
	require.EqualValues(t, 1, delivery.Status().Delivered.ZeroDateRows)
}

// TestDeliveryRecoversPendingRecordsAcrossGTIDMemberChange 验证同代次切换成员后能继续交付已落盘的待确认记录。
func TestDeliveryRecoversPendingRecordsAcrossGTIDMemberChange(t *testing.T) {
	ctx := t.Context()
	config := deliveryTestConfig(t)
	config.GTIDAutoPosition = true
	target := &deliveryTestTarget{confirmations: make(map[string]LoadConfirmation)}
	delivery, err := OpenDelivery(ctx, config, target)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, delivery.Close()) })

	primary := config.Baseline
	primary.Position++
	primary.GTIDSet = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:1-3,bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb:1"
	promoted := primary
	promoted.SourceIdentity = "mysql-b-uuid"
	promoted.File, promoted.Position = "promoted-bin.000001", 45
	promoted.GTIDSet = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:1-4,bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb:1"
	for _, record := range []OrderedRecord{
		{Sequence: 1, TransactionID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:3", Cursor: primary,
			Batches: []LoadBatch{{TargetTable: "report.accounts", RowCount: 1, Payload: []byte("12,old\n")}}},
		{Sequence: 2, TransactionID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:4", Cursor: promoted,
			Batches: []LoadBatch{{TargetTable: "report.accounts", RowCount: 1, Payload: []byte("12,new\n")}}},
	} {
		require.NoError(t, delivery.AppendRecord(record))
	}
	require.NoError(t, delivery.Close())
	physicalConfig := config
	physicalConfig.GTIDAutoPosition = false
	reopened, err := OpenDelivery(ctx, physicalConfig, target)
	require.ErrorContains(t, err, "cursor mode")
	require.Nil(t, reopened)

	reopened, err = OpenDelivery(ctx, config, target)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	require.Equal(t, promoted, reopened.Status().Durable.Cursor)
	require.Equal(t, config.Baseline, reopened.Status().Delivered.Cursor)
	for range 2 {
		progress, err := reopened.DeliverNext(ctx)
		require.NoError(t, err)
		require.True(t, progress)
	}
	require.NoError(t, reopened.Close())
	confirmed, err := OpenDelivery(ctx, config, target)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, confirmed.Close()) })
	require.Equal(t, promoted, confirmed.Status().Delivered.Cursor)
	progress, err := confirmed.DeliverNext(ctx)
	require.NoError(t, err)
	require.False(t, progress)
	require.Len(t, target.submitted, 2)
}

// TestGTIDCursorRejectsMissingOrUnrelatedAdvancement 验证自动定位会拒绝缺少目标事务或推进到无关事务的 GTID 集。
func TestGTIDCursorRejectsMissingOrUnrelatedAdvancement(t *testing.T) {
	config := deliveryTestConfig(t)
	previous := config.Baseline
	promoted := previous
	promoted.SourceIdentity = "mysql-b-uuid"
	promoted.File, promoted.Position = "promoted-bin.000001", 40

	require.ErrorContains(t, validateCursorAdvance(previous, promoted, true), "must advance the committed GTID set")
	promoted.GTIDSet = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:1-4"
	require.ErrorContains(t, validateCursorAdvance(previous, promoted, true), "does not include previously committed transactions")
	promoted.GTIDSet += ",bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb:1"
	require.ErrorContains(t, validateCursorProgress(previous, promoted, true, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:3"),
		"advance by exactly its transaction GTID")
	require.ErrorContains(t, validateCursorProgress(previous, promoted, true, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:3-4"),
		"requires one MySQL GTID")
	require.ErrorContains(t, validateCursorProgress(previous, promoted, true, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:2"),
		"repeated an already committed GTID")
	promoted.GTIDSet = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:1-3,bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb:1"
	require.NoError(t, validateCursorProgress(previous, promoted, true, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa:3"))
	require.Error(t, validateAdvance(previous, promoted))
	promoted.SourceIdentity = previous.SourceIdentity
	promoted.File = previous.File
	require.ErrorContains(t, validateCursorAdvance(previous, promoted, true), "advance the physical binlog position")
}

// TestSchemaHistoryRestagesUncommittedDDLAfterPromotion 验证切换成员后可重新暂存未提交 DDL 的结构版本。
func TestSchemaHistoryRestagesUncommittedDDLAfterPromotion(t *testing.T) {
	store, err := spool.Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	const sid = "356c87a0-3884-11ef-9047-00155d447001"
	baseline := bootstrapState{Cursor: Cursor{
		SourceIdentity: "orders/primary", File: "primary.000008", Position: 400,
		Flavor: "mysql", GTIDSet: sid + ":1-5",
	}}
	staged := schemaVersion{Sequence: 1, Cursor: baseline.Cursor}
	staged.Cursor.GTIDSet, staged.Cursor.Position = sid+":1-6", 800
	previous, err := json.Marshal(staged)
	require.NoError(t, err)
	require.NoError(t, store.Append(staged.Sequence, previous))
	_, _, err = readSchemaVersion(store, baseline, Checkpoint{Cursor: baseline.Cursor}, true)
	require.NoError(t, err)

	staged.Cursor.SourceIdentity, staged.Cursor.File, staged.Cursor.Position = "orders/promoted", "replica.000001", 200
	contents, err := json.Marshal(staged)
	require.NoError(t, err)
	require.NoError(t, store.Append(staged.Sequence, contents))
	recovered, revision, err := readSchemaVersion(store, baseline, Checkpoint{
		Sequence: staged.Sequence, Cursor: staged.Cursor, StructureRevision: digest(contents),
	}, true)
	require.NoError(t, err)
	require.Equal(t, staged, recovered)
	require.Equal(t, digest(contents), revision)
}
