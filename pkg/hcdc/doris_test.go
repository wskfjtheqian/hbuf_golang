package cdc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

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
	verified       bool
	auditQueries   int
	auditChecksum  string
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
			rows.values = []driver.Value{d.batch.Label, table, "Success", d.auditChecksum, int64(d.batch.RowCount), int64(d.batch.RowCount), int64(0), int64(0)}
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
	fields []string
	values []driver.Value
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
	r.values = nil
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
