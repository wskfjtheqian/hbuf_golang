package hcdc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
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

// managerTestPipelines 创建两个端点相同但缓冲目录、复制 ID 和目标库各自独立的源配置。
func managerTestPipelines(t *testing.T) (pipelineConfig, pipelineConfig) {
	t.Helper()
	directory := t.TempDir()
	first := pipelineConfig{
		Identity:  StreamIdentity{PipelineID: "test", SourceID: "first", Generation: "1"},
		Directory: filepath.Join(directory, "first"),
		Source: mysqlConfig{
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
	_, err := prepareDesiredConfig([]pipelineConfig{first, second})
	require.NoError(t, err)

	for _, scenario := range []struct {
		name  string
		edit  func(*pipelineConfig)
		error string
	}{
		{"invalid endpoint", func(p *pipelineConfig) { p.Doris.StreamLoadURL = "/invalid" }, "HTTP(S) URL"},
		{"reused source ID", func(p *pipelineConfig) { p.Identity.SourceID = first.Identity.SourceID }, "duplicate CDC source ID"},
		{"reused target", func(p *pipelineConfig) { p.TargetDatabase = first.TargetDatabase }, "target database or checkpoint identity"},
		{"reused directory", func(p *pipelineConfig) { p.Directory = first.Directory + string(filepath.Separator) + "." }, "share a buffer directory"},
		{"invalid zero date policy", func(p *pipelineConfig) { p.ZeroDatePolicy = "invalid" }, "zero date policy"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			manager := NewManager(t.Context())
			candidate := second
			scenario.edit(&candidate)
			err := manager.setConfig(t.Context(), nil, []pipelineConfig{first, candidate})
			require.ErrorContains(t, err, scenario.error)
			_, installed := manager.Pipeline(first.Identity.SourceID)
			require.False(t, installed)
		})
	}
}

// TestManagerEquivalentPipelines 验证等价比较、实际参数变更和重复提交时的快速返回。
func TestManagerEquivalentPipelines(t *testing.T) {
	first, second := managerTestPipelines(t)
	current := []pipelineConfig{first, second}

	updatedFirst := first
	updatedFirst.Source.Tables = []string{"items", "entries"}
	updatedFirst.Source.Flavor = "mysql"
	updatedFirst.Doris.ControlDatabase = "cdc_control"
	updatedFirst.ZeroDatePolicy = ZeroDateReject
	updated := []pipelineConfig{second, updatedFirst}
	require.True(t, equalPipelineConfig(first, updatedFirst))
	require.True(t, equalPipelineConfig(updatedFirst, first))
	require.Equal(t, []string{"items", "entries"}, updated[1].Source.Tables)

	updated[1].Source.Password = "new-password"
	require.False(t, equalPipelineConfig(first, updated[1]))
	updated[1] = updatedFirst
	updated[1].Doris.SQLDSN = "new-user@tcp(127.0.0.1:9030)/"
	require.False(t, equalPipelineConfig(first, updated[1]))
	updated[1] = updatedFirst
	updated[1].MaxPendingBytes = 1024
	require.False(t, equalPipelineConfig(first, updated[1]))
	updated[1] = updatedFirst
	updated[1].ZeroDatePolicy = ZeroDateEpoch
	_, err := prepareDesiredConfig(updated)
	require.NoError(t, err)
	require.False(t, equalPipelineConfig(first, updated[1]))
	require.False(t, equalPipelineConfig(updated[1], first))

	manager := NewManager(t.Context())
	prepared, err := prepareManagerConfig(current, nil)
	require.NoError(t, err)
	for id, config := range prepared {
		pipeline := &Pipeline{done: make(chan struct{}), status: PipelineStatus{State: PipelineRunning}}
		manager.sources[id] = managedSource{config: config, pipeline: pipeline}
	}
	_, err = prepareManagerConfig(updated, manager.sources)
	require.ErrorContains(t, err, "requires a new generation")
	updated[1] = updatedFirst

	before := manager.sources[first.Identity.SourceID].pipeline
	// 将原来的待建目录变为普通文件，使重新预检必定失败。
	require.NoError(t, os.WriteFile(first.Directory, nil, 0o600))
	_, err = prepareManagedConfig(first)
	require.Error(t, err)
	require.NoError(t, manager.setConfig(t.Context(), nil, updated))
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
	layouts, err := buildTableLayouts(pipelineConfig{TargetDatabase: "report"}, []mysqlsource.Table{table})
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
	layouts, err := buildTableLayouts(pipelineConfig{TargetDatabase: "report"}, []mysqlsource.Table{table})
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
	config := pipelineConfig{TargetDatabase: "report"}
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
	layouts, err := buildTableLayouts(pipelineConfig{TargetDatabase: "report"}, []mysqlsource.Table{table})
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

type bootstrapTestTarget struct {
	*deliveryTestTarget
	proof Checkpoint
}

// TestBootstrapRetainsInitialTables 验证新增表不会重写已绑定的快照清单和选表规则。
func TestBootstrapRetainsInitialTables(t *testing.T) {
	config, _ := managerTestPipelines(t)
	filter, err := compileCanalFilter([]string{"^entries$", "^items"}, nil, "IncludeTables", "ExcludeTables")
	require.NoError(t, err)
	config.tableDiscovery = &tableDiscoveryConfig{Interval: time.Second, Filter: filter}
	manifest, _, err := validatePipelineConfig(config)
	require.NoError(t, err)
	store, err := spool.Open(filepath.Join(config.Directory, "bootstrap"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	payload, err := json.Marshal(manifest)
	require.NoError(t, err)
	_, err = store.Bind(payload)
	require.NoError(t, err)

	discovered := config
	discovered.Source.Tables = []string{"entries", "items", "items_archive"}
	require.True(t, equalPipelineConfig(config, discovered))
	require.False(t, equalPipelineConfig(discovered, config))
	next, _, err := validatePipelineConfig(discovered)
	require.NoError(t, err)
	tables, err := recoverBootstrapTables(store, discovered, next)
	require.NoError(t, err)
	require.Equal(t, manifest.SourceTables, tables)
	bound, err := store.Metadata()
	require.NoError(t, err)
	require.Equal(t, payload, bound)

	reordered, err := compileCanalFilter([]string{"^items", "^entries$", "^items"}, nil, "IncludeTables", "ExcludeTables")
	require.NoError(t, err)
	discovered.tableDiscovery = &tableDiscoveryConfig{Interval: time.Second, Filter: reordered}
	require.True(t, equalPipelineConfig(config, discovered))
	_, err = recoverBootstrapTables(store, discovered, next)
	require.NoError(t, err)

	discovered.tableDiscovery = &tableDiscoveryConfig{Interval: time.Second}
	require.False(t, equalPipelineConfig(config, discovered))
	_, err = recoverBootstrapTables(store, discovered, next)
	require.ErrorContains(t, err, "a changed filter requires a new generation")

	next.SourceTables = []string{"items", "items_archive"}
	_, err = recoverBootstrapTables(store, config, next)
	require.ErrorContains(t, err, "table selection was removed or changed")
}

// TestStagedSnapshotModeSurvivesRestart 验证库首次全量的恢复模式和初始表清单在重启时保持绑定。
func TestStagedSnapshotModeSurvivesRestart(t *testing.T) {
	config, _ := managerTestPipelines(t)
	config.tableDiscovery = &tableDiscoveryConfig{Interval: time.Second}
	manifest, _, err := validatePipelineConfig(config)
	require.NoError(t, err)
	path := filepath.Join(config.Directory, "bootstrap")
	store, err := spool.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	mode, err := bindStagedSnapshotMode(store, manifest, false)
	require.NoError(t, err)
	require.False(t, mode)
	metadata, err := store.Metadata()
	require.NoError(t, err)
	require.Nil(t, metadata)
	mode, err = bindStagedSnapshotMode(store, manifest, true)
	require.NoError(t, err)
	require.True(t, mode)
	require.NoError(t, store.Close())

	store, err = spool.Open(path)
	require.NoError(t, err)
	config.Source.Tables = append(config.Source.Tables, "new_table")
	discovered, _, err := validatePipelineConfig(config)
	require.NoError(t, err)
	mode, err = bindStagedSnapshotMode(store, discovered, false)
	require.NoError(t, err)
	require.True(t, mode)
	discovered.StagedSnapshot = mode
	tables, err := recoverBootstrapTables(store, config, discovered)
	require.NoError(t, err)
	require.Equal(t, manifest.SourceTables, tables)

	legacy, err := spool.Open(filepath.Join(config.Directory, "legacy-bootstrap"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, legacy.Close()) })
	original, err := json.Marshal(manifest)
	require.NoError(t, err)
	_, err = legacy.Bind(original)
	require.NoError(t, err)
	mode, err = bindStagedSnapshotMode(legacy, discovered, true)
	require.NoError(t, err)
	require.False(t, mode)
	bound, err := legacy.Metadata()
	require.NoError(t, err)
	require.Equal(t, original, bound)
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
			config := pipelineConfig{Directory: fixture.Directory, Identity: fixture.Identity}
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
	config := pipelineConfig{Directory: fixture.Directory, Identity: fixture.Identity}
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
