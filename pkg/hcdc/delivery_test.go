package hcdc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

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

// TestDorisVisibleLoadExpiresWithoutAuditEvidence 验证可见事务缺少审计证据时仍保留结果未明的状态。
func TestDorisVisibleLoadExpiresWithoutAuditEvidence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		payload := []byte("record")
		batch := LoadBatch{
			TargetTable: "warehouse.events", RowCount: 1, Label: "missing_audit",
			Payload: payload, Checksum: digest(payload),
		}
		fixture := &dorisTestDB{batch: batch, intent: true}
		db := sql.OpenDB(fixture)
		defer db.Close()
		endpoint, err := url.Parse("http://doris.example.invalid")
		require.NoError(t, err)
		target := &DorisTarget{
			db: db, frontend: endpoint,
			client: &http.Client{Transport: dorisTestTransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodGet, req.Method)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":"VISIBLE"}`))}, nil
			})},
		}
		start := time.Now()
		_, err = target.ConfirmLoad(t.Context(), batch)
		require.ErrorIs(t, err, ErrDorisLoadOutcomeUnknown)
		require.False(t, fixture.verified)
		require.Greater(t, fixture.auditQueries, 1)
		require.Equal(t, dorisLoadRecheckTimeout, time.Since(start))
	})
}

// TestDorisLoadRetainsRecheckDeadline 验证 UNKNOWN 在重启前后共用原期限，并且始终只查原 Label。
func TestDorisLoadRetainsRecheckDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		id := "42"
		payload, err := EncodeDorisLoad([]string{"id"}, []DorisRow{{Values: []*string{&id}}})
		require.NoError(t, err)
		batch := LoadBatch{TargetTable: "warehouse.events", RowCount: 1, Label: "unknown_load", Payload: payload, Checksum: digest(payload)}
		fixture := &dorisTestDB{batch: batch}
		endpoint, err := url.Parse("http://doris.example.invalid")
		require.NoError(t, err)
		state := "UNKNOWN"
		puts, probes := 0, 0
		client := &http.Client{Transport: dorisTestTransport(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPut {
				puts++
				require.Equal(t, batch.Label, req.Header.Get("label"))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("bad response"))}, nil
			}
			require.Equal(t, http.MethodGet, req.Method)
			require.Equal(t, batch.Label, req.URL.Query().Get("label"))
			probes++
			body := fmt.Sprintf(`{"code":0,"data":%q}`, state)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		start := time.Now()
		db := sql.OpenDB(fixture)
		timeout := 2 * dorisLoadRecheckTimeout
		target := &DorisTarget{db: db, backend: endpoint, frontend: endpoint, client: client, config: DorisConfig{LoadRecheckTimeout: timeout}}
		_, err = target.SubmitLoad(t.Context(), batch)
		require.ErrorIs(t, err, ErrLoadPending)
		require.ErrorIs(t, err, ErrDorisLoadOutcomeUnknown)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, err = target.ConfirmLoad(ctx, batch)
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.NoError(t, db.Close())
		require.Equal(t, start.Add(timeout).UnixMilli(), fixture.deadlineMS)

		reopened := sql.OpenDB(fixture)
		defer reopened.Close()
		target = &DorisTarget{db: reopened, backend: endpoint, frontend: endpoint, client: client}
		fixture.auditChecksum = "different payload"
		_, err = target.ConfirmLoad(t.Context(), batch)
		require.ErrorContains(t, err, "Stream Load audit did not match")
		require.False(t, fixture.verified)
		require.Equal(t, 5*time.Second, time.Since(start))

		fixture.auditChecksum = ""
		_, err = target.SubmitLoad(t.Context(), batch)
		require.ErrorIs(t, err, ErrDorisLoadOutcomeUnknown)
		require.Equal(t, timeout, time.Since(start))
		require.Equal(t, 1, fixture.deadlineWrites)
		require.False(t, fixture.verified)

		previous := probes
		_, err = target.ConfirmLoad(t.Context(), batch)
		require.ErrorIs(t, err, ErrDorisLoadOutcomeUnknown)
		require.Equal(t, previous+1, probes)
		require.Equal(t, timeout, time.Since(start))

		state = "VISIBLE"
		fixture.auditChecksum = batch.Checksum
		confirmation, err := target.ConfirmLoad(t.Context(), batch)
		require.NoError(t, err)
		require.Equal(t, dorisConfirmed(batch), confirmation)
		require.True(t, fixture.verified)
		require.Equal(t, 1, fixture.deadlineWrites)
		require.Equal(t, 1, puts)
	})
}

// TestDorisReplaysOnlySealedSnapshotBatches 验证密封快照仅在原回执允许时补发相同请求。
func TestDorisReplaysOnlySealedSnapshotBatches(t *testing.T) {
	id := "42"
	payload, err := EncodeDorisLoad([]string{"id"}, []DorisRow{{Values: []*string{&id}}})
	require.NoError(t, err)
	batch := LoadBatch{TargetTable: "warehouse.events", RowCount: 1, Label: "cdc_snapshot_original", Payload: payload, Checksum: digest(payload)}
	for _, test := range []struct {
		name, state, auditStatus, auditChecksum, wantError string
		wantPuts                                           int
		verified, priorFailure                             bool
	}{
		{name: "aborted original attempt", state: "ABORTED", auditStatus: "Fail", auditChecksum: batch.Checksum, wantPuts: 1},
		{name: "visible after prior failure", state: "VISIBLE", auditStatus: "Success", auditChecksum: batch.Checksum, priorFailure: true},
		{name: "verified receipt", state: "UNKNOWN", verified: true},
		{name: "contradictory success", state: "ABORTED", auditStatus: "Success", auditChecksum: batch.Checksum, wantError: "conflicts with its success audit"},
		{name: "mismatched failure", state: "ABORTED", auditStatus: "Fail", auditChecksum: "another batch", wantError: "Stream Load audit did not match"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &dorisTestDB{
				batch: batch, intent: true, verified: test.verified,
				auditChecksum: test.auditChecksum, auditStatus: test.auditStatus, priorFailure: test.priorFailure,
			}
			db := sql.OpenDB(fixture)
			defer db.Close()
			endpoint, err := url.Parse("http://doris.example.invalid")
			require.NoError(t, err)
			puts := 0
			target := &DorisTarget{
				db: db, frontend: endpoint, backend: endpoint,
				client: &http.Client{Transport: dorisTestTransport(func(req *http.Request) (*http.Response, error) {
					var response string
					switch req.Method {
					case http.MethodGet:
						require.Equal(t, batch.Label, req.URL.Query().Get("label"))
						response = fmt.Sprintf(`{"code":0,"data":%q}`, test.state)
					case http.MethodPut:
						puts++
						require.Equal(t, "/api/warehouse/events/_stream_load", req.URL.Path)
						require.Equal(t, batch.Label, req.Header.Get("label"))
						require.Equal(t, batch.Checksum, req.Header.Get("comment"))
						body, err := io.ReadAll(req.Body)
						require.NoError(t, err)
						require.Equal(t, "NDI=,0\n", string(body))
						response = fmt.Sprintf(`{"TxnId":42,"Status":"Success","Label":%q,"Comment":%q,"NumberTotalRows":1,"NumberLoadedRows":1,"NumberFilteredRows":0,"NumberUnselectedRows":0}`,
							batch.Label, batch.Checksum)
					default:
						t.Fatalf("unexpected method %s", req.Method)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response))}, nil
				})},
			}
			err = deliverBatch(t.Context(), recoverableSnapshotTarget{target}, batch)
			if test.wantError == "" {
				require.NoError(t, err)
				require.True(t, fixture.verified)
			} else {
				require.ErrorContains(t, err, test.wantError)
				require.False(t, fixture.verified)
			}
			require.Equal(t, test.wantPuts, puts)
			require.Zero(t, fixture.intentWrites)
		})
	}
}

// TestDorisLoadRejectsSuccessWithFilteredRows 验证 Doris 报告成功但过滤了行时拒绝确认批次。
func TestDorisLoadRejectsSuccessWithFilteredRows(t *testing.T) {
	id := "10"
	payload, err := EncodeDorisLoad([]string{"id"}, []DorisRow{{Values: []*string{&id}}})
	require.NoError(t, err)
	batch := LoadBatch{TargetTable: "warehouse.events", RowCount: 1, Label: "filtered_load", Payload: payload, Checksum: digest(payload)}
	fixture := &dorisTestDB{batch: batch}
	db := sql.OpenDB(fixture)
	defer db.Close()
	endpoint, err := url.Parse("http://doris.example.invalid")
	require.NoError(t, err)
	target := &DorisTarget{
		db: db, backend: endpoint,
		client: &http.Client{Transport: dorisTestTransport(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodPut, request.Method)
			body := fmt.Sprintf(`{"Status":"Success","Label":%q,"Comment":%q,"NumberTotalRows":1,"NumberLoadedRows":0,"NumberFilteredRows":1,"NumberUnselectedRows":0}`,
				batch.Label, batch.Checksum)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		})},
	}
	_, err = target.SubmitLoad(t.Context(), batch)
	require.ErrorContains(t, err, "load_label=filtered_load")
	require.ErrorContains(t, err, "filtered_rows=1")
	require.True(t, fixture.intent)
	require.False(t, fixture.verified)
}

type dorisTestTransport func(*http.Request) (*http.Response, error)

// RoundTrip 将测试 HTTP 请求转交给夹具提供的传输函数。
func (f dorisTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type dorisTestDB struct {
	batch          LoadBatch
	intent         bool
	intentWrites   int
	verified       bool
	auditQueries   int
	auditChecksum  string
	auditStatus    string
	priorFailure   bool
	deadlineMS     int64
	deadlineWrites int
}

// Connect 返回用于模拟 Doris SQL 操作的测试连接。
func (d *dorisTestDB) Connect(context.Context) (driver.Conn, error) { return d, nil }

// Driver 实现测试连接器的驱动接口，连接由测试夹具提供。
func (d *dorisTestDB) Driver() driver.Driver { return nil }

// Close 关闭测试替身，夹具无需额外释放资源。
func (d *dorisTestDB) Close() error { return nil }

// Prepare 指示 SQL 测试可以回退到直接执行语句。
func (d *dorisTestDB) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

// Begin 指示 SQL 测试不提供事务实现。
func (d *dorisTestDB) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }

// ExecContext 处理测试中的 Doris 写入请求。
func (d *dorisTestDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "INSERT INTO") && strings.Contains(query, "load_recheck_deadlines") && len(args) == 2 {
		d.deadlineMS = args[1].Value.(int64)
		d.deadlineWrites++
		return driver.RowsAffected(1), nil
	}
	if !strings.Contains(query, "INSERT INTO") || !strings.Contains(query, "load_receipts") || len(args) != 5 {
		return nil, fmt.Errorf("unexpected Doris exec: %s", query)
	}
	d.intent = true
	d.verified = args[4].Value.(bool)
	if !d.verified {
		d.intentWrites++
	}
	return driver.RowsAffected(1), nil
}

// QueryContext 构造供测试读取的 Doris 元数据查询结果。
func (d *dorisTestDB) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "SHOW STREAM LOAD") {
		d.auditQueries++
		rows := &dorisTestRows{
			fields: []string{"Label", "Table", "Status", "Comment", "TotalRows", "LoadedRows", "FilteredRows", "UnselectedRows"},
		}
		if d.auditChecksum != "" {
			_, table, _ := splitDorisTable(d.batch.TargetTable)
			status := d.auditStatus
			if status == "" {
				status = "Success"
			}
			count := int64(d.batch.RowCount)
			if status == "Fail" {
				count = 0
			}
			rows.values = []driver.Value{d.batch.Label, table, status, d.auditChecksum, count, count, int64(0), int64(0)}
			if d.priorFailure {
				rows.remaining = [][]driver.Value{rows.values}
				rows.values = []driver.Value{d.batch.Label, table, "Fail", d.batch.Checksum, int64(0), int64(0), int64(0), int64(0)}
			}
		}
		return rows, nil
	}
	if strings.HasPrefix(query, "SHOW TRANSACTION") {
		return &dorisTestRows{
			fields: []string{"TransactionId", "Label", "TransactionStatus"},
			values: []driver.Value{int64(42), d.batch.Label, "VISIBLE"},
		}, nil
	}
	if strings.Contains(query, "load_recheck_deadlines") {
		rows := &dorisTestRows{fields: []string{"deadline_ms"}}
		if d.deadlineWrites != 0 {
			rows.values = []driver.Value{d.deadlineMS}
		}
		return rows, nil
	}
	if strings.Contains(query, "load_receipts") {
		rows := &dorisTestRows{fields: []string{"target_table", "payload_checksum", "expected_rows", "verified"}}
		if d.intent {
			rows.values = []driver.Value{d.batch.TargetTable, d.batch.Checksum, int64(d.batch.RowCount), d.verified}
		}
		return rows, nil
	}
	return nil, fmt.Errorf("unexpected Doris query: %s", query)
}

type dorisTestRows struct {
	fields    []string
	values    []driver.Value
	remaining [][]driver.Value
}

// Columns 返回测试查询结果的列名。
func (r *dorisTestRows) Columns() []string { return r.fields }

// Close 关闭测试替身，夹具无需额外释放资源。
func (r *dorisTestRows) Close() error { return nil }

// Next 将下一行测试数据写入调用方缓冲，并报告读取结束。
func (r *dorisTestRows) Next(values []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(values, r.values)
	if len(r.remaining) == 0 {
		r.values = nil
	} else {
		r.values = r.remaining[0]
		r.remaining = r.remaining[1:]
	}
	return nil
}

// TestDorisApprovedColumnMutationsReconcileWithoutAnotherSend 验证已审批的列变更在重启后凭原回执确认，避免重发 DDL。
func TestDorisApprovedColumnMutationsReconcileWithoutAnotherSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		id := DorisColumn{Name: "id", Type: "BIGINT"}
		before := DorisTable{Database: "warehouse", Name: "events", PrimaryKey: []string{"id"}, Columns: []DorisColumn{
			id, {Name: "value", Type: "BIGINT", Nullable: true},
		}}
		after := before
		after.Columns = []DorisColumn{id, {Name: "renamed", Type: "BIGINT", Nullable: true}}
		plan := DDLPlan{ID: "mutation", Kind: DDLRenameColumn, Before: before, After: after, RequiresApproval: true}
		fixture := &dorisMutationDB{plan: plan}
		db := sql.OpenDB(fixture)
		defer db.Close()
		target := &DorisTarget{db: db, config: DorisConfig{ControlDatabase: "cdc_control"}}

		ctx, stop := context.WithTimeout(t.Context(), 500*time.Millisecond)
		err := target.ApplyDDL(ctx, plan)
		stop()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Equal(t, 1, fixture.executions)
		require.True(t, fixture.intent)
		require.False(t, fixture.verified)

		// 适配器重启后须等待原 DDL 的执行证据。
		reopened := sql.OpenDB(fixture)
		defer reopened.Close()
		target = &DorisTarget{db: reopened, config: target.config}
		ctx, stop = context.WithTimeout(t.Context(), 500*time.Millisecond)
		err = target.ApplyDDL(ctx, plan)
		stop()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, fixture.executions)
		require.False(t, fixture.verified)

		fixture.applied = true
		fixture.jobState = "RUNNING"
		err = target.ApplyDDL(t.Context(), plan)
		require.ErrorIs(t, err, ErrLoadPending)
		require.False(t, fixture.verified, "a visible schema is insufficient while its SQL job remains active")
		require.Contains(t, fixture.receipt[2], `"schema_job_id":42`)
		require.NoError(t, reopened.Close())
		time.Sleep(dorisDDLResolutionTimeout + time.Second)
		afterRestart := sql.OpenDB(fixture)
		defer afterRestart.Close()
		target = &DorisTarget{db: afterRestart, config: target.config}
		fixture.jobID = 43
		require.ErrorContains(t, target.ApplyDDL(t.Context(), plan), "conflict with the original DDL receipt")
		require.False(t, fixture.verified)
		fixture.jobID = 42
		require.ErrorIs(t, target.ApplyDDL(t.Context(), plan), ErrLoadPending)
		time.Sleep(defaultDorisDDLJobTimeout)
		require.ErrorContains(t, target.ApplyDDL(t.Context(), plan), "persisted monitoring deadline")
		require.False(t, fixture.verified)

		fixture.jobState = "FINISHED"
		require.NoError(t, target.ApplyDDL(t.Context(), plan))
		require.NoError(t, target.ApplyDDL(t.Context(), plan))
		require.Equal(t, 1, fixture.executions)
		require.True(t, fixture.verified)
	})
}

type dorisMutationDB struct {
	plan                      DDLPlan
	intent, verified, applied bool
	receipt                   []driver.Value
	executions                int
	jobState                  string
	jobID                     int64
}

// Connect 返回用于模拟 Doris SQL 操作的测试连接。
func (d *dorisMutationDB) Connect(context.Context) (driver.Conn, error) {
	return d, nil
}

// Driver 实现测试连接器的驱动接口，连接由测试夹具提供。
func (d *dorisMutationDB) Driver() driver.Driver { return nil }

// Close 关闭测试替身，夹具无需额外释放资源。
func (d *dorisMutationDB) Close() error { return nil }

// Prepare 指示 SQL 测试可以回退到直接执行语句。
func (d *dorisMutationDB) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

// Begin 指示 SQL 测试不提供事务实现。
func (d *dorisMutationDB) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }

// ExecContext 处理测试中的 Doris 写入请求。
func (d *dorisMutationDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(query, "CREATE TABLE IF NOT EXISTS"):
		return driver.RowsAffected(0), nil
	case strings.HasPrefix(query, "INSERT INTO") && strings.Contains(query, "ddl_receipts"):
		d.intent = true
		d.verified = args[4].Value.(bool)
		d.receipt = []driver.Value{args[1].Value, args[2].Value, args[3].Value, args[4].Value}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "ALTER TABLE"):
		d.executions++
		if query != "ALTER TABLE `warehouse`.`events` RENAME COLUMN `value` `renamed`" || !d.intent || d.verified {
			return nil, fmt.Errorf("unexpected mutation submission %q", query)
		}
		return nil, io.ErrUnexpectedEOF
	default:
		return nil, fmt.Errorf("unexpected Doris mutation exec %q", query)
	}
}

// QueryContext 构造供测试读取的 Doris 元数据查询结果。
func (d *dorisMutationDB) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(query, "SELECT") && strings.Contains(query, "ddl_receipts"):
		return &dorisTestRows{fields: []string{"plan_digest", "job_fence", "partition_ids", "verified"}, values: d.receipt}, nil
	case strings.HasPrefix(query, "SHOW CREATE TABLE"):
		return &dorisTestRows{fields: []string{"Table", "Create Table"}, values: []driver.Value{"events", "CREATE TABLE `events` (id BIGINT)\nUNIQUE KEY(`id`)"}}, nil
	case strings.HasPrefix(query, "DESC "):
		table := d.plan.Before
		if d.applied {
			table = d.plan.After
		}
		rows := &dorisMutationRows{columns: []string{"Field", "Type", "Null", "Key", "Default"}}
		for _, col := range table.Columns {
			nullable := "No"
			if col.Nullable {
				nullable = "Yes"
			}
			rows.rows = append(rows.rows, []driver.Value{col.Name, col.Type, nullable, col.Name == "id", nil})
		}
		return rows, nil
	case strings.HasPrefix(query, "SHOW ALTER TABLE COLUMN"):
		rows := &dorisMutationRows{columns: []string{"JobId", "State"}}
		if d.jobState != "" {
			jobID := d.jobID
			if jobID == 0 {
				jobID = 42
			}
			// 同一 JobId 可因涉及多个物化索引而返回多行。
			rows.rows = [][]driver.Value{{jobID, d.jobState}, {jobID, d.jobState}}
		}
		return rows, nil
	case strings.HasPrefix(query, "SHOW PARTITIONS"):
		return &dorisTestRows{fields: []string{"PartitionId"}, values: []driver.Value{int64(11)}}, nil
	default:
		return nil, fmt.Errorf("unexpected Doris mutation query %q", query)
	}
}

type dorisMutationRows struct {
	columns []string
	rows    [][]driver.Value
}

// Columns 返回测试查询结果的列名。
func (r *dorisMutationRows) Columns() []string { return r.columns }

// Close 关闭测试替身，夹具无需额外释放资源。
func (r *dorisMutationRows) Close() error { return nil }

// Next 将下一行测试数据写入调用方缓冲，并报告读取结束。
func (r *dorisMutationRows) Next(values []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(values, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}
