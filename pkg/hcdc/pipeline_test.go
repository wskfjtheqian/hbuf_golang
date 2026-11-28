package cdc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
)

// TestPipelineWatchStatus 验证首次快照、慢回调合并、状态隔离与终态通知。
func TestPipelineWatchStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &Pipeline{changed: make(chan struct{}), status: PipelineStatus{State: PipelineStarting}}
		observed := make(chan PipelineStatus)
		unblock := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			finished <- p.WatchStatus(t.Context(), func(_ context.Context, status PipelineStatus) {
				p.Status() // 回调可以在同一 goroutine 中读取状态。
				observed <- status
				if status.State == PipelineStarting {
					<-unblock
				}
			})
		}()
		require.Equal(t, PipelineStarting, (<-observed).State)
		p.mu.Lock()
		p.status.Progress.Durable.Sequence = 1
		p.signalStatusLocked()
		p.status.Progress.Durable.Sequence = 2
		p.status.PendingDDL = &DDLPlan{Before: DorisTable{Columns: []DorisColumn{{Name: "amount"}}}}
		p.transitionLocked(PipelineAwaitingApproval, "DDL approval required")
		p.signalLocked()
		p.mu.Unlock()
		close(unblock)

		status := <-observed
		require.Equal(t, PipelineAwaitingApproval, status.State)
		require.EqualValues(t, 2, status.Progress.Durable.Sequence)
		status.PendingDDL.Before.Columns[0].Name = "modified"
		require.Equal(t, "amount", p.Status().PendingDDL.Before.Columns[0].Name)

		failure := errors.New("test pipeline error")
		p.mu.Lock()
		p.status.Err = failure
		p.transitionLocked(PipelineFailed, failure.Error())
		p.signalLocked()
		p.mu.Unlock()
		status = <-observed
		require.Equal(t, PipelineFailed, status.State)
		require.ErrorIs(t, status.Err, failure)
		require.NoError(t, <-finished)
	})
}

// TestPipelineWatchStatusCancellation 验证监听取消后释放等待，且流水线继续运行。
func TestPipelineWatchStatusCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &Pipeline{status: PipelineStatus{State: PipelineRunning}}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		initial := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			finished <- p.WatchStatus(ctx, func(context.Context, PipelineStatus) { close(initial) })
		}()
		<-initial
		cancel()
		require.ErrorIs(t, <-finished, context.Canceled)
		require.Equal(t, PipelineRunning, p.Status().State)
		require.Error(t, p.WatchStatus(t.Context(), nil))
	})
}

// managerTestPipelines 创建两个端点相同但缓冲目录、复制 ID 和目标库各自独立的源配置。
func managerTestPipelines(t *testing.T) (PipelineConfig, PipelineConfig) {
	t.Helper()
	directory := t.TempDir()
	first := PipelineConfig{
		Identity:  StreamIdentity{PipelineID: "test", SourceID: "first", Generation: "1"},
		Directory: filepath.Join(directory, "first"),
		Source: MySQLConfig{
			Address: "127.0.0.1:3306", User: "cdc", SourceIdentity: "source", ServerID: 10,
			Database: "orders", Tables: []string{"entries", "items"},
		},
		Doris: DorisConfig{
			SQLDSN: "cdc@tcp(127.0.0.1:9030)/", FrontendURL: "http://127.0.0.1:8030", StreamLoadURL: "http://127.0.0.1:8040",
		},
		TargetDatabase: "first_shadow",
	}
	second := first
	second.Identity.SourceID = "second"
	second.Directory = filepath.Join(directory, "second")
	second.Source.ServerID++
	second.TargetDatabase = "second_shadow"
	return first, second
}

// TestManagerValidatesCompleteConfigBeforeStarting 验证批量配置在启动任何源之前拒绝无效端点和重复资源。
func TestManagerValidatesCompleteConfigBeforeStarting(t *testing.T) {
	first, second := managerTestPipelines(t)
	var absent *Config
	require.False(t, absent.Validate(t.Context()))
	require.True(t, (&Config{Pipelines: []PipelineConfig{first, second}}).Validate(t.Context()))

	for _, scenario := range []struct {
		name  string
		edit  func(*PipelineConfig)
		error string
	}{
		{"invalid endpoint", func(p *PipelineConfig) { p.Doris.StreamLoadURL = "/invalid" }, "HTTP(S) URL"},
		{"reused source ID", func(p *PipelineConfig) { p.Identity.SourceID = first.Identity.SourceID }, "duplicate CDC source ID"},
		{"reused target", func(p *PipelineConfig) { p.TargetDatabase = first.TargetDatabase }, "target database or checkpoint identity"},
		{"reused directory", func(p *PipelineConfig) { p.Directory = first.Directory + string(filepath.Separator) + "." }, "share a buffer directory"},
		{"invalid zero date policy", func(p *PipelineConfig) { p.ZeroDatePolicy = "invalid" }, "zero date policy"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			manager := NewManager(t.Context())
			candidate := second
			scenario.edit(&candidate)
			config := &Config{Pipelines: []PipelineConfig{first, candidate}}
			require.False(t, config.Validate(t.Context()))
			err := manager.SetConfig(t.Context(), config)
			require.ErrorContains(t, err, scenario.error)
			_, installed := manager.Pipeline(first.Identity.SourceID)
			require.False(t, installed)
		})
	}
}

// TestConfigEqual 验证等价比较、实际参数变更和重复提交时的快速返回。
func TestConfigEqual(t *testing.T) {
	first, second := managerTestPipelines(t)
	current := &Config{Pipelines: []PipelineConfig{first, second}}
	var absent *Config
	require.True(t, absent.Equal(nil))
	require.False(t, absent.Equal(current))
	require.False(t, current.Equal(nil))

	updatedFirst := first
	updatedFirst.Source.Tables = []string{"items", "entries"}
	updatedFirst.Source.Flavor = "mysql"
	updatedFirst.Doris.ControlDatabase = "cdc_control"
	updatedFirst.ZeroDatePolicy = ZeroDateReject
	updated := &Config{Pipelines: []PipelineConfig{second, updatedFirst}}
	require.True(t, current.Equal(updated))
	require.True(t, updated.Equal(current))
	require.Equal(t, []string{"items", "entries"}, updated.Pipelines[1].Source.Tables)

	updated.Pipelines[1].Source.Password = "new-password"
	require.False(t, current.Equal(updated))
	updated.Pipelines[1] = updatedFirst
	updated.Pipelines[1].Doris.SQLDSN = "new-user@tcp(127.0.0.1:9030)/"
	require.False(t, current.Equal(updated))
	updated.Pipelines[1] = updatedFirst
	updated.Pipelines[1].MaxPendingBytes = 1024
	require.False(t, current.Equal(updated))
	updated.Pipelines[1] = updatedFirst
	updated.Pipelines[1].ZeroDatePolicy = ZeroDateEpoch
	require.True(t, updated.Validate(t.Context()))
	require.False(t, current.Equal(updated))
	require.False(t, updated.Equal(current))

	manager := NewManager(t.Context())
	prepared, err := prepareManagerConfig(current, nil)
	require.NoError(t, err)
	for id, config := range prepared {
		pipeline := &Pipeline{done: make(chan struct{}), status: PipelineStatus{State: PipelineRunning}}
		manager.sources[id] = managedSource{config: config, pipeline: pipeline}
	}
	_, err = prepareManagerConfig(updated, manager.sources)
	require.ErrorContains(t, err, "requires a new generation")
	updated.Pipelines[1] = updatedFirst

	before := manager.sources[first.Identity.SourceID].pipeline
	// 将原来的待建目录变为普通文件，使重新预检必定失败。
	require.NoError(t, os.WriteFile(first.Directory, nil, 0o600))
	_, err = prepareManagedConfig(first)
	require.Error(t, err)
	require.NoError(t, manager.SetConfig(t.Context(), updated))
	after, installed := manager.Pipeline(first.Identity.SourceID)
	require.True(t, installed)
	require.Same(t, before, after)
}

// TestManagerKeepsTimedOutStopUntilResourcesAreReleased 验证停止超时后保留原句柄，直到流水线释放资源。
func TestManagerKeepsTimedOutStopUntilResourcesAreReleased(t *testing.T) {
	manager := NewManager(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pipeline := &Pipeline{
		changed: make(chan struct{}), done: make(chan struct{}),
		stopCapture: cancel, cancel: func() {}, ready: true,
		status: PipelineStatus{State: PipelineRunning},
	}
	manager.sources["source"] = managedSource{pipeline: pipeline}

	require.ErrorIs(t, manager.SetConfig(ctx, nil), context.Canceled)
	stopping, installed := manager.Pipeline("source")
	require.True(t, installed)
	require.Same(t, pipeline, stopping)
	require.Equal(t, PipelineStopping, stopping.Status().State)

	close(pipeline.done)
	require.NoError(t, manager.SetConfig(t.Context(), nil))
	_, installed = manager.Pipeline("source")
	require.False(t, installed)
}

// planTransaction 使用生产规划器执行内存内测试事务。盘上路径由实库专项验证。
func planTransaction(layouts []tableLayout, transaction mysqlsource.Transaction) ([]LoadBatch, uint64, error) {
	planner := newTransactionPlan(context.Background(), layouts, 0, func() (*os.File, error) {
		return os.CreateTemp("", "cdc-test-transaction-")
	})
	defer planner.reset()
	for _, change := range transaction.Changes {
		if err := planner.stage(change); err != nil {
			return nil, 0, err
		}
	}
	var batches []LoadBatch
	affected, err := planner.batches(func(batch LoadBatch) error {
		batches = append(batches, batch)
		return nil
	})
	return batches, affected, err
}

// TestPlanCoalescesPrimaryKeyChangesAndKeepsPhysicalColumns 验证主键修改按事务归并，并保留源物理列序。
func TestPlanCoalescesPrimaryKeyChangesAndKeepsPhysicalColumns(t *testing.T) {
	table := mysqlsource.Table{
		Ref: mysqlsource.TableRef{Schema: "source", Name: "users"},
		Columns: []mysqlsource.Column{
			{Name: "note", DataType: "varchar", ColumnType: "varchar(50)", Nullable: true},
			{Name: "id", DataType: "bigint", ColumnType: "bigint"},
		},
		PrimaryKey: []int{1},
	}
	layouts, err := buildTableLayouts(PipelineConfig{TargetDatabase: "report"}, []mysqlsource.Table{table})
	require.NoError(t, err)
	require.Equal(t, "id", layouts[0].target.Columns[0].Name)
	directory := t.TempDir()
	planner := newTransactionPlan(t.Context(), layouts, 0, func() (*os.File, error) {
		return os.CreateTemp(directory, "transaction-")
	})
	defer planner.reset()
	// 每行单独落段，确保同主键的新值覆盖较早磁盘段。
	planner.runLimit = 1
	for _, change := range []mysqlsource.Change{
		{Table: table.Ref, Before: []any{"old", int64(1)}, After: []any{"middle", int64(2)}},
		{Table: table.Ref, Before: []any{"middle", int64(2)}, After: []any{"", int64(2)}},
	} {
		require.NoError(t, planner.stage(change))
	}
	var batches []LoadBatch
	affected, err := planner.batches(func(batch LoadBatch) error {
		batches = append(batches, batch)
		return nil
	})
	require.NoError(t, err)
	require.Zero(t, affected)
	require.Len(t, batches, 1)
	require.EqualValues(t, 2, batches[0].RowCount)
	var payload dorisLoadPayload
	require.NoError(t, json.Unmarshal(batches[0].Payload, &payload))
	require.Equal(t, "b2xk,MQ==,1\n-,Mg==,0\n", payload.CSV)
}

// TestNullableBitmapRejectsNull 验证可空源 Bitmap 映射至 Doris 非空列，实际 NULL 在交付前阻塞。
func TestNullableBitmapRejectsNull(t *testing.T) {
	table := mysqlsource.Table{
		Ref: mysqlsource.TableRef{Schema: "source", Name: "events"},
		Columns: []mysqlsource.Column{
			{Name: "id", DataType: "bigint", ColumnType: "bigint"},
			{Name: "members", DataType: "mediumblob", Nullable: true, Comment: "CustomType=Bitmap64"},
		},
		PrimaryKey: []int{0},
	}
	layouts, err := buildTableLayouts(PipelineConfig{TargetDatabase: "report"}, []mysqlsource.Table{table})
	require.NoError(t, err)
	require.Equal(t, DorisColumn{Name: "members", Type: "BITMAP"}, layouts[0].target.Columns[1])
	_, _, err = planTransaction(layouts, mysqlsource.Transaction{Changes: []mysqlsource.Change{
		{Table: table.Ref, After: []any{int64(1), nil}},
	}})
	require.ErrorContains(t, err, "source.events.members contains SQL NULL")
}

// TestZeroDatePolicy 验证零日期映射、主键保护和归并后导入行数。
func TestZeroDatePolicy(t *testing.T) {
	table := mysqlsource.Table{
		Ref: mysqlsource.TableRef{Schema: "source", Name: "events"},
		Columns: []mysqlsource.Column{
			{Name: "id", DataType: "bigint", ColumnType: "bigint"},
			{Name: "day", DataType: "date", ColumnType: "date"},
			{Name: "created_at", DataType: "datetime", ColumnType: "datetime(6)"},
			{Name: "seen_at", DataType: "timestamp", ColumnType: "timestamp(3)"},
			{Name: "duration", DataType: "time", ColumnType: "time(3)"},
			{Name: "year", DataType: "year", ColumnType: "year"},
			{Name: "note", DataType: "varchar", ColumnType: "varchar(50)"},
			{Name: "optional_day", DataType: "date", ColumnType: "date", Nullable: true},
		},
		PrimaryKey: []int{0},
	}
	config := PipelineConfig{TargetDatabase: "report"}
	layouts, err := buildTableLayouts(config, []mysqlsource.Table{table})
	require.NoError(t, err)
	regular := []any{int64(1), "2025-01-31", "2025-01-31 12:34:56.120000", "2025-01-31 12:34:56.000", "00:00:00.000", 0, "0000-00-00", nil}
	zeros := slices.Clone(regular)
	zeros[1], zeros[2], zeros[3] = []byte("0000-00-00"), "0000-00-00 12:34:56.120000", "0000-00-00 00:00:00.000"

	// 分别检查快照和增量中遇到的每种日期类型。
	for _, column := range []int{1, 2, 3} {
		values := slices.Clone(regular)
		values[column] = zeros[column]
		_, _, err := layouts[0].encodeRow(values, false)
		require.ErrorContains(t, err, "zero date")
		_, _, err = planTransaction(layouts, mysqlsource.Transaction{Changes: []mysqlsource.Change{
			{Table: table.Ref, After: values},
		}})
		require.ErrorContains(t, err, "zero date")
	}
	_, mapped, err := layouts[0].encodeRow(regular, false)
	require.NoError(t, err)
	require.False(t, mapped)

	config.ZeroDatePolicy = ZeroDateEpoch
	layouts, err = buildTableLayouts(config, []mysqlsource.Table{table})
	require.NoError(t, err)
	row, mapped, err := layouts[0].encodeRow(zeros, false)
	require.NoError(t, err)
	require.True(t, mapped)
	for index, want := range []string{"1", "1970-01-01", "1970-01-01 12:34:56.12", "1970-01-01 00:00:00", "00:00:00", "0", "0000-00-00"} {
		require.NotNil(t, row.Values[index])
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte(want)), *row.Values[index])
	}
	require.Nil(t, row.Values[7])

	oldSecond, newSecond := slices.Clone(zeros), slices.Clone(regular)
	oldSecond[0], newSecond[0] = int64(2), int64(2)
	tombstone := slices.Clone(zeros)
	tombstone[0] = int64(3)
	batches, affected, err := planTransaction(layouts, mysqlsource.Transaction{Changes: []mysqlsource.Change{
		{Table: table.Ref, After: zeros},
		{Table: table.Ref, Before: zeros, After: zeros},
		{Table: table.Ref, After: oldSecond},
		{Table: table.Ref, Before: oldSecond, After: newSecond},
		{Table: table.Ref, Before: tombstone},
	}})
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.EqualValues(t, 3, batches[0].RowCount)
	require.EqualValues(t, 2, affected)

	table.PrimaryKey = []int{0, 1}
	layouts, err = buildTableLayouts(config, []mysqlsource.Table{table})
	require.NoError(t, err)
	_, mapped, err = layouts[0].encodeRow(zeros, false)
	require.NoError(t, err)
	require.True(t, mapped)
	epoch := slices.Clone(regular)
	epoch[1] = "1970-01-01"
	_, _, err = layouts[0].encodeRow(epoch, false)
	require.ErrorContains(t, err, "reserved")
	config.ZeroDatePolicy = ZeroDateReject
	layouts, err = buildTableLayouts(config, []mysqlsource.Table{table})
	require.NoError(t, err)
	_, mapped, err = layouts[0].encodeRow(epoch, false)
	require.NoError(t, err)
	require.False(t, mapped)
}

// TestPlanTransactionBatchesByEncodedBytes 验证窄行事务合并和大于批次目标的事务按编码字节数拆分。
func TestPlanTransactionBatchesByEncodedBytes(t *testing.T) {
	table := mysqlsource.Table{
		Ref: mysqlsource.TableRef{Schema: "source", Name: "events"},
		Columns: []mysqlsource.Column{
			{Name: "id", DataType: "bigint", ColumnType: "bigint"},
			{Name: "body", DataType: "longtext", ColumnType: "longtext"},
		},
		PrimaryKey: []int{0},
	}
	layouts, err := buildTableLayouts(PipelineConfig{TargetDatabase: "report"}, []mysqlsource.Table{table})
	require.NoError(t, err)
	for _, test := range []struct {
		name      string
		rows      int
		bodyBytes int
		batchRows []uint64
	}{
		{name: "narrow transaction", rows: 5000, bodyBytes: 118, batchRows: []uint64{5000}},
		{name: "CSV exceeds 10 MiB", rows: 3, bodyBytes: 3 << 20, batchRows: []uint64{2, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Repeat("x", test.bodyBytes)
			transaction := mysqlsource.Transaction{Changes: make([]mysqlsource.Change, test.rows)}
			for index := range transaction.Changes {
				transaction.Changes[index] = mysqlsource.Change{Table: table.Ref, After: []any{int64(index), body}}
			}
			batches, affected, err := planTransaction(layouts, transaction)
			require.NoError(t, err)
			require.Zero(t, affected)
			require.Len(t, batches, len(test.batchRows))
			for index, batch := range batches {
				require.Equal(t, test.batchRows[index], batch.RowCount)
				var payload dorisLoadPayload
				require.NoError(t, json.Unmarshal(batch.Payload, &payload))
				require.EqualValues(t, batch.RowCount, strings.Count(payload.CSV, "\n"))
				require.LessOrEqual(t, len(payload.CSV), defaultDorisLoadBatchBytes)
			}
		})
	}
}

// TestSnapshotBatchFlushesWhileSourceIsIdle 验证源扫描空闲时，快照缓冲仍按期限提交。
func TestSnapshotBatchFlushesWhileSourceIsIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		input := make(chan DorisRow)
		submitted := make(chan []DorisRow, 3)
		finished := make(chan error, 1)
		scan := func(ctx context.Context, consume func(DorisRow) error) error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case row, ok := <-input:
					if !ok {
						return nil
					}
					if err := consume(row); err != nil {
						return err
					}
				}
			}
		}
		go func() {
			finished <- streamSnapshotBatches(ctx, tableLayout{}, 0, scan, func(rows []DorisRow) error {
				submitted <- slices.Clone(rows)
				return nil
			})
		}()

		first, second, tail := snapshotBatchTestRow("first"), snapshotBatchTestRow("second"), snapshotBatchTestRow("tail")
		input <- first
		synctest.Wait()
		time.Sleep(4 * time.Second)
		input <- second
		synctest.Wait()
		time.Sleep(defaultDorisLoadBatchInterval - 4*time.Second - time.Nanosecond)
		synctest.Wait()
		require.Len(t, submitted, 0)
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.Len(t, submitted, 1)
		require.Equal(t, []DorisRow{first, second}, <-submitted)

		eof := time.Now()
		input <- tail
		close(input)
		require.NoError(t, <-finished)
		require.Equal(t, eof, time.Now())
		require.Len(t, submitted, 1)
		require.Equal(t, []DorisRow{tail}, <-submitted)
	})
}

// TestSnapshotBatchRespectsByteLimitAndExplicitRows 验证快照默认字节上限和显式行数配置。
func TestSnapshotBatchRespectsByteLimitAndExplicitRows(t *testing.T) {
	wide := snapshotBatchTestRow(strings.Repeat("x", defaultDorisLoadBatchBytes/2))
	input := []DorisRow{wide, wide, snapshotBatchTestRow("tail")}
	scan := func(_ context.Context, consume func(DorisRow) error) error {
		for _, row := range input {
			if err := consume(row); err != nil {
				return err
			}
		}
		return nil
	}
	for _, example := range []struct {
		name      string
		batchRows int
		want      []int
	}{
		{name: "default bytes", want: []int{1, 2}},
		{name: "explicit rows", batchRows: 2, want: []int{2, 1}},
	} {
		t.Run(example.name, func(t *testing.T) {
			var got []int
			err := streamSnapshotBatches(t.Context(), tableLayout{}, example.batchRows, scan, func(rows []DorisRow) error {
				got = append(got, len(rows))
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, example.want, got)
		})
	}
}

// snapshotBatchTestRow 构造只有一个文本字段的快照测试行。
func snapshotBatchTestRow(value string) DorisRow {
	return DorisRow{Values: []*string{&value}}
}
