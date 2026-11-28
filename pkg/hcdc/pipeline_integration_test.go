package cdc

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// skipCDCIntegration 允许本地单元测试跳过实库用例，在发布验收中将缺少配置视为失败。
func skipCDCIntegration(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("CDC_TEST_REQUIRE_INTEGRATION") == "1" {
		t.Fatal(reason)
	}
	t.Skip(reason)
}

// TestDorisWriterAndRetentionIntegration 验证不同进程的所有权冲突和真实控制表回收。
func TestDorisWriterAndRetentionIntegration(t *testing.T) {
	config := DorisConfig{
		SQLDSN: os.Getenv("CDC_TEST_DORIS_DSN"), FrontendURL: os.Getenv("CDC_TEST_DORIS_FE"), StreamLoadURL: os.Getenv("CDC_TEST_DORIS_BE"),
	}
	if config.SQLDSN == "" || config.FrontendURL == "" || config.StreamLoadURL == "" {
		skipCDCIntegration(t, "set the isolated Doris test endpoints")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	if business := os.Getenv("CDC_TEST_WRITER_CHILD_TARGET"); business != "" {
		config.ControlDatabase = os.Getenv("CDC_TEST_WRITER_CHILD_CONTROL")
		identity := StreamIdentity{PipelineID: "cdc_it", SourceID: "child", Generation: "g2"}
		owner, err := loadDorisWriterIdentity(t.TempDir(), business, identity)
		require.NoError(t, err)
		_, err = openDorisTarget(ctx, config, &owner)
		require.ErrorIs(t, err, ErrDorisWriterOwned)
		return
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	business := "cdc_it_writer_" + suffix
	config.ControlDatabase = "cdc_it_writer_ctl_" + suffix
	identity := StreamIdentity{PipelineID: "cdc_it", SourceID: business, Generation: "g1"}
	directory := t.TempDir()
	owner, err := loadDorisWriterIdentity(directory, business, identity)
	require.NoError(t, err)
	target, err := openDorisTarget(ctx, config, &owner)
	require.NoError(t, err)
	defer target.Close()
	t.Cleanup(func() {
		db, err := sql.Open("mysql", config.SQLDSN)
		if err != nil {
			t.Error(err)
			return
		}
		defer db.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		deleteTestWriter(t, cleanup, db, business, config.ControlDatabase, identity)
		_, err = db.ExecContext(cleanup, "DROP DATABASE IF EXISTS "+quoteDoris(config.ControlDatabase))
		if err != nil {
			t.Error(err)
		}
	})
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDorisWriterAndRetentionIntegration$")
	child.Env = append(os.Environ(), "CDC_TEST_WRITER_CHILD_TARGET="+business, "CDC_TEST_WRITER_CHILD_CONTROL="+config.ControlDatabase)
	output, err := child.CombinedOutput()
	require.NoError(t, err, "%s", output)
	recovered, err := loadDorisWriterIdentity(directory, business, identity)
	require.NoError(t, err)
	require.Equal(t, owner, recovered)
	require.NoError(t, target.claimWriter(ctx, recovered))

	require.NoError(t, target.ensureSchemaHistory(ctx))
	for _, receipt := range []struct {
		label    string
		sequence uint64
		verified bool
	}{{"confirmed", 1, true}, {"pending", 1, false}, {"recent", 2, true}} {
		require.NoError(t, target.trackLoadReceipt(withDorisLoadHistory(ctx, identity, receipt.sequence), receipt.label))
		_, err := target.db.ExecContext(ctx, "INSERT INTO "+target.controlTable("load_receipts")+" VALUES (?, 'business.events', 'checksum', 1, ?)", receipt.label, receipt.verified)
		require.NoError(t, err)
		_, err = target.db.ExecContext(ctx, "INSERT INTO "+target.controlTable("load_recheck_deadlines")+" VALUES (?, 1)", receipt.label)
		require.NoError(t, err)
	}
	for sequence := range 3 {
		_, err := target.db.ExecContext(ctx, "INSERT INTO "+target.controlTable("checkpoints")+" VALUES (?, ?, '{}')", dorisStreamKey(identity), sequence)
		require.NoError(t, err)
	}
	for range 2 {
		require.NoError(t, target.pruneControlHistory(ctx, dorisStreamKey(identity), 1))
	}
	var labels string
	require.NoError(t, target.db.QueryRowContext(ctx, "SELECT GROUP_CONCAT(load_label, ',' ORDER BY load_label) FROM "+target.controlTable("load_receipts")).Scan(&labels))
	require.Equal(t, "pending,recent", labels)
	require.NoError(t, target.db.QueryRowContext(ctx, "SELECT GROUP_CONCAT(load_label, ',' ORDER BY load_label) FROM "+target.controlTable("load_recheck_deadlines")).Scan(&labels))
	require.Equal(t, "pending,recent", labels)
	var checkpoints string
	require.NoError(t, target.db.QueryRowContext(ctx, "SELECT GROUP_CONCAT(CAST(sequence_no AS STRING), ',' ORDER BY sequence_no) FROM "+target.controlTable("checkpoints")).Scan(&checkpoints))
	require.Equal(t, "0,2", checkpoints)
}

// deleteTestWriter 清理测试自己创建且所有流水线均已关闭的注册对象。
func deleteTestWriter(t *testing.T, ctx context.Context, db *sql.DB, database, control string, identity StreamIdentity) {
	t.Helper()
	for _, object := range []string{
		dorisWriterObject("target", strings.ToLower(database)),
		dorisWriterObject("stream", strings.ToLower(control)+"/"+dorisStreamKey(identity)),
	} {
		if _, err := db.ExecContext(ctx, "DROP VIEW IF EXISTS "+object); err != nil {
			t.Logf("cleanup CDC writer record: %v", err)
		}
	}
}

// TestPipelineIntegration 在真实 MySQL 和 MariaDB 上验证全量、增量和断点续传。
func TestPipelineIntegration(t *testing.T) {
	dorisDSN := os.Getenv("CDC_TEST_DORIS_DSN")
	frontend := os.Getenv("CDC_TEST_DORIS_FE")
	backend := os.Getenv("CDC_TEST_DORIS_BE")
	if dorisDSN == "" || frontend == "" || backend == "" {
		skipCDCIntegration(t, "set CDC_TEST_DORIS_DSN, CDC_TEST_DORIS_FE and CDC_TEST_DORIS_BE for real database verification")
	}
	for _, flavor := range []string{"mysql", "mariadb"} {
		t.Run(flavor, func(t *testing.T) {
			sourceDSN := os.Getenv("CDC_TEST_MYSQL_DSN")
			if flavor == "mariadb" {
				sourceDSN = os.Getenv("CDC_TEST_MARIADB_DSN")
			}
			if sourceDSN == "" {
				skipCDCIntegration(t, "source test DSN is unset")
			}
			testRealPipeline(t, flavor, sourceDSN, DorisConfig{
				SQLDSN: dorisDSN, FrontendURL: frontend, StreamLoadURL: backend,
			})
		})
	}
}

// TestManagerIntegration 验证真实链路上的增删源、配置更新和同代次检查点续传。
func TestManagerIntegration(t *testing.T) {
	doris := DorisConfig{
		SQLDSN: os.Getenv("CDC_TEST_DORIS_DSN"), FrontendURL: os.Getenv("CDC_TEST_DORIS_FE"),
		StreamLoadURL: os.Getenv("CDC_TEST_DORIS_BE"),
	}
	mysqlDSN, mariaDSN := os.Getenv("CDC_TEST_MYSQL_DSN"), os.Getenv("CDC_TEST_MARIADB_DSN")
	if mysqlDSN == "" || mariaDSN == "" || doris.SQLDSN == "" || doris.FrontendURL == "" || doris.StreamLoadURL == "" {
		skipCDCIntegration(t, "set the isolated MySQL, MariaDB and Doris test endpoints")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	primary := newDDLIntegrationFixture(t, ctx, "mysql", mysqlDSN, doris)
	sibling := newDDLIntegrationFixture(t, ctx, "mariadb", mariaDSN, doris)
	serviceCtx, stopService := context.WithCancel(context.Background())
	manager := NewManager(serviceCtx)
	t.Cleanup(func() {
		defer stopService()
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, manager.SetConfig(stopCtx, nil))
	})
	primaryID, siblingID := primary.config.Identity.SourceID, sibling.config.Identity.SourceID
	installed := func(sourceID string) *Pipeline {
		t.Helper()
		pipeline, ok := manager.Pipeline(sourceID)
		require.True(t, ok, "source %s is not installed", sourceID)
		return pipeline
	}
	setConfig := func(configs ...PipelineConfig) {
		t.Helper()
		require.NoError(t, manager.SetConfig(ctx, &Config{Pipelines: configs}))
	}
	assertStopped := func(pipeline *Pipeline) {
		t.Helper()
		select {
		case <-pipeline.Done():
		default:
			t.Fatal("retired pipeline still owns its resources")
		}
	}

	requestCtx, cancelRequest := context.WithCancel(ctx)
	err := manager.SetConfig(requestCtx, &Config{Pipelines: []PipelineConfig{primary.config}})
	cancelRequest()
	require.NoError(t, err)
	first := installed(primaryID)
	waitPipelineState(t, ctx, first, PipelineRunning)
	seed := ddlIntegrationRow{id: 1, amount: 100}
	primary.waitRows(t, ctx, first, false, seed)

	setConfig(primary.config, sibling.config)
	second := installed(siblingID)
	waitPipelineState(t, ctx, second, PipelineRunning)
	sibling.waitRows(t, ctx, second, false, seed)
	setConfig(sibling.config, primary.config)
	require.Same(t, first, installed(primaryID))
	require.Same(t, second, installed(siblingID))

	invalid := primary.config
	invalid.Source.Tables = []string{"ledger", "extra"}
	changedSibling := sibling.config
	changedSibling.MaxPendingBytes = 8 << 20
	require.Error(t, manager.SetConfig(ctx, &Config{Pipelines: []PipelineConfig{changedSibling, invalid}}))
	require.Same(t, first, installed(primaryID))
	require.Same(t, second, installed(siblingID))

	initial := first.Status().Progress.Delivered.Sequence
	primary.executeSource(t, ctx, "INSERT INTO "+primary.sourceTable()+" VALUES (2, 200)")
	rows := []ddlIntegrationRow{seed, {id: 2, amount: 200}}
	primary.waitRows(t, ctx, first, false, rows...)
	require.True(t, waitDDLIntegration(ctx, func() bool {
		return first.Status().Progress.Delivered.Sequence > initial
	}), "incremental transaction was not confirmed before the configuration update")
	checkpoint := latestDestructiveCheckpoint(t, ctx, primary)
	updated := primary.config
	updated.MaxPendingBytes = 8 << 20
	setConfig(updated, sibling.config)
	restarted := installed(primaryID)
	require.NotSame(t, first, restarted)
	assertStopped(first)
	restartedStatus := waitPipelineState(t, ctx, restarted, PipelineRunning)
	require.Equal(t, checkpoint, restartedStatus.Progress.Delivered)
	require.Same(t, second, installed(siblingID))
	primary.waitRows(t, ctx, restarted, false, rows...)

	setConfig(sibling.config)
	_, present := manager.Pipeline(primaryID)
	require.False(t, present)
	assertStopped(restarted)
	require.Same(t, second, installed(siblingID))
	primary.executeSource(t, ctx, "INSERT INTO "+primary.sourceTable()+" VALUES (3, 300)")
	sibling.executeSource(t, ctx, "INSERT INTO "+sibling.sourceTable()+" VALUES (2, 200)")
	sibling.waitRows(t, ctx, second, false, rows...)
	setConfig(updated, sibling.config)
	resumed := installed(primaryID)
	waitPipelineState(t, ctx, resumed, PipelineRunning)
	primary.waitRows(t, ctx, resumed, false, append(rows, ddlIntegrationRow{id: 3, amount: 300})...)
	require.True(t, waitDDLIntegration(ctx, func() bool {
		return resumed.Status().Progress.Delivered.Sequence > checkpoint.Sequence
	}), "re-added source did not confirm the transaction written while it was stopped")

	require.NoError(t, manager.SetConfig(ctx, nil))
	for _, sourceID := range []string{primaryID, siblingID} {
		_, present := manager.Pipeline(sourceID)
		require.False(t, present)
	}
	assertStopped(resumed)
	assertStopped(second)
}

// TestManagerRecoveryIntegration 验证错误凭据、超时和并发配置更新时的回退与源隔离。
func TestManagerRecoveryIntegration(t *testing.T) {
	doris := DorisConfig{
		SQLDSN: os.Getenv("CDC_TEST_DORIS_DSN"), FrontendURL: os.Getenv("CDC_TEST_DORIS_FE"),
		StreamLoadURL: os.Getenv("CDC_TEST_DORIS_BE"),
	}
	mysqlDSN, mariaDSN := os.Getenv("CDC_TEST_MYSQL_DSN"), os.Getenv("CDC_TEST_MARIADB_DSN")
	if mysqlDSN == "" || mariaDSN == "" || doris.SQLDSN == "" || doris.FrontendURL == "" || doris.StreamLoadURL == "" {
		skipCDCIntegration(t, "set the isolated MySQL, MariaDB and Doris test endpoints")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	backend, err := url.Parse(doris.StreamLoadURL)
	require.NoError(t, err)
	frontend, err := url.Parse(doris.FrontendURL)
	require.NoError(t, err)
	var holdLoad, holdProbe atomic.Bool
	var puts atomic.Int64
	held, release, probeHeld := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseResponse := sync.OnceFunc(func() { close(release) })
	defer releaseResponse()
	proxy := httptest.NewServer(&httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			if strings.HasSuffix(request.In.URL.Path, "/_stream_load") {
				request.SetURL(backend)
				return
			}
			request.SetURL(frontend)
		},
		ModifyResponse: func(response *http.Response) error {
			if response.Request.URL.Query().Get("label") == "cdc_config_probe" && holdProbe.CompareAndSwap(true, false) {
				close(probeHeld)
				select {
				case <-response.Request.Context().Done():
					return response.Request.Context().Err()
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if response.Request.Method != http.MethodPut || !strings.HasSuffix(response.Request.URL.Path, "/_stream_load") {
				return nil
			}
			puts.Add(1)
			if holdLoad.CompareAndSwap(true, false) {
				close(held)
				select {
				case <-release:
					return nil
				case <-response.Request.Context().Done():
					return response.Request.Context().Err()
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
	})
	t.Cleanup(proxy.Close)
	proxiedDoris := doris
	proxiedDoris.StreamLoadURL, proxiedDoris.FrontendURL = proxy.URL, proxy.URL
	primary := newDDLIntegrationFixture(t, ctx, "mysql", mysqlDSN, proxiedDoris)
	sibling := newDDLIntegrationFixture(t, ctx, "mariadb", mariaDSN, doris)
	serviceCtx, stopService := context.WithCancel(context.Background())
	manager := NewManager(serviceCtx)
	t.Cleanup(func() {
		defer stopService()
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, manager.SetConfig(stopCtx, nil))
	})
	apply := func(ctx context.Context, primaryConfig, siblingConfig PipelineConfig) error {
		return manager.SetConfig(ctx, &Config{Pipelines: []PipelineConfig{primaryConfig, siblingConfig}})
	}
	running := func(t *testing.T, sourceID string) *Pipeline {
		t.Helper()
		var pipeline *Pipeline
		var status PipelineStatus
		require.True(t, waitDDLIntegration(ctx, func() bool {
			pipeline, _ = manager.Pipeline(sourceID)
			if pipeline == nil {
				return false
			}
			status = pipeline.Status()
			return status.State == PipelineRunning
		}), "source %s did not recover (state %s, error %v)", sourceID, status.State, status.Err)
		return pipeline
	}
	checkpoint := func(t *testing.T, fixture ddlIntegrationFixture, pipeline *Pipeline, sequence uint64) Checkpoint {
		t.Helper()
		require.True(t, waitDDLIntegration(ctx, func() bool {
			return pipeline.Status().Progress.Delivered.Sequence >= sequence
		}), "source %s did not deliver sequence %d", fixture.config.Identity.SourceID, sequence)
		persisted := latestDestructiveCheckpoint(t, ctx, fixture)
		require.Equal(t, sequence, persisted.Sequence)
		require.Equal(t, persisted, pipeline.Status().Progress.Delivered)
		return persisted
	}
	primaryID, siblingID := primary.config.Identity.SourceID, sibling.config.Identity.SourceID
	require.NoError(t, apply(ctx, primary.config, sibling.config))
	first, second := running(t, primaryID), running(t, siblingID)
	rows := []ddlIntegrationRow{{id: 1, amount: 100}, {id: 2, amount: 200}}
	primary.executeSource(t, ctx, "INSERT INTO "+primary.sourceTable()+" VALUES (2, 200)")
	sibling.executeSource(t, ctx, "INSERT INTO "+sibling.sourceTable()+" VALUES (2, 200)")
	primaryCheckpoint := checkpoint(t, primary, first, 1)
	siblingCheckpoint := checkpoint(t, sibling, second, 1)

	invalidUser := "no_cdc_" + strings.ToLower(rand.Text()[:16])
	badMySQL, badMariaDB, badDorisSQL, badDorisHTTP := primary.config, sibling.config, primary.config, primary.config
	badMySQL.Source.User, badMariaDB.Source.User = invalidUser, invalidUser
	badSQL, err := mysql.ParseDSN(doris.SQLDSN)
	require.NoError(t, err)
	badSQL.User = invalidUser
	badDorisSQL.Doris.SQLDSN = badSQL.FormatDSN()
	badDorisHTTP.Doris.Username, badDorisHTTP.Doris.Password = invalidUser, "invalid"
	for _, rejection := range []struct {
		name             string
		primary, sibling PipelineConfig
		unaffectedID     string
	}{
		{"mysql_credentials", badMySQL, sibling.config, siblingID},
		{"mariadb_credentials", primary.config, badMariaDB, primaryID},
		{"doris_sql_credentials", badDorisSQL, sibling.config, siblingID},
		{"doris_http_credentials", badDorisHTTP, sibling.config, siblingID},
	} {
		if !t.Run(rejection.name, func(t *testing.T) {
			unaffected := running(t, rejection.unaffectedID)
			require.Error(t, apply(ctx, rejection.primary, rejection.sibling))
			first, second = running(t, primaryID), running(t, siblingID)
			require.Same(t, unaffected, running(t, rejection.unaffectedID))
			require.Equal(t, primaryCheckpoint, checkpoint(t, primary, first, 1))
			require.Equal(t, siblingCheckpoint, checkpoint(t, sibling, second, 1))
			require.NoError(t, apply(ctx, primary.config, sibling.config))
			require.Same(t, first, running(t, primaryID), "primary should still use its original configuration")
			require.Same(t, second, running(t, siblingID), "sibling should still use its original configuration")
		}) {
			return
		}
	}

	updated, contender := primary.config, sibling.config
	updated.MaxPendingBytes = 8 << 20
	contender.MaxPendingBytes = 16 << 20
	if !t.Run("candidate_timeout", func(t *testing.T) {
		holdProbe.Store(true)
		probeCtx, stopProbe := context.WithTimeout(ctx, 3*time.Second)
		defer stopProbe()
		result := make(chan error, 1)
		go func() { result <- apply(probeCtx, updated, sibling.config) }()
		select {
		case <-probeHeld:
		case err := <-result:
			t.Fatalf("configuration update returned before Doris verification: %v", err)
		case <-probeCtx.Done():
			t.Fatal("configuration update did not reach Doris verification")
		}
		select {
		case err := <-result:
			require.ErrorIs(t, err, context.DeadlineExceeded)
		case <-ctx.Done():
			t.Fatal("timed out candidate did not return")
		}
		first = running(t, primaryID)
		require.Equal(t, primaryCheckpoint, checkpoint(t, primary, first, 1))
		require.Same(t, second, running(t, siblingID))
		require.NoError(t, apply(ctx, primary.config, sibling.config))
		require.Same(t, first, running(t, primaryID), "the timed out candidate should not replace the original configuration")
	}) {
		return
	}

	before := puts.Load()
	holdLoad.Store(true)
	rows = append(rows, ddlIntegrationRow{id: 3, amount: 300})
	primary.executeSource(t, ctx, "INSERT INTO "+primary.sourceTable()+" VALUES (3, 300)")
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("Doris did not respond to the gated Stream Load")
	}
	primary.waitRows(t, ctx, first, false, rows...)
	require.True(t, first.Status().InFlight)
	require.Equal(t, primaryCheckpoint, latestDestructiveCheckpoint(t, ctx, primary))
	updateCtx, stopUpdate := context.WithTimeout(ctx, 4*time.Second)
	defer stopUpdate()
	result := make(chan error, 1)
	go func() { result <- apply(updateCtx, updated, sibling.config) }()
	waitPipelineState(t, ctx, first, PipelineStopping)

	sibling.executeSource(t, ctx, "INSERT INTO "+sibling.sourceTable()+" VALUES (3, 300)")
	sibling.waitRows(t, ctx, second, false, rows...)
	checkpoint(t, sibling, second, 2)
	contenderCtx, stopContender := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stopContender()
	require.ErrorIs(t, apply(contenderCtx, primary.config, contender), context.DeadlineExceeded)
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-ctx.Done():
		t.Fatal("timed out configuration update did not return")
	}

	require.True(t, first.Status().InFlight, "an expired update must leave the original delivery running to completion")
	require.Equal(t, primaryCheckpoint, latestDestructiveCheckpoint(t, ctx, primary))
	recoveryCtx, stopRecoveryWait := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stopRecoveryWait()
	require.ErrorIs(t, apply(recoveryCtx, primary.config, contender), context.DeadlineExceeded,
		"a concurrent update cannot overtake recovery after the first caller returns")
	primary.executeSource(t, ctx, "INSERT INTO "+primary.sourceTable()+" VALUES (4, 400)")
	rows = append(rows, ddlIntegrationRow{id: 4, amount: 400})
	releaseResponse()
	restored := running(t, primaryID)
	require.Same(t, second, running(t, siblingID))
	final := checkpoint(t, primary, restored, 3)
	drained := first.Status().Progress.Delivered
	require.EqualValues(t, 2, drained.Sequence, "the old pipeline should confirm only its in-flight transaction")
	require.NoError(t, validateAdvance(drained.Cursor, final.Cursor))
	require.Equal(t, before+2, puts.Load(), "drain and catch-up must each submit exactly one Stream Load")
	require.NoError(t, apply(ctx, primary.config, sibling.config))
	require.Same(t, restored, running(t, primaryID), "timed out update should restore the original configuration")
	primary.waitRows(t, ctx, restored, false, rows...)
}

// testRealPipeline 在指定源上执行完整同步，并用真实 Doris 数据核对重启前后的结果。
func testRealPipeline(t *testing.T, flavor, sourceDSN string, dorisConfig DorisConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	sourceConfig, err := mysql.ParseDSN(sourceDSN)
	require.NoError(t, err)
	require.Equal(t, "tcp", sourceConfig.Net)
	sourceConfig.DBName = ""
	source, err := sql.Open("mysql", sourceConfig.FormatDSN())
	require.NoError(t, err)
	defer source.Close()
	doris, err := sql.Open("mysql", dorisConfig.SQLDSN)
	require.NoError(t, err)
	defer doris.Close()

	suffix := fmt.Sprintf("%s_%d", flavor, time.Now().UnixNano())
	sourceDB := "cdc_it_src_" + suffix
	targetDB := "cdc_it_dst_" + suffix
	identity := StreamIdentity{PipelineID: "integration", SourceID: flavor, Generation: suffix}
	dorisConfig.ControlDatabase = "cdc_it_ctl_" + suffix
	qualified := sourceDB + ".accounts"
	execSQL := func(statement string, args ...any) {
		t.Helper()
		_, err := source.ExecContext(ctx, statement, args...)
		require.NoError(t, err)
	}
	execSQL("CREATE DATABASE " + sourceDB)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		deleteTestWriter(t, cleanup, doris, targetDB, dorisConfig.ControlDatabase, identity)
		for _, item := range []struct {
			db   *sql.DB
			name string
		}{{source, sourceDB}, {doris, targetDB}, {doris, dorisConfig.ControlDatabase}} {
			if _, err := item.db.ExecContext(cleanup, "DROP DATABASE IF EXISTS "+item.name); err != nil {
				t.Logf("cleanup %s: %v", item.name, err)
			}
		}
	}()
	execSQL("CREATE TABLE " + qualified + " (note VARCHAR(100) NULL, id BIGINT NOT NULL PRIMARY KEY, score INT NOT NULL, exact_value BIGINT UNSIGNED NOT NULL, " +
		"flags BLOB NULL COMMENT 'CustomType=Bitmap32', members BLOB NULL COMMENT 'CustomType=Bitmap64', " +
		"elapsed TIME(6), raw_bytes VARBINARY(32), attributes JSON) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
	flags, err := roaring.BitmapOf(0, 4294967295).MarshalBinary()
	require.NoError(t, err)
	members, err := roaring64.BitmapOf(4294967296, 9223372036854775808, 18446744073709551615).MarshalBinary()
	require.NoError(t, err)
	emptyMembers, err := roaring64.NewBitmap().MarshalBinary()
	require.NoError(t, err)
	execSQL("INSERT INTO "+qualified+" VALUES ('seed', 1, 100, 18446744073709551615, ?, ?, '-36:02:03.125000', ?, ?), (NULL, 2, 200, 2, ?, ?, NULL, NULL, NULL)",
		flags, members, []byte{0, 255, 10}, `{"v":"seed"}`, flags, emptyMembers)

	config := PipelineConfig{
		Identity:  identity,
		Directory: t.TempDir(),
		Source: MySQLConfig{
			Address: sourceConfig.Addr, User: sourceConfig.User, Password: sourceConfig.Passwd,
			Flavor: flavor, SourceIdentity: suffix, ServerID: uint32(12000 + time.Now().UnixNano()%40000),
			Database: sourceDB, Tables: []string{"accounts"},
		},
		Doris: dorisConfig, TargetDatabase: targetDB,
	}
	type observedRow struct {
		Note      sql.NullString
		ID, Score int64
		Exact     string
	}
	waitRows := func(done <-chan error, expected ...observedRow) {
		t.Helper()
		timer := time.NewTicker(200 * time.Millisecond)
		defer timer.Stop()
		var observed []observedRow
		var lastErr error
		deadline := time.NewTimer(45 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case err := <-done:
				t.Fatalf("CDC pipeline stopped: %v (last rows: %v, last query error: %v)", err, observed, lastErr)
			case <-ctx.Done():
				t.Fatalf("database verification expired: %v, last rows: %v, last query error: %v", ctx.Err(), observed, lastErr)
			case <-deadline.C:
				t.Fatalf("expected rows %v, last rows: %v, last query error: %v", expected, observed, lastErr)
			case <-timer.C:
				observed = nil
				rows, err := doris.QueryContext(ctx, "SELECT note, id, score, CAST(exact_value AS STRING) FROM "+targetDB+".accounts ORDER BY id")
				lastErr = err
				if err != nil {
					continue
				}
				for rows.Next() {
					var item observedRow
					lastErr = rows.Scan(&item.Note, &item.ID, &item.Score, &item.Exact)
					if lastErr != nil {
						break
					}
					observed = append(observed, item)
				}
				if lastErr == nil {
					lastErr = rows.Err()
				}
				rows.Close()
				if lastErr == nil && equalObservedRows(observed, expected) {
					return
				}
			}
		}
	}
	start := func() (<-chan error, context.CancelFunc) {
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- RunPipeline(runCtx, config); close(done) }()
		return done, stop
	}
	stopAndWait := func(done <-chan error, stop context.CancelFunc) {
		t.Helper()
		stop()
		select {
		case err := <-done:
			require.True(t, err == nil || errors.Is(err, context.Canceled), "pipeline shutdown: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("pipeline did not stop")
		}
	}
	validNote := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	done, stop := start()
	defer stop()
	waitRows(done, observedRow{validNote("seed"), 1, 100, "18446744073709551615"}, observedRow{sql.NullString{}, 2, 200, "2"})
	verifyBitmap := func(id int, expectedMembers string, expectedSize int) {
		t.Helper()
		var flagsText, memberText string
		var size int
		err := doris.QueryRowContext(ctx, "SELECT bitmap_to_string(flags),bitmap_to_string(members),bitmap_count(members) FROM "+targetDB+".accounts WHERE id=?", id).Scan(&flagsText, &memberText, &size)
		require.NoError(t, err)
		require.Equal(t, "0,4294967295", flagsText)
		require.Equal(t, expectedMembers, memberText)
		require.Equal(t, expectedSize, size)
	}
	verifyBitmap(1, "4294967296,9223372036854775808,18446744073709551615", 3)
	verifyBitmap(2, "", 0)
	verifySpecialValues := func(id int, duration, encodedBytes, document sql.NullString) {
		t.Helper()
		var actualDuration, actualBytes, actualDocument sql.NullString
		err := doris.QueryRowContext(ctx, "SELECT elapsed,raw_bytes,CAST(attributes AS STRING) FROM "+targetDB+".accounts WHERE id=?", id).
			Scan(&actualDuration, &actualBytes, &actualDocument)
		require.NoError(t, err)
		require.Equal(t, duration, actualDuration)
		require.Equal(t, encodedBytes, actualBytes)
		require.Equal(t, document.Valid, actualDocument.Valid)
		if document.Valid {
			require.JSONEq(t, document.String, actualDocument.String)
		}
	}
	verifySpecialValues(1, validNote("-36:02:03.125"), validNote("AP8K"), validNote(`{"v":"seed"}`))
	verifySpecialValues(2, sql.NullString{}, sql.NullString{}, sql.NullString{})

	txn, err := source.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer txn.Rollback()
	_, err = txn.ExecContext(ctx, "UPDATE "+qualified+" SET id=3, score=101, note=?, members=?, elapsed='36:02:03.125000', raw_bytes=?, attributes=? WHERE id=1",
		"数据,行\n", emptyMembers, []byte{}, `{"v":[1,true]}`)
	require.NoError(t, err)
	_, err = txn.ExecContext(ctx, "DELETE FROM "+qualified+" WHERE id=2")
	require.NoError(t, err)
	_, err = txn.ExecContext(ctx, "INSERT INTO "+qualified+" VALUES ('', 4, 400, 4, ?, ?, '00:00:00', '', ?)", flags, members, `null`)
	require.NoError(t, err)
	require.NoError(t, txn.Commit())
	waitRows(done, observedRow{validNote("数据,行\n"), 3, 101, "18446744073709551615"}, observedRow{validNote(""), 4, 400, "4"})
	verifyBitmap(3, "", 0)
	verifyBitmap(4, "4294967296,9223372036854775808,18446744073709551615", 3)
	verifySpecialValues(3, validNote("36:02:03.125"), validNote(""), validNote(`{"v":[1,true]}`))
	verifySpecialValues(4, validNote("00:00:00"), validNote(""), validNote(`null`))
	target, err := OpenDorisTarget(ctx, dorisConfig)
	require.NoError(t, err)
	defer target.Close()
	readCheckpoint := func(minSequence uint64) Checkpoint {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			checkpoint, err := target.LoadCheckpoint(ctx, config.Identity)
			require.NoError(t, err)
			if checkpoint != nil && checkpoint.Sequence >= minSequence {
				return *checkpoint
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("Doris did not confirm checkpoint sequence %d", minSequence)
		return Checkpoint{}
	}
	before := readCheckpoint(1)
	stopAndWait(done, stop)

	execSQL("UPDATE " + qualified + " SET score = 402 WHERE id = 4")
	execSQL("FLUSH BINARY LOGS")
	execSQL("INSERT INTO "+qualified+" VALUES ('offline', 5, 500, 5, ?, ?, '-00:00:01', ?, ?)", flags, members, []byte{255}, `[]`)
	restarted, stopRestarted := start()
	defer stopRestarted()
	waitRows(restarted, observedRow{validNote("数据,行\n"), 3, 101, "18446744073709551615"},
		observedRow{validNote(""), 4, 402, "4"}, observedRow{validNote("offline"), 5, 500, "5"})
	after := readCheckpoint(before.Sequence + 2)
	require.NoError(t, validateAdvance(before.Cursor, after.Cursor))
	require.NotEqual(t, before.Cursor.File, after.Cursor.File)
	verifyBitmap(5, "4294967296,9223372036854775808,18446744073709551615", 3)
	verifySpecialValues(5, validNote("-00:00:01"), validNote("/w=="), validNote(`[]`))
	execSQL("CREATE TABLE " + sourceDB + ".excluded_events (id INT PRIMARY KEY)")
	excludedDDL := readCheckpoint(after.Sequence + 1)
	countLoadReceipts := func() uint64 {
		t.Helper()
		var count uint64
		require.NoError(t, doris.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+dorisConfig.ControlDatabase+".load_receipts").Scan(&count))
		return count
	}
	loadReceipts := countLoadReceipts()
	execSQL("INSERT INTO " + sourceDB + ".excluded_events VALUES (1)")
	excludedDML := readCheckpoint(excludedDDL.Sequence + 1)
	require.NoError(t, validateAdvance(excludedDDL.Cursor, excludedDML.Cursor))
	require.Equal(t, loadReceipts, countLoadReceipts())
	t.Logf("%s snapshot, incremental transaction and restart confirmed: %s:%d -> %s:%d; GTID=%t",
		flavor, before.Cursor.File, before.Cursor.Position, after.Cursor.File, after.Cursor.Position, after.Cursor.GTIDSet != "")
	stopAndWait(restarted, stopRestarted)
}

// equalObservedRows 按顺序比较实际读取的行与期望的行。
func equalObservedRows[T comparable](got, want []T) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

type ddlIntegrationFixture struct {
	config PipelineConfig
	source *sql.DB
	doris  *sql.DB
}

type ddlIntegrationRow struct {
	id     int64
	amount int64
	note   sql.NullString
}

type ddlIntegrationSchemaVersion struct {
	revision string
	catalog  schemaVersion
}

// TestPipelineDDLIntegration 在两种源引擎上验证加列顺序与清表审批的重启恢复。
func TestPipelineDDLIntegration(t *testing.T) {
	dorisConfig := DorisConfig{
		SQLDSN:        os.Getenv("CDC_TEST_DORIS_DSN"),
		FrontendURL:   os.Getenv("CDC_TEST_DORIS_FE"),
		StreamLoadURL: os.Getenv("CDC_TEST_DORIS_BE"),
	}
	if dorisConfig.SQLDSN == "" || dorisConfig.FrontendURL == "" || dorisConfig.StreamLoadURL == "" {
		skipCDCIntegration(t, "set CDC_TEST_DORIS_DSN, CDC_TEST_DORIS_FE and CDC_TEST_DORIS_BE for real DDL verification")
	}
	for _, flavor := range []string{"mysql", "mariadb"} {
		t.Run(flavor, func(t *testing.T) {
			sourceDSN := os.Getenv("CDC_TEST_MYSQL_DSN")
			if flavor == "mariadb" {
				sourceDSN = os.Getenv("CDC_TEST_MARIADB_DSN")
			}
			if sourceDSN == "" {
				skipCDCIntegration(t, "source test DSN is unset")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			t.Cleanup(cancel)
			fixture := newDDLIntegrationFixture(t, ctx, flavor, sourceDSN, dorisConfig)
			testRealPipelineDDL(t, ctx, fixture)
		})
	}
}

// newDDLIntegrationFixture 创建并注册清理隔离源库、Doris 目标库和控制库，生成测试流水线配置。
func newDDLIntegrationFixture(t *testing.T, ctx context.Context, flavor, sourceDSN string, dorisConfig DorisConfig) ddlIntegrationFixture {
	t.Helper()
	sourceConnection, err := mysql.ParseDSN(sourceDSN)
	require.NoError(t, err)
	require.Equal(t, "tcp", sourceConnection.Net)
	sourceConnection.DBName = ""
	source, err := sql.Open("mysql", sourceConnection.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { _ = source.Close() })
	doris, err := sql.Open("mysql", dorisConfig.SQLDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = doris.Close() })

	suffix := fmt.Sprintf("%s_%d", flavor, time.Now().UnixNano())
	sourceDatabase := "cdc_ddl_src_" + suffix
	targetDatabase := "cdc_ddl_dst_" + suffix
	dorisConfig.ControlDatabase = "cdc_ddl_ctl_" + suffix
	config := PipelineConfig{
		Identity:  StreamIdentity{PipelineID: "ddl_integration", SourceID: flavor, Generation: suffix},
		Directory: t.TempDir(),
		Source: MySQLConfig{
			Address:        sourceConnection.Addr,
			User:           sourceConnection.User,
			Password:       sourceConnection.Passwd,
			Flavor:         flavor,
			SourceIdentity: suffix,
			ServerID:       uint32(62000 + time.Now().UnixNano()%40000),
			Database:       sourceDatabase,
			Tables:         []string{"ledger"},
		},
		Doris:             dorisConfig,
		TargetDatabase:    targetDatabase,
		SnapshotBatchRows: 1,
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		deleteTestWriter(t, cleanup, doris, targetDatabase, dorisConfig.ControlDatabase, config.Identity)
		for _, database := range []struct {
			db   *sql.DB
			name string
		}{{source, sourceDatabase}, {doris, targetDatabase}, {doris, dorisConfig.ControlDatabase}} {
			if _, err := database.db.ExecContext(cleanup, "DROP DATABASE IF EXISTS "+database.name); err != nil {
				t.Logf("cleanup %s: %v", database.name, err)
			}
		}
	})
	fixture := ddlIntegrationFixture{config: config, source: source, doris: doris}
	fixture.executeSource(t, ctx, "CREATE DATABASE "+sourceDatabase)
	fixture.executeSource(t, ctx, "CREATE TABLE "+fixture.sourceTable()+
		" (id BIGINT NOT NULL PRIMARY KEY, amount INT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
	fixture.executeSource(t, ctx, "INSERT INTO "+fixture.sourceTable()+" VALUES (1, 100)")
	return fixture
}

// sourceTable 返回集成测试源表的限定名称。
func (fixture ddlIntegrationFixture) sourceTable() string {
	return fixture.config.Source.Database + ".ledger"
}

// executeSource 在隔离源连接上执行指定 SQL，并检查执行结果。
func (fixture ddlIntegrationFixture) executeSource(t *testing.T, ctx context.Context, statement string) {
	t.Helper()
	_, err := fixture.source.ExecContext(ctx, statement)
	require.NoError(t, err)
}

// rows 按主键读取 Doris 目标行，可同时读取 DDL 新增的列。
func (fixture ddlIntegrationFixture) rows(ctx context.Context, withNote bool) ([]ddlIntegrationRow, error) {
	columns := "id, amount"
	if withNote {
		columns += ", note"
	}
	rows, err := fixture.doris.QueryContext(ctx, "SELECT "+columns+" FROM "+fixture.config.TargetDatabase+".ledger ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []ddlIntegrationRow
	for rows.Next() {
		var row ddlIntegrationRow
		fields := []any{&row.id, &row.amount}
		if withNote {
			fields = append(fields, &row.note)
		}
		if err := rows.Scan(fields...); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	return values, rows.Err()
}

// checkpoints 按序号读回当前流的 Doris 持久检查点。
func (fixture ddlIntegrationFixture) checkpoints(t *testing.T, ctx context.Context) []Checkpoint {
	t.Helper()
	rows, err := fixture.doris.QueryContext(ctx, "SELECT checkpoint_json FROM "+fixture.config.Doris.ControlDatabase+
		".checkpoints WHERE stream_key='"+dorisStreamKey(fixture.config.Identity)+"' ORDER BY sequence_no")
	require.NoError(t, err)
	defer rows.Close()
	var checkpoints []Checkpoint
	for rows.Next() {
		var encoded string
		require.NoError(t, rows.Scan(&encoded))
		var checkpoint Checkpoint
		require.NoError(t, json.Unmarshal([]byte(encoded), &checkpoint))
		checkpoints = append(checkpoints, checkpoint)
	}
	require.NoError(t, rows.Err())
	return checkpoints
}

// schemaVersions 按序号查询并解码当前流在 Doris 中的结构版本。
func (fixture ddlIntegrationFixture) schemaVersions(t *testing.T, ctx context.Context) []ddlIntegrationSchemaVersion {
	t.Helper()
	rows, err := fixture.doris.QueryContext(ctx, "SELECT sequence_no, structure_revision, source_catalog FROM "+
		fixture.config.Doris.ControlDatabase+".schema_versions WHERE stream_key='"+
		dorisStreamKey(fixture.config.Identity)+"' ORDER BY sequence_no")
	require.NoError(t, err)
	defer rows.Close()
	var versions []ddlIntegrationSchemaVersion
	for rows.Next() {
		var version ddlIntegrationSchemaVersion
		var sequence uint64
		var encoded []byte
		require.NoError(t, rows.Scan(&sequence, &version.revision, &encoded))
		require.NoError(t, json.Unmarshal(encoded, &version.catalog))
		require.Equal(t, sequence, version.catalog.Sequence)
		require.Equal(t, digest(encoded), version.revision)
		versions = append(versions, version)
	}
	require.NoError(t, rows.Err())
	return versions
}

// waitRows 等待 Doris 实际行与期望一致，并检查流水线是否提前失败。
func (fixture ddlIntegrationFixture) waitRows(t *testing.T, ctx context.Context, pipeline *Pipeline, withNote bool, expected ...ddlIntegrationRow) {
	t.Helper()
	var lastRows []ddlIntegrationRow
	var lastErr error
	matched := waitDDLIntegration(ctx, func() bool {
		lastRows, lastErr = fixture.rows(ctx, withNote)
		return pipeline.Status().State == PipelineFailed || (lastErr == nil && equalObservedRows(lastRows, expected))
	})
	require.NotEqual(t, PipelineFailed, pipeline.Status().State, "pipeline error: %v", pipeline.Status().Err)
	require.True(t, matched, "expected target rows %v, got %v (%v)", expected, lastRows, lastErr)
}

// waitDDLIntegration 重复检查集成测试的就绪条件，直到满足或上下文结束。
func waitDDLIntegration(ctx context.Context, ready func() bool) bool {
	deadline := time.NewTimer(40 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		if ready() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-poll.C:
		}
	}
}

// testRealPipelineDDL 验证兼容加列、清表屏障审批及重启后的结构和交付顺序。
func testRealPipelineDDL(t *testing.T, ctx context.Context, fixture ddlIntegrationFixture) {
	t.Helper()
	start := func(state PipelineState) *Pipeline {
		t.Helper()
		pipeline, err := StartPipeline(ctx, fixture.config)
		require.NoError(t, err)
		t.Cleanup(func() {
			stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := pipeline.Close(stop); err != nil {
				t.Errorf("close CDC pipeline: %v", err)
			}
		})
		waitPipelineState(t, ctx, pipeline, state)
		return pipeline
	}
	closePipeline := func(pipeline *Pipeline) {
		t.Helper()
		stop, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		require.NoError(t, pipeline.Close(stop))
		select {
		case <-pipeline.Done():
		case <-stop.Done():
			t.Fatalf("pipeline Done was not closed: %v", stop.Err())
		}
		require.Equal(t, PipelineStopped, pipeline.Status().State)
	}
	waitCheckpointAfter := func(previous uint64) Checkpoint {
		t.Helper()
		var latest Checkpoint
		matched := waitDDLIntegration(ctx, func() bool {
			checkpoints := fixture.checkpoints(t, ctx)
			if len(checkpoints) != 0 {
				latest = checkpoints[len(checkpoints)-1]
			}
			return latest.Sequence > previous
		})
		require.True(t, matched, "Doris checkpoint did not advance past %d: %#v", previous, latest)
		return latest
	}

	seed := ddlIntegrationRow{id: 1, amount: 100}
	beforeDDL := ddlIntegrationRow{id: 2, amount: 200}
	pipeline := start(PipelineRunning)
	fixture.waitRows(t, ctx, pipeline, false, seed)
	fixture.executeSource(t, ctx, "INSERT INTO "+fixture.sourceTable()+" VALUES (2, 200)")
	fixture.waitRows(t, ctx, pipeline, false, seed, beforeDDL)
	before := waitCheckpointAfter(0)

	fixture.executeSource(t, ctx, "ALTER TABLE "+fixture.sourceTable()+" ADD COLUMN note VARCHAR(32) NULL FIRST")
	fixture.executeSource(t, ctx, "UPDATE "+fixture.sourceTable()+" SET note='after add', amount=201 WHERE id=2")
	afterDDL := ddlIntegrationRow{id: 2, amount: 201, note: sql.NullString{String: "after add", Valid: true}}
	fixture.waitRows(t, ctx, pipeline, true, seed, afterDDL)
	after := waitCheckpointAfter(before.Sequence + 1)
	require.NoError(t, validateAdvance(before.Cursor, after.Cursor))
	require.NotEmpty(t, after.StructureRevision)
	require.NotEqual(t, before.StructureRevision, after.StructureRevision)
	var schemaBarrier *Checkpoint
	for _, checkpoint := range fixture.checkpoints(t, ctx) {
		if checkpoint.Sequence > before.Sequence && checkpoint.Sequence < after.Sequence &&
			checkpoint.StructureRevision == after.StructureRevision {
			schemaBarrier = &checkpoint
			break
		}
	}
	require.NotNil(t, schemaBarrier, "new structure revision must be checkpointed before DML that uses it")

	fixture.executeSource(t, ctx, "TRUNCATE TABLE "+fixture.sourceTable())
	pending := waitPipelineState(t, ctx, pipeline, PipelineAwaitingApproval).PendingDDL
	require.NotNil(t, pending)
	require.Equal(t, DDLTruncate, pending.Kind)
	require.NotEmpty(t, pending.ID)
	fixture.executeSource(t, ctx, "INSERT INTO "+fixture.sourceTable()+" (id, amount, note) VALUES (3, 300, 'after truncate')")
	require.True(t, waitDDLIntegration(ctx, func() bool {
		return pipeline.Status().Progress.Durable.Sequence > pending.Sequence
	}), "capture did not durably queue the DML behind the unapproved barrier")
	rows, err := fixture.rows(ctx, true)
	require.NoError(t, err)
	require.Equal(t, []ddlIntegrationRow{seed, afterDDL}, rows)
	checkpoints := fixture.checkpoints(t, ctx)
	require.Equal(t, pending.Sequence-1, checkpoints[len(checkpoints)-1].Sequence,
		"unapproved DDL and following DML must remain behind the barrier")
	require.Error(t, pipeline.ApproveDDL(ctx, pending.ID+"-invalid"))
	closePipeline(pipeline)

	pipeline = start(PipelineAwaitingApproval)
	restored := pipeline.Status().PendingDDL
	require.NotNil(t, restored)
	require.Equal(t, pending.ID, restored.ID)
	rows, err = fixture.rows(ctx, true)
	require.NoError(t, err)
	require.Equal(t, []ddlIntegrationRow{seed, afterDDL}, rows)
	require.NoError(t, pipeline.Pause(ctx, "hold approved DDL across restart"))
	require.NoError(t, pipeline.ApproveDDL(ctx, restored.ID))
	waitPipelineState(t, ctx, pipeline, PipelinePaused)
	rows, err = fixture.rows(ctx, true)
	require.NoError(t, err)
	require.Equal(t, []ddlIntegrationRow{seed, afterDDL}, rows)
	closePipeline(pipeline)

	pipeline = start(PipelinePaused)
	require.NoError(t, pipeline.Resume(ctx))
	afterTruncate := ddlIntegrationRow{id: 3, amount: 300, note: sql.NullString{String: "after truncate", Valid: true}}
	fixture.waitRows(t, ctx, pipeline, true, afterTruncate)
	approved := waitCheckpointAfter(after.Sequence + 1)
	require.NoError(t, validateAdvance(after.Cursor, approved.Cursor))
	versions := fixture.schemaVersions(t, ctx)
	require.Len(t, versions, 3)
	require.Zero(t, versions[0].catalog.Sequence)
	require.Equal(t, before.StructureRevision, versions[0].revision)
	require.Equal(t, schemaBarrier.Sequence, versions[1].catalog.Sequence)
	require.Equal(t, schemaBarrier.Cursor, versions[1].catalog.Cursor)
	require.Equal(t, schemaBarrier.StructureRevision, versions[1].revision)
	require.Equal(t, pending.Sequence, versions[2].catalog.Sequence)
	require.Equal(t, pending.Cursor, versions[2].catalog.Cursor)
	require.Equal(t, approved.StructureRevision, versions[2].revision)
	require.NotEqual(t, versions[1].revision, versions[2].revision)
	for index, expected := range [][]string{{"id", "amount"}, {"note", "id", "amount"}, {"note", "id", "amount"}} {
		require.Len(t, versions[index].catalog.Tables, 1)
		var names []string
		for _, column := range versions[index].catalog.Tables[0].Columns {
			names = append(names, column.Name)
		}
		require.Equal(t, expected, names, "source physical columns in version %d", index)
	}
	closePipeline(pipeline)

	pipeline = start(PipelineRunning)
	fixture.executeSource(t, ctx, "INSERT INTO "+fixture.sourceTable()+" (id, amount, note) VALUES (4, 400, 'after restart')")
	finalRows := []ddlIntegrationRow{
		afterTruncate, {id: 4, amount: 400, note: sql.NullString{String: "after restart", Valid: true}},
	}
	fixture.waitRows(t, ctx, pipeline, true, finalRows...)
	restarted := waitCheckpointAfter(approved.Sequence)
	require.NoError(t, validateAdvance(approved.Cursor, restarted.Cursor))
	require.Equal(t, approved.StructureRevision, restarted.StructureRevision)
	closePipeline(pipeline)
}

// waitPipelineState 等待流水线达到指定状态，并返回该时刻的状态快照。
func waitPipelineState(t *testing.T, ctx context.Context, pipeline *Pipeline, expected PipelineState) PipelineStatus {
	t.Helper()
	var latest PipelineStatus
	matched := waitDDLIntegration(ctx, func() bool {
		latest = pipeline.Status()
		return latest.State == expected || latest.State == PipelineFailed
	})
	require.True(t, matched, "expected pipeline state %v, got %v (%v)", expected, latest.State, latest.Err)
	require.Equal(t, expected, latest.State, "pipeline error: %v", latest.Err)
	return latest
}

// TestDestructiveDDLIntegration 在真实双引擎链路上验证可审批变更和需要新代次的变更。
func TestDestructiveDDLIntegration(t *testing.T) {
	doris := DorisConfig{
		SQLDSN: os.Getenv("CDC_TEST_DORIS_DSN"), FrontendURL: os.Getenv("CDC_TEST_DORIS_FE"),
		StreamLoadURL: os.Getenv("CDC_TEST_DORIS_BE"),
	}
	if os.Getenv("CDC_TEST_DESTRUCTIVE_DDL") != "1" || doris.SQLDSN == "" || doris.FrontendURL == "" || doris.StreamLoadURL == "" {
		skipCDCIntegration(t, "set CDC_TEST_DESTRUCTIVE_DDL=1 and the real Doris test endpoints")
	}
	for _, source := range []struct{ flavor, dsn string }{
		{"mysql", os.Getenv("CDC_TEST_MYSQL_DSN")},
		{"mariadb", os.Getenv("CDC_TEST_MARIADB_DSN")},
	} {
		t.Run(source.flavor, func(t *testing.T) {
			if source.dsn == "" {
				skipCDCIntegration(t, "source test DSN is unset")
			}
			t.Run("approved_changes", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				fixture := newDDLIntegrationFixture(t, ctx, source.flavor, source.dsn, doris)
				testApprovedDestructiveChanges(t, ctx, fixture)
			})
			t.Run("narrowing_requires_rebuild", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				fixture := newDDLIntegrationFixture(t, ctx, source.flavor, source.dsn, doris)
				testNarrowingRequiresRebuild(t, ctx, fixture)
			})
		})
	}
}

// testApprovedDestructiveChanges 验证删列、改名、整数扩宽经审批交付，并检查各阶段的持久位点。
func testApprovedDestructiveChanges(t *testing.T, ctx context.Context, fixture ddlIntegrationFixture) {
	t.Helper()
	table := fixture.sourceTable()
	fixture.executeSource(t, ctx, "ALTER TABLE "+table+" MODIFY COLUMN amount INT NULL DEFAULT NULL")
	fixture.executeSource(t, ctx, "ALTER TABLE "+table+" ADD COLUMN legacy_name VARCHAR(32) NULL, ADD COLUMN disposable VARCHAR(32) NULL")
	fixture.executeSource(t, ctx, "UPDATE "+table+" SET legacy_name='one', disposable='discard-one' WHERE id=1")
	pipeline := startDestructivePipeline(t, ctx, fixture.config, PipelineRunning)

	tests := []struct {
		kind       DDLKind
		priorDML   string
		ddl        string
		laterDML   string
		oldColumns []string
		newColumns []string
		oldRows    [][]string
		newRows    [][]string
	}{
		{
			kind:       DDLDropColumn,
			priorDML:   "INSERT INTO " + table + " VALUES (2, 200, 'two', 'discard-two')",
			ddl:        "ALTER TABLE " + table + " DROP COLUMN disposable",
			laterDML:   "INSERT INTO " + table + " VALUES (3, 300, 'three')",
			oldColumns: []string{"id", "amount", "legacy_name", "disposable"},
			newColumns: []string{"id", "amount", "legacy_name"},
			oldRows:    [][]string{{"1", "100", "one", "discard-one"}, {"2", "200", "two", "discard-two"}},
			newRows:    [][]string{{"1", "100", "one"}, {"2", "200", "two"}, {"3", "300", "three"}},
		},
		{
			kind:       DDLRenameColumn,
			priorDML:   "UPDATE " + table + " SET legacy_name='renamed-two' WHERE id=2",
			ddl:        "ALTER TABLE " + table + " RENAME COLUMN legacy_name TO entry_name",
			laterDML:   "INSERT INTO " + table + " VALUES (4, 400, 'four')",
			oldColumns: []string{"id", "amount", "legacy_name"},
			newColumns: []string{"id", "amount", "entry_name"},
			oldRows:    [][]string{{"1", "100", "one"}, {"2", "200", "renamed-two"}, {"3", "300", "three"}},
			newRows:    [][]string{{"1", "100", "one"}, {"2", "200", "renamed-two"}, {"3", "300", "three"}, {"4", "400", "four"}},
		},
		{
			kind:       DDLWidenInteger,
			priorDML:   "UPDATE " + table + " SET amount=301 WHERE id=3",
			ddl:        "ALTER TABLE " + table + " MODIFY COLUMN amount BIGINT NULL",
			laterDML:   "UPDATE " + table + " SET amount=3000000000 WHERE id=4",
			oldColumns: []string{"id", "amount", "entry_name"},
			newColumns: []string{"id", "amount", "entry_name"},
			oldRows:    [][]string{{"1", "100", "one"}, {"2", "200", "renamed-two"}, {"3", "301", "three"}, {"4", "400", "four"}},
			newRows:    [][]string{{"1", "100", "one"}, {"2", "200", "renamed-two"}, {"3", "301", "three"}, {"4", "3000000000", "four"}},
		},
	}
	for _, test := range tests {
		require.NoError(t, pipeline.Pause(ctx, "queue DML on both sides of "+string(test.kind)))
		beforePause := pipeline.Status().Progress.Delivered
		fixture.executeSource(t, ctx, test.priorDML)
		fixture.executeSource(t, ctx, test.ddl)
		fixture.executeSource(t, ctx, test.laterDML)
		require.True(t, waitDDLIntegration(ctx, func() bool {
			status := pipeline.Status()
			return status.State == PipelineFailed || status.Progress.Durable.Sequence >= beforePause.Sequence+2
		}), "capture should queue %s behind the preceding DML, status: %+v", test.kind, pipeline.Status())
		require.NotEqual(t, PipelineFailed, pipeline.Status().State, "pipeline error: %v", pipeline.Status().Err)
		require.Equal(t, beforePause, pipeline.Status().Progress.Delivered)
		require.NoError(t, pipeline.Resume(ctx))

		pending := waitPipelineState(t, ctx, pipeline, PipelineAwaitingApproval).PendingDDL
		require.NotNil(t, pending)
		require.Equal(t, test.kind, pending.Kind, "unexpected barrier: %+v", pending)
		require.NotEmpty(t, pending.ID)
		if test.kind == DDLWidenInteger {
			require.Equal(t, pending.Before, pending.After, "source widening must not change the Doris layout")
		}
		require.True(t, waitDDLIntegration(ctx, func() bool {
			status := pipeline.Status()
			return status.State == PipelineFailed || status.Progress.Durable.Sequence > pending.Sequence
		}), "following DML should be durable behind %s", test.kind)
		require.NotEqual(t, PipelineFailed, pipeline.Status().State, "pipeline error: %v", pipeline.Status().Err)
		require.Equal(t, pending.Sequence-1, latestDestructiveCheckpoint(t, ctx, fixture).Sequence)
		assertDestructiveRows(t, ctx, fixture, test.oldColumns, test.oldRows)
		require.Error(t, pipeline.ApproveDDL(ctx, pending.ID+"-wrong"))
		closeDestructivePipeline(t, ctx, pipeline)

		pipeline = startDestructivePipeline(t, ctx, fixture.config, PipelineAwaitingApproval)
		require.Equal(t, pending, pipeline.Status().PendingDDL)
		require.NoError(t, pipeline.Pause(ctx, "persist approval before dispatch"))
		require.NoError(t, pipeline.ApproveDDL(ctx, pending.ID))
		waitPipelineState(t, ctx, pipeline, PipelinePaused)
		assertDestructiveRows(t, ctx, fixture, test.oldColumns, test.oldRows)
		closeDestructivePipeline(t, ctx, pipeline)

		pipeline = startDestructivePipeline(t, ctx, fixture.config, PipelinePaused)
		require.NoError(t, pipeline.Resume(ctx))
		require.True(t, waitDDLIntegration(ctx, func() bool {
			status := pipeline.Status()
			return status.State == PipelineFailed || status.Progress.Delivered.Sequence > pending.Sequence
		}), "%s did not deliver its following DML", test.kind)
		require.NotEqual(t, PipelineFailed, pipeline.Status().State, "pipeline error: %v", pipeline.Status().Err)
		latest := latestDestructiveCheckpoint(t, ctx, fixture)
		require.Greater(t, latest.Sequence, pending.Sequence)
		assertDestructiveRows(t, ctx, fixture, test.newColumns, test.newRows)
		require.Equal(t, test.newRows, readDestructiveRows(t, ctx, fixture.source, table, test.newColumns))
		assertDestructiveCheckpoint(t, ctx, fixture, pending, latest)
		t.Logf("%s %s verified at checkpoint %d, following DML at %d", fixture.config.Source.Flavor, test.kind, pending.Sequence, latest.Sequence)
	}
	closeDestructivePipeline(t, ctx, pipeline)

	last := tests[len(tests)-1]
	pipeline = startDestructivePipeline(t, ctx, fixture.config, PipelineRunning)
	previous := latestDestructiveCheckpoint(t, ctx, fixture)
	fixture.executeSource(t, ctx, "INSERT INTO "+table+" VALUES (5, 5000000000, 'after restart')")
	require.True(t, waitDDLIntegration(ctx, func() bool {
		status := pipeline.Status()
		return status.State == PipelineFailed || status.Progress.Delivered.Sequence > previous.Sequence
	}), "widened schema must remain usable after restart")
	require.NotEqual(t, PipelineFailed, pipeline.Status().State, "pipeline error: %v", pipeline.Status().Err)
	finalRows := append(last.newRows, []string{"5", "5000000000", "after restart"})
	assertDestructiveRows(t, ctx, fixture, last.newColumns, finalRows)
	require.Equal(t, finalRows, readDestructiveRows(t, ctx, fixture.source, table, last.newColumns))
	require.Equal(t, previous.StructureRevision, latestDestructiveCheckpoint(t, ctx, fixture).StructureRevision)
	require.Len(t, fixture.schemaVersions(t, ctx), 4, "restarting DML must not create another DDL version")
	closeDestructivePipeline(t, ctx, pipeline)
}

// testNarrowingRequiresRebuild 验证窄化屏障阻塞原代次，且新代次全量重建后的数据一致。
func testNarrowingRequiresRebuild(t *testing.T, ctx context.Context, fixture ddlIntegrationFixture) {
	t.Helper()
	table := fixture.sourceTable()
	fixture.executeSource(t, ctx, "ALTER TABLE "+table+" ADD COLUMN disposable VARCHAR(32) NULL")
	fixture.executeSource(t, ctx, "UPDATE "+table+" SET disposable='preserve' WHERE id=1")
	pipeline := startDestructivePipeline(t, ctx, fixture.config, PipelineRunning)
	columns := []string{"id", "amount", "disposable"}
	before := [][]string{{"1", "100", "preserve"}}
	assertDestructiveRows(t, ctx, fixture, columns, before)
	fixture.executeSource(t, ctx, "ALTER TABLE "+table+" MODIFY COLUMN amount SMALLINT NOT NULL")
	pending := waitPipelineState(t, ctx, pipeline, PipelineAwaitingApproval).PendingDDL
	require.NotNil(t, pending)
	require.Equal(t, DDLUnsupported, pending.Kind)
	require.False(t, pending.RequiresApproval)
	require.NotEmpty(t, pending.Reason)
	require.ErrorContains(t, pipeline.ApproveDDL(ctx, pending.ID), "requires a new generation")
	expectedCheckpoint := latestDestructiveCheckpoint(t, ctx, fixture)
	require.Equal(t, pending.Sequence-1, expectedCheckpoint.Sequence)
	assertDestructiveRows(t, ctx, fixture, columns, before)
	closeDestructivePipeline(t, ctx, pipeline)

	pipeline = startDestructivePipeline(t, ctx, fixture.config, PipelineAwaitingApproval)
	require.Equal(t, pending, pipeline.Status().PendingDDL)
	require.Error(t, pipeline.ApproveDDL(ctx, pending.ID))
	require.Equal(t, expectedCheckpoint, latestDestructiveCheckpoint(t, ctx, fixture))
	closeDestructivePipeline(t, ctx, pipeline)
	fixture.executeSource(t, ctx, "INSERT INTO "+table+" VALUES (2, 200, 'rebuilt')")
	shadow := fixture
	shadow.config.Identity.Generation += "_rebuild"
	shadow.config.Directory = t.TempDir()
	shadow.config.TargetDatabase += "_rebuild"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		deleteTestWriter(t, cleanup, shadow.doris, shadow.config.TargetDatabase, shadow.config.Doris.ControlDatabase, shadow.config.Identity)
		if _, err := shadow.doris.ExecContext(cleanup, "DROP DATABASE IF EXISTS "+shadow.config.TargetDatabase); err != nil {
			t.Logf("cleanup shadow: %v", err)
		}
	})
	pipeline = startDestructivePipeline(t, ctx, shadow.config, PipelineRunning)
	assertDestructiveRows(t, ctx, shadow, columns, [][]string{before[0], {"2", "200", "rebuilt"}})
	assertDestructiveRows(t, ctx, fixture, columns, before)
	verification, err := VerifyShadow(ctx, ShadowVerificationConfig{
		Source:  ShadowTable{DB: fixture.source, Schema: fixture.config.Source.Database, Table: "ledger"},
		Legacy:  ShadowTable{DB: fixture.doris, Schema: fixture.config.TargetDatabase, Table: "ledger"},
		Shadow:  ShadowTable{DB: shadow.doris, Schema: shadow.config.TargetDatabase, Table: "ledger"},
		Columns: columns, KeyColumns: []string{"id"}, SampleLimit: 2,
	})
	require.NoError(t, err)
	require.Len(t, verification.Ranges, 1)
	require.Equal(t, verification.Ranges[0].Source, verification.Ranges[0].Shadow)
	require.EqualValues(t, 2, verification.Ranges[0].Source.Rows)
	require.NotEqual(t, verification.Ranges[0].Legacy, verification.Ranges[0].Shadow)
	require.Len(t, verification.Differences, 1)
	require.Equal(t, []string{"2"}, verification.Differences[0].Key)
	require.Nil(t, verification.Differences[0].Legacy)
	versions := shadow.schemaVersions(t, ctx)
	require.Len(t, versions, 1)
	require.Equal(t, "smallint", versions[0].catalog.Tables[0].Columns[1].DataType)
	require.Equal(t, expectedCheckpoint, latestDestructiveCheckpoint(t, ctx, fixture))
	closeDestructivePipeline(t, ctx, pipeline)
}

// startDestructivePipeline 启动破坏性 DDL 测试流水线并等待指定状态。
func startDestructivePipeline(t *testing.T, ctx context.Context, config PipelineConfig, state PipelineState) *Pipeline {
	t.Helper()
	pipeline, err := StartPipeline(ctx, config)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, pipeline.Close(ctx))
	})
	waitPipelineState(t, ctx, pipeline, state)
	return pipeline
}

// closeDestructivePipeline 在测试期限内关闭流水线，并确认其资源已释放。
func closeDestructivePipeline(t *testing.T, ctx context.Context, pipeline *Pipeline) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, pipeline.Close(ctx))
	require.Equal(t, PipelineStopped, pipeline.Status().State)
}

// latestDestructiveCheckpoint 读取 DDL 集成用例当前最新的目标检查点。
func latestDestructiveCheckpoint(t *testing.T, ctx context.Context, fixture ddlIntegrationFixture) Checkpoint {
	t.Helper()
	checkpoints := fixture.checkpoints(t, ctx)
	require.NotEmpty(t, checkpoints)
	return checkpoints[len(checkpoints)-1]
}

// assertDestructiveRows 核对 DDL 变更后的目标投影及行数据。
func assertDestructiveRows(t *testing.T, ctx context.Context, fixture ddlIntegrationFixture, columns []string, expected [][]string) {
	t.Helper()
	table := fixture.config.TargetDatabase + ".ledger"
	require.Equal(t, expected, readDestructiveRows(t, ctx, fixture.doris, table, columns))
	var name, definition sql.NullString
	require.NoError(t, fixture.doris.QueryRowContext(ctx, "SHOW CREATE TABLE "+table).Scan(&name, &definition))
	require.Contains(t, strings.ToUpper(strings.Join(strings.Fields(definition.String), "")), "UNIQUEKEY(`ID`)")
}

// readDestructiveRows 按主键读取指定列，并将列值转换为便于统一核对的字符串。
func readDestructiveRows(t *testing.T, ctx context.Context, db *sql.DB, table string, columns []string) [][]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()
	actualColumns, err := rows.Columns()
	require.NoError(t, err)
	require.Equal(t, columns, actualColumns)
	var actual [][]string
	for rows.Next() {
		row := make([]string, len(columns))
		destinations := make([]any, len(columns))
		for index := range row {
			destinations[index] = &row[index]
		}
		require.NoError(t, rows.Scan(destinations...))
		actual = append(actual, row)
	}
	require.NoError(t, rows.Err())
	return actual
}

// assertDestructiveCheckpoint 核对屏障检查点、后续游标和对应的结构版本。
func assertDestructiveCheckpoint(t *testing.T, ctx context.Context, fixture ddlIntegrationFixture, pending *DDLPlan, latest Checkpoint) {
	t.Helper()
	checkpoints := fixture.checkpoints(t, ctx)
	require.NotEmpty(t, checkpoints)
	var atBarrier *Checkpoint
	for index, checkpoint := range checkpoints {
		require.EqualValues(t, index, checkpoint.Sequence, "checkpoints must remain contiguous")
		if checkpoint.Sequence == pending.Sequence {
			atBarrier = &checkpoints[index]
		}
	}
	require.NotNil(t, atBarrier)
	require.Equal(t, pending.Cursor, atBarrier.Cursor)
	require.Equal(t, latest.StructureRevision, atBarrier.StructureRevision)
	require.NotEmpty(t, atBarrier.StructureRevision)
	require.NoError(t, validateAdvance(atBarrier.Cursor, latest.Cursor))
	for _, version := range fixture.schemaVersions(t, ctx) {
		if version.catalog.Sequence == pending.Sequence {
			require.Equal(t, atBarrier.StructureRevision, version.revision)
			require.Equal(t, pending.Cursor, version.catalog.Cursor)
			return
		}
	}
	t.Fatal("DDL checkpoint has no corresponding target schema version")
}

// TestDorisIntegrationRecoversLostStreamLoadResponse 验证真实 Stream Load 响应丢失后通过 Doris 审计恢复且按原批次确认。
func TestDorisIntegrationRecoversLostStreamLoadResponse(t *testing.T) {
	dsn := os.Getenv("CDC_TEST_DORIS_DSN")
	frontend := os.Getenv("CDC_TEST_DORIS_FE")
	backend := os.Getenv("CDC_TEST_DORIS_BE")
	if dsn == "" || frontend == "" || backend == "" || os.Getenv("CDC_TEST_DORIS_AUDIT") != "1" {
		skipCDCIntegration(t, "set CDC_TEST_DORIS_DSN, CDC_TEST_DORIS_FE, CDC_TEST_DORIS_BE and CDC_TEST_DORIS_AUDIT=1")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	suffix := fmt.Sprint(time.Now().UnixNano())
	database := "cdc_audit_dst_" + suffix
	control := "cdc_audit_ctl_" + suffix
	target, err := OpenDorisTarget(ctx, DorisConfig{
		SQLDSN: dsn, FrontendURL: frontend, StreamLoadURL: backend, ControlDatabase: control,
	})
	require.NoError(t, err)
	defer target.Close()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, schema := range []string{database, control} {
			if _, err := target.db.ExecContext(cleanup, "DROP DATABASE IF EXISTS "+quoteDoris(schema)); err != nil {
				t.Logf("cleanup %s: %v", schema, err)
			}
		}
	}()
	require.NoError(t, target.EnsureTable(ctx, DorisTable{
		Database: database, Name: "events", PrimaryKey: []string{"id"},
		Columns: []DorisColumn{{Name: "id", Type: "BIGINT"}},
	}))

	id := "42"
	payload, err := EncodeDorisLoad([]string{"id"}, []DorisRow{{Values: []*string{&id}}})
	require.NoError(t, err)
	batch := LoadBatch{
		TargetTable: database + ".events", RowCount: 1, Label: "cdc_audit_" + suffix,
		Payload: payload, Checksum: digest(payload),
	}
	var puts int
	target.client.Transport = dorisTestTransport(func(req *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(req)
		if req.Method != http.MethodPut || err != nil {
			return response, err
		}
		puts++
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		return nil, errors.Join(io.ErrUnexpectedEOF, readErr, closeErr)
	})
	_, err = target.SubmitLoad(ctx, batch)
	require.ErrorIs(t, err, ErrDorisLoadOutcomeUnknown)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		confirmation, err := target.ConfirmLoad(ctx, batch)
		if err == nil && confirmation.State == LoadSucceeded {
			require.Equal(t, dorisConfirmed(batch), confirmation)
			break
		}
		if err != nil && !errors.Is(err, ErrDorisLoadOutcomeUnknown) {
			t.Fatalf("reconcile real Stream Load: %v", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("Doris audit did not confirm label %s: %v", batch.Label, ctx.Err())
		}
	}
	var inserted int64
	require.NoError(t, target.db.QueryRowContext(ctx, "SELECT id FROM "+quoteDoris(database)+".`events`").Scan(&inserted))
	require.Equal(t, int64(42), inserted)
	require.Equal(t, 1, puts)
}

// TestSQLShadowRouteStoreIntegration 验证真实 InnoDB 路由存储的并发发布、恢复读回及旧修订保护。
func TestSQLShadowRouteStoreIntegration(t *testing.T) {
	if os.Getenv("CDC_TEST_PERSISTENT_ROUTES") != "1" {
		skipCDCIntegration(t, "set CDC_TEST_PERSISTENT_ROUTES=1 with isolated MySQL and MariaDB test DSNs")
	}
	for _, metadata := range []struct{ flavor, dsn string }{
		{"mysql", os.Getenv("CDC_TEST_MYSQL_DSN")},
		{"mariadb", os.Getenv("CDC_TEST_MARIADB_DSN")},
	} {
		t.Run(metadata.flavor, func(t *testing.T) {
			require.NotEmpty(t, metadata.dsn)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			connection, err := mysql.ParseDSN(metadata.dsn)
			require.NoError(t, err)
			connection.DBName = ""
			db, err := sql.Open("mysql", connection.FormatDSN())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			database := "cdc_routes_" + strings.ToLower(rand.Text())
			_, err = db.ExecContext(ctx, "CREATE DATABASE "+quoteDoris(database))
			require.NoError(t, err)
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
				defer stop()
				_, err := db.ExecContext(cleanup, "DROP DATABASE IF EXISTS "+quoteDoris(database))
				require.NoError(t, err)
			})
			store, err := NewSQLShadowRouteStore(db, database)
			require.NoError(t, err)
			selected := ShadowRoute{
				SourceID: "selected", SourceIdentity: "selected-member", SourceDatabase: "sales",
				LegacyDatabase: "sales_legacy", ShadowDatabase: "sales_shadow",
				Tables: map[string]ShadowTableProjection{"orders": {Columns: []string{"id"}, KeyColumns: []string{"id"}}},
			}
			sibling := selected
			sibling.SourceID, sibling.SourceIdentity, sibling.SourceDatabase = "sibling", "sibling-member", "stock"
			sibling.LegacyDatabase, sibling.ShadowDatabase = "stock_legacy", "stock_shadow"
			configs := []ShadowRoute{selected, sibling}
			require.NoError(t, store.Initialize(ctx, configs))
			initial, err := store.loadRecord(ctx, selected)
			require.NoError(t, err)
			other, err := store.load(ctx, sibling)
			require.NoError(t, err)

			type attempt struct {
				route ReadRoute
				err   error
			}
			start := make(chan struct{})
			results := make(chan attempt, 2)
			for _, position := range []uint32{42, 84} {
				candidate := ReadRoute{
					SourceID: selected.SourceID, Database: selected.ShadowDatabase,
					UsingShadow: true, Revision: 2, SwitchedAt: time.Now().UTC(),
					Watermark: Cursor{SourceIdentity: selected.SourceIdentity, File: "binlog.000001", Position: position, Flavor: metadata.flavor},
				}
				go func() {
					<-start
					err := store.compareAndSwap(ctx, selected, initial, shadowRoutePublication{ReadRoute: candidate})
					results <- attempt{route: candidate, err: err}
				}()
			}
			close(start)
			var winner ReadRoute
			for range 2 {
				result := <-results
				if result.err != nil {
					require.ErrorIs(t, result.err, ErrShadowRouteRevisionChanged)
					continue
				}
				require.Zero(t, winner.Revision, "a revision can be committed once")
				winner = result.route
			}
			require.EqualValues(t, 2, winner.Revision)
			require.NoError(t, store.Initialize(ctx, []ShadowRoute{sibling, selected}))
			published, err := store.load(ctx, selected)
			require.NoError(t, err)
			require.Equal(t, winner, published)
			unchanged, err := store.load(ctx, sibling)
			require.NoError(t, err)
			require.Equal(t, other, unchanged)

			beforePrepare, err := store.loadRecord(ctx, selected)
			require.NoError(t, err)
			router, err := OpenPersistentShadowRouter(ctx, store, configs)
			require.NoError(t, err)
			require.NoError(t, router.PrepareSourceMigration(ctx, selected.SourceID, winner.Revision))
			staleRollback := winner
			staleRollback.Database, staleRollback.UsingShadow, staleRollback.Revision = selected.LegacyDatabase, false, winner.Revision+1
			staleRollback.Watermark.Position++
			require.ErrorIs(t, store.compareAndSwap(ctx, selected, beforePrepare, shadowRoutePublication{ReadRoute: staleRollback}),
				ErrShadowRouteRevisionChanged, "preparing must also fence a publication already holding the previous revision and token")
			stillPublished, err := router.RouteContext(ctx, selected.SourceID)
			require.NoError(t, err)
			require.Equal(t, winner, stillPublished)
		})
	}
}

// TestTransactionRecoveryIntegration 验证真实源断连以及单笔事务超限后的同位点捕获。
func TestTransactionRecoveryIntegration(t *testing.T) {
	doris := DorisConfig{
		SQLDSN: os.Getenv("CDC_TEST_DORIS_DSN"), FrontendURL: os.Getenv("CDC_TEST_DORIS_FE"), StreamLoadURL: os.Getenv("CDC_TEST_DORIS_BE"),
	}
	if doris.SQLDSN == "" || doris.FrontendURL == "" || doris.StreamLoadURL == "" {
		skipCDCIntegration(t, "set the isolated Doris test endpoints")
	}
	for _, tc := range []struct{ flavor, dsn string }{
		{"mysql", os.Getenv("CDC_TEST_MYSQL_DSN")}, {"mariadb", os.Getenv("CDC_TEST_MARIADB_DSN")},
	} {
		t.Run(tc.flavor, func(t *testing.T) {
			if tc.dsn == "" {
				skipCDCIntegration(t, "source test DSN is unset")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			fixture := newDDLIntegrationFixture(t, ctx, tc.flavor, tc.dsn, doris)
			fixture.executeSource(t, ctx, "ALTER TABLE "+fixture.sourceTable()+" ADD COLUMN payload VARCHAR(1500) NOT NULL DEFAULT ''")
			config := fixture.config
			proxyAddress, configSwitch := newTestSourceProxy(t, config.Source.Address)
			config.Source.Address = proxyAddress
			config.MaxPendingBytes, config.MaxTransactionBytes = 64<<20, 768
			pipeline, err := StartPipeline(ctx, config)
			require.NoError(t, err)
			require.NoError(t, pipeline.waitReady(ctx))
			fixture.waitRows(t, ctx, pipeline, false, ddlIntegrationRow{id: 1, amount: 100})
			configSwitch(true)
			require.True(t, waitDDLIntegration(ctx, func() bool { return pipeline.Status().CaptureError != "" }), "capture did not observe the disconnect")
			fixture.executeSource(t, ctx, "INSERT INTO "+fixture.sourceTable()+" VALUES (2, 200, 'during disconnect')")
			configSwitch(false)
			fixture.waitRows(t, ctx, pipeline, false, ddlIntegrationRow{id: 1, amount: 100}, ddlIntegrationRow{id: 2, amount: 200})
			require.Empty(t, pipeline.Status().CaptureError)
			require.True(t, waitDDLIntegration(ctx, func() bool { return pipeline.Status().Progress.Delivered.Sequence > 0 }))
			confirmed := pipeline.Status().Progress.Durable

			const rows = 8192
			const payloadLength = 1200
			transaction, err := fixture.source.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer transaction.Rollback()
			for start := 0; start < rows; start += 256 {
				var statement strings.Builder
				statement.WriteString("INSERT INTO " + fixture.sourceTable() + " VALUES ")
				for offset := range 256 {
					if offset != 0 {
						statement.WriteByte(',')
					}
					fmt.Fprintf(&statement, "(%d,7,REPEAT('x',%d))", 100+start+offset, payloadLength)
				}
				_, err := transaction.ExecContext(ctx, statement.String())
				require.NoError(t, err)
			}
			require.NoError(t, transaction.Commit())
			require.ErrorIs(t, pipeline.Wait(), ErrTransactionTooLarge)
			blocked := pipeline.Status().Progress
			require.Equal(t, confirmed, blocked.Durable)
			require.Equal(t, confirmed, blocked.Delivered)
			var count int
			require.NoError(t, fixture.doris.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+config.TargetDatabase+".ledger WHERE id>=100").Scan(&count))
			require.Zero(t, count)

			config.MaxTransactionBytes = 32 << 20
			restarted, err := StartPipeline(ctx, config)
			require.NoError(t, err)
			defer restarted.Close(ctx)
			require.NoError(t, restarted.waitReady(ctx))
			require.True(t, waitDDLIntegration(ctx, func() bool {
				status := restarted.Status()
				return status.State == PipelineFailed || status.Progress.Delivered.Sequence == confirmed.Sequence+1
			}), "raised budget did not finish the original transaction")
			status := restarted.Status()
			require.NoError(t, status.Err)
			require.Equal(t, confirmed.Sequence+1, status.Progress.Delivered.Sequence)
			require.NoError(t, validateAdvance(confirmed.Cursor, status.Progress.Delivered.Cursor))
			var sum, payloadBytes int
			require.NoError(t, fixture.doris.QueryRowContext(ctx, "SELECT COUNT(*),SUM(amount),SUM(LENGTH(payload)) FROM "+config.TargetDatabase+".ledger WHERE id>=100").Scan(&count, &sum, &payloadBytes))
			require.Equal(t, rows, count)
			require.Equal(t, 7*rows, sum)
			require.Equal(t, payloadLength*rows, payloadBytes)
			t.Logf("%s: isolation window recovered, one transaction of %d rows/%d payload bytes blocked then replayed from sequence %d to %d",
				tc.flavor, rows, payloadBytes, confirmed.Sequence, status.Progress.Delivered.Sequence)
			if tc.flavor == "mysql" && (os.Getenv("CDC_TEST_COMPRESSED_BINLOG") == "1" || os.Getenv("CDC_TEST_REQUIRE_INTEGRATION") == "1") {
				connection, err := fixture.source.Conn(ctx)
				require.NoError(t, err)
				defer connection.Close()
				_, err = connection.ExecContext(ctx, "SET SESSION binlog_transaction_compression = ON")
				require.NoError(t, err)
				_, err = connection.ExecContext(ctx, "INSERT INTO "+fixture.sourceTable()+" VALUES (4, 400, REPEAT('y',1000))")
				require.NoError(t, err)
				require.True(t, waitDDLIntegration(ctx, func() bool {
					state := restarted.Status()
					return state.State == PipelineFailed || state.Progress.Delivered.Sequence == confirmed.Sequence+2
				}), "compressed binlog transaction did not finish")
				require.NoError(t, restarted.Status().Err)
				require.NoError(t, fixture.doris.QueryRowContext(ctx, "SELECT LENGTH(payload) FROM "+config.TargetDatabase+".ledger WHERE id=4").Scan(&payloadBytes))
				require.Equal(t, 1000, payloadBytes)
				t.Log("small MySQL compressed transaction delivered")
			}
		})
	}
}

// newTestSourceProxy 只阻断当前测试流水线使用的连接，源写入仍走直连。
func newTestSourceProxy(t *testing.T, upstreamAddress string) (string, func(bool)) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var blocked bool
	active := make(map[net.Conn]struct{})
	setBlocked := func(value bool) {
		mu.Lock()
		defer mu.Unlock()
		blocked = value
		if blocked {
			for connection := range active {
				_ = connection.Close()
			}
		}
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if blocked {
				mu.Unlock()
				_ = connection.Close()
				continue
			}
			active[connection] = struct{}{}
			mu.Unlock()
			workers.Go(func() {
				defer func() {
					_ = connection.Close()
					mu.Lock()
					delete(active, connection)
					mu.Unlock()
				}()
				upstream, err := net.DialTimeout("tcp", upstreamAddress, time.Second)
				if err != nil {
					return
				}
				defer upstream.Close()
				done := make(chan struct{})
				go func() {
					_, _ = io.Copy(upstream, connection)
					_ = upstream.Close()
					close(done)
				}()
				_, _ = io.Copy(connection, upstream)
				_ = connection.Close()
				<-done
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		setBlocked(true)
		workers.Wait()
	})
	return listener.Addr().String(), setBlocked
}
