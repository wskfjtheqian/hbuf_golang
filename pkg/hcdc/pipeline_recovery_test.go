package hcdc

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
)

type bootstrapTestTarget struct {
	*deliveryTestTarget
	proof Checkpoint
}

// verifySchemaRevision 核对快照在测试目标中留下的基线结构证据。
func (target *bootstrapTestTarget) verifySchemaRevision(_ context.Context, proof Checkpoint) error {
	if target.proof != proof {
		return errors.New("snapshot proof does not exist in this target")
	}
	return nil
}

// TestBootstrapRecoversPreparedSnapshot 验证基线发布前后保留快照证据，重启只补齐基线与本地完成标记。
func TestBootstrapRecoversPreparedSnapshot(t *testing.T) {
	for _, published := range []bool{false, true} {
		name := "before_baseline"
		if published {
			name = "after_baseline"
		}
		t.Run(name, func(t *testing.T) {
			fixture := deliveryTestConfig(t)
			config := PipelineConfig{Directory: fixture.Directory, Identity: fixture.Identity}
			state := bootstrapState{Cursor: fixture.Baseline, ZeroDateRows: 5}
			path := filepath.Join(config.Directory, "bootstrap")
			store, err := spool.Open(path)
			require.NoError(t, err)
			payload, err := json.Marshal(bootstrapRecord{bootstrapState: state, Prepared: true})
			require.NoError(t, err)
			require.NoError(t, store.Append(bootstrapPreparedSequence, payload))

			catalog, err := snapshotCatalog(state)
			require.NoError(t, err)
			target := &bootstrapTestTarget{
				deliveryTestTarget: &deliveryTestTarget{},
				proof:              Checkpoint{Identity: config.Identity, Cursor: state.Cursor, StructureRevision: digest(catalog)},
			}
			if published {
				delivery, err := openPipelineDelivery(t.Context(), config, target, state.Cursor)
				require.NoError(t, err)
				require.NoError(t, delivery.Close())
				target.persistCheckpoint = func(Checkpoint) error {
					t.Fatal("recovery rewrote an already confirmed baseline")
					return nil
				}
			}
			require.NoError(t, store.Close())

			store, err = spool.Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			recovered, err := restoreBootstrap(t.Context(), store, target, config)
			require.NoError(t, err)
			require.Equal(t, state, recovered)
			require.Equal(t, Checkpoint{Identity: config.Identity, Cursor: state.Cursor}, *target.checkpoint)
			completed, err := store.Read(bootstrapCompletedSequence)
			require.NoError(t, err)
			require.Equal(t, payload, completed)
			require.Empty(t, target.submitted)

			// 完成后已存在增量进度时，仍恢复原始快照基线供交付模块核对。
			target.checkpoint.Sequence = 1
			recovered, err = restoreBootstrap(t.Context(), store, target, config)
			require.NoError(t, err)
			require.Equal(t, state, recovered)
		})
	}
}

// TestBootstrapRequiresSnapshotEvidence 验证旧完成记录兼容且中断的快照无法凭远端位点补写完成标记。
func TestBootstrapRequiresSnapshotEvidence(t *testing.T) {
	fixture := deliveryTestConfig(t)
	config := PipelineConfig{Directory: fixture.Directory, Identity: fixture.Identity}
	state := bootstrapState{Cursor: fixture.Baseline, ZeroDateRows: 5}
	target := &bootstrapTestTarget{deliveryTestTarget: &deliveryTestTarget{checkpoint: &Checkpoint{Identity: config.Identity, Cursor: state.Cursor}}}
	store, err := spool.Open(filepath.Join(config.Directory, "bootstrap"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = restoreBootstrap(t.Context(), store, target, config)
	require.ErrorContains(t, err, "snapshot was interrupted")

	payload, err := json.Marshal(bootstrapRecord{bootstrapState: state, Prepared: true})
	require.NoError(t, err)
	require.NoError(t, store.Append(bootstrapPreparedSequence, payload))
	_, err = prepareBootstrap(t.Context(), store, nil, nil, config, bootstrapManifest{}, false)
	require.ErrorContains(t, err, "bootstrap manifest is missing")
	target.checkpoint.Cursor.Position++
	_, err = restoreBootstrap(t.Context(), store, target, config)
	require.ErrorContains(t, err, "conflicts with the prepared CDC snapshot baseline")
	_, err = restoreBootstrap(t.Context(), store, &bootstrapTestTarget{deliveryTestTarget: &deliveryTestTarget{}}, config)
	require.ErrorContains(t, err, "verify prepared CDC snapshot in its original Doris target")
	sequences, err := store.Sequences()
	require.NoError(t, err)
	require.Equal(t, []uint64{bootstrapPreparedSequence}, sequences)

	legacy, err := spool.Open(filepath.Join(config.Directory, "old-bootstrap"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = legacy.Close() })
	oldPayload, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, legacy.Append(bootstrapPreparedSequence, oldPayload))
	target.checkpoint.Sequence = 1
	recovered, err := restoreBootstrap(t.Context(), legacy, target, config)
	require.NoError(t, err)
	require.Equal(t, state, recovered)
}

type controlTestStore func(uint64, []byte) error

// Append 调用测试指定的落盘行为。
func (store controlTestStore) Append(sequence uint64, data []byte) error {
	return store(sequence, data)
}

// TestPipelineCloseWhileControlWriteWaits 验证阻塞的控制落盘不会占住状态锁或延迟带期限的关闭返回。
func TestPipelineCloseWhileControlWriteWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		_, stopCapture := context.WithCancel(runCtx)
		defer stopCapture()
		writing, allowWrite := make(chan struct{}), make(chan struct{})
		pipeline := &Pipeline{
			changed: make(chan struct{}), done: make(chan struct{}),
			cancel: cancel, stopCapture: stopCapture, ready: true,
			status: PipelineStatus{State: PipelineRunning},
			control: controlTestStore(func(uint64, []byte) error {
				close(writing)
				<-allowWrite
				return nil
			}),
		}
		go func() {
			<-runCtx.Done()
			pipeline.waitForControlWrite()
			close(pipeline.done)
		}()
		pauseCtx, cancelPause := context.WithCancel(t.Context())
		defer cancelPause()
		paused := make(chan error, 1)
		go func() { paused <- pipeline.Pause(pauseCtx, "test") }()
		<-writing
		require.Equal(t, PipelinePaused, pipeline.Status().State)
		pipeline.mu.Lock()
		blocked := pipeline.deliveryBlockedLocked()
		pipeline.mu.Unlock()
		require.True(t, blocked)

		closeCtx, stop := context.WithTimeout(t.Context(), time.Second)
		defer stop()
		require.ErrorIs(t, pipeline.Close(closeCtx), context.DeadlineExceeded)
		require.Equal(t, PipelineStopping, pipeline.Status().State)
		require.False(t, channelClosed(pipeline.Done()))
		cancelPause()
		require.ErrorIs(t, <-paused, ErrControlOutcomeUnknown)

		close(allowWrite)
		<-pipeline.Done()
		require.Equal(t, uint64(1), pipeline.controlSequence)
	})
}

// TestPipelineControlRetriesIdenticalUncertainRecord 验证控制记录结果未知时阻止派发，并核对原记录后解除阻塞。
func TestPipelineControlRetriesIdenticalUncertainRecord(t *testing.T) {
	var sequences []uint64
	var payloads []string
	pipeline := &Pipeline{
		changed: make(chan struct{}), ready: true,
		status: PipelineStatus{State: PipelineRunning},
		control: controlTestStore(func(sequence uint64, payload []byte) error {
			sequences = append(sequences, sequence)
			payloads = append(payloads, string(payload))
			if len(sequences) == 1 {
				return spool.ErrPublicationUncertain
			}
			return nil
		}),
	}
	require.ErrorIs(t, pipeline.Pause(t.Context(), "test"), ErrControlOutcomeUnknown)
	require.Equal(t, ErrControlOutcomeUnknown.Error(), pipeline.Status().Reason)
	require.NoError(t, pipeline.Resume(t.Context()))
	require.Equal(t, []uint64{1, 1, 2}, sequences)
	require.Equal(t, payloads[0], payloads[1])
	require.Equal(t, PipelineRunning, pipeline.Status().State)
}

// TestDeliveryCloseContextStopsNewWork 验证带期限关闭可返回，已有请求结束后同一次关闭释放资源。
func TestDeliveryCloseContextStopsNewWork(t *testing.T) {
	delivery, config, target := openDeliveryTest(t)
	started, blocked := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(blocked) })
	defer unblock()
	target.submitLoad = func(batch LoadBatch) (LoadConfirmation, error) {
		close(started)
		<-blocked
		return successfulDeliveryTestLoad(batch), nil
	}
	record := OrderedRecord{Sequence: 1, TransactionID: "in-flight", Cursor: config.Baseline,
		Batches: []LoadBatch{{TargetTable: "report.events", RowCount: 1, Payload: []byte("1\n")}}}
	record.Cursor.Position++
	require.NoError(t, delivery.AppendRecord(record))
	result := make(chan error, 1)
	go func() {
		_, err := delivery.DeliverNext(t.Context())
		result <- err
	}()
	<-started
	closeCtx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, delivery.CloseContext(closeCtx), context.Canceled)
	require.ErrorIs(t, delivery.AppendRecord(record), ErrDeliveryClosed)
	_, err := delivery.DeliverNext(t.Context())
	require.ErrorIs(t, err, ErrDeliveryClosed)
	unblock()
	require.NoError(t, <-result)
	require.NoError(t, delivery.Close())
}

// channelClosed 报告完成通道是否已经关闭。
func channelClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// TestSchemaHistoryReclaimsBeforeDeliveredAnchor 验证结构历史压缩保留检查点结构和未交付版本。
func TestSchemaHistoryReclaimsBeforeDeliveredAnchor(t *testing.T) {
	fixture := deliveryTestConfig(t)
	history, err := spool.Open(fixture.Directory)
	require.NoError(t, err)
	t.Cleanup(func() { _ = history.Close() })

	var delivered, durable Checkpoint
	for _, sequence := range []uint64{1, 2, 4} {
		cursor := fixture.Baseline
		cursor.Position += uint32(sequence)
		version := schemaVersion{Sequence: sequence, Cursor: cursor}
		catalog, err := json.Marshal(version)
		require.NoError(t, err)
		require.NoError(t, history.Append(sequence, catalog))
		switch sequence {
		case 2:
			delivered = Checkpoint{Sequence: 3, StructureRevision: digest(catalog)}
		case 4:
			durable = Checkpoint{Sequence: sequence, Cursor: cursor, StructureRevision: digest(catalog)}
		}
	}
	conflicting := delivered
	conflicting.StructureRevision = "other"
	require.ErrorContains(t, reclaimSchemaHistory(history, conflicting), "anchor does not match")
	require.NoError(t, reclaimSchemaHistory(history, delivered))
	sequences, err := history.Sequences()
	require.NoError(t, err)
	require.Equal(t, []uint64{2, 4}, sequences)
	version, revision, err := readSchemaVersion(history, bootstrapState{Cursor: fixture.Baseline}, durable, false)
	require.NoError(t, err)
	require.Equal(t, uint64(4), version.Sequence)
	require.Equal(t, durable.StructureRevision, revision)
}

// TestLegacyDeliveryRequiresConfirmedRecord 验证缺失摘要的已确认物理记录仍可恢复，未确认记录不能派发。
func TestLegacyDeliveryRequiresConfirmedRecord(t *testing.T) {
	fixture := deliveryTestConfig(t)
	target := &deliveryTestTarget{}
	delivery, err := OpenDelivery(t.Context(), fixture, target)
	require.NoError(t, err)
	legacy := OrderedRecord{Sequence: 1, TransactionID: "legacy", Cursor: fixture.Baseline}
	legacy.Cursor.Position++
	payload, err := json.Marshal(bufferedOrderedRecord{OrderedRecord: legacy})
	require.NoError(t, err)
	require.NoError(t, delivery.store.Append(legacy.Sequence, payload))
	require.NoError(t, delivery.Close())
	_, err = OpenDelivery(t.Context(), fixture, target)
	require.ErrorContains(t, err, "unconfirmed legacy CDC record has no manifest digest")
	conflicting := Checkpoint{Identity: fixture.Identity, Sequence: legacy.Sequence, Cursor: legacy.Cursor}
	conflicting.Cursor.Position++
	target.checkpoint = &conflicting
	_, err = OpenDelivery(t.Context(), fixture, target)
	require.ErrorContains(t, err, "legacy CDC record does not match the verified checkpoint")
	target.checkpoint.Cursor = legacy.Cursor
	delivery, err = OpenDelivery(t.Context(), fixture, target)
	require.NoError(t, err)
	require.Equal(t, *target.checkpoint, delivery.Status().Delivered)
	require.NoError(t, delivery.Close())
}

// TestMariaDBGTIDRejectsRepeatedDomain 验证合法多域位置通过，原始位置中重复域被显式拒绝。
func TestMariaDBGTIDRejectsRepeatedDomain(t *testing.T) {
	cursor := Cursor{SourceIdentity: "maria", File: "maria-bin.000001", Position: 99, Flavor: "mariadb", GTIDSet: "0-4-31,8-3-42"}
	_, _, set, err := parseCursor(cursor)
	require.NoError(t, err)
	require.NoError(t, validateGTIDCursor(cursor, set))
	cursor.GTIDSet += ",0-5-32"
	_, _, set, err = parseCursor(cursor)
	require.NoError(t, err)
	require.ErrorContains(t, validateGTIDCursor(cursor, set), "one MariaDB GTID per domain")
}
