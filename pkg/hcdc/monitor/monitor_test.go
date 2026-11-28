package monitor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	mysqlserver "github.com/go-mysql-org/go-mysql/server"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
)

// TestMonitorSDKLifecycle 验证状态快照、按源控制、目录更新和服务关闭。
func TestMonitorSDKLifecycle(t *testing.T) {
	ctx := t.Context()
	const primary = "canal-70001-orders"
	const second = "canal-70001-payments"
	const replacement = "canal-70001-replacement"
	config := monitorTestConfig(t, "^(orders|payments)$")
	m := New(ctx)
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	require.Empty(t, m.Snapshot().Sources)
	require.ErrorIs(t, m.Start(ctx, primary), ErrSourceNotFound)
	require.NoError(t, m.Reload(ctx, config))
	config.Canals[0].IncludeDBs[0] = "^replacement$"

	initial := m.Snapshot()
	require.NotZero(t, initial.UpdatedAt)
	require.Len(t, initial.Sources, 2)
	require.Equal(t, primary, initial.Sources[0].ID)
	require.Equal(t, second, initial.Sources[1].ID)
	require.Equal(t, "report_orders", initial.Sources[0].TargetDatabase)
	require.Equal(t, "report_payments", initial.Sources[1].TargetDatabase)
	require.Equal(t, StateNotStarted, initial.Sources[0].State)
	require.Equal(t, []Action{ActionStart}, initial.Sources[0].Actions)
	const largeCounter = 1<<54 + 17
	initial.Sources[0] = m.byID[primary].snapshot(hcdc.PipelineStatus{State: StateNotStarted,
		Err: fmt.Errorf("connection diagnostics: source-secret and doris-secret")}, true, false)
	initial.Sources[0].Delivered.Sequence = largeCounter
	initial.Sources[0].SnapshotZeroDateRows = largeCounter
	encoded, err := json.Marshal(initial)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"sequence":"18014398509482001"`)
	require.Contains(t, string(encoded), `"snapshotZeroDateRows":"18014398509482001"`)
	require.Contains(t, string(encoded), `"error":"connection diagnostics: [redacted] and [redacted]"`)
	require.NotContains(t, string(encoded), "source-secret")
	require.NotContains(t, string(encoded), "doris-secret")

	initial.Sources[0].Actions[0] = ActionStop
	status, err := m.Status(primary)
	require.NoError(t, err)
	require.Zero(t, status.Delivered.Sequence)
	require.Equal(t, []Action{ActionStart}, status.Actions)
	_, err = m.Status("unknown")
	require.ErrorIs(t, err, ErrSourceNotFound)
	require.ErrorIs(t, m.Start(ctx, "unknown"), ErrSourceNotFound)
	require.ErrorIs(t, m.Pause(ctx, primary, "维护"), ErrInvalidState)
	require.ErrorIs(t, m.Resume(ctx, primary), ErrInvalidState)
	require.ErrorIs(t, m.Stop(ctx, primary), ErrInvalidState)
	require.ErrorIs(t, m.ApproveDDL(ctx, primary, "ddl"), ErrInvalidState)

	blockMonitorTestBootstrap(t, config.Canals[0], primary)
	require.NoError(t, m.Start(ctx, primary))
	waitForMonitorFailure(t, m, primary)
	status, err = m.Status(second)
	require.NoError(t, err)
	require.Equal(t, StateNotStarted, status.State)

	require.NoError(t, m.Reload(ctx, config))
	require.Error(t, m.Reload(ctx, nil))
	require.Error(t, m.Reload(ctx, &hcdc.Config{}))
	_, err = m.Status(primary)
	require.ErrorIs(t, err, ErrSourceNotFound)
	status, err = m.Status(replacement)
	require.NoError(t, err)
	require.Equal(t, StateNotStarted, status.State)

	config.Canals = []hcdc.CanalConfig{}
	require.NoError(t, m.Reload(ctx, config))
	require.Empty(t, m.Snapshot().Sources)

	require.NoError(t, m.Close(ctx))
	require.ErrorIs(t, m.Start(ctx, replacement), ErrClosed)
	require.ErrorIs(t, m.Reload(ctx, config), ErrClosed)
}

// TestMonitorWatchStatus 验证空闲源只通知一次，订阅退出时保持原流水线句柄。
func TestMonitorWatchStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	const sourceID = "canal-70001-orders"
	config := monitorTestConfig(t, "^orders$")
	m := New(t.Context())
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	require.NoError(t, m.Reload(ctx, config))

	var idle []SourceStatus
	require.NoError(t, m.WatchStatus(ctx, sourceID, func(_ context.Context, state SourceStatus) {
		idle = append(idle, state)
		_, err := m.Status(state.ID)
		require.NoError(t, err)
	}))
	require.Len(t, idle, 1)
	require.Equal(t, StateNotStarted, idle[0].State)
	require.ErrorIs(t, m.WatchStatus(ctx, "unknown", func(context.Context, SourceStatus) {}), ErrSourceNotFound)
	canceled, stop := context.WithCancel(ctx)
	stop()
	require.ErrorIs(t, m.WatchStatus(canceled, sourceID, func(context.Context, SourceStatus) {
		t.Fatal("取消的监听收到了通知")
	}), context.Canceled)

	blockMonitorTestBootstrap(t, config.Canals[0], sourceID)
	require.NoError(t, m.Start(ctx, sourceID))
	states := make(chan SourceStatus, 8)
	finished := make(chan error, 1)
	release := make(chan struct{})
	finishCallback := sync.OnceFunc(func() { close(release) })
	defer finishCallback()
	go func() {
		finished <- m.WatchStatus(ctx, sourceID, func(callbackCtx context.Context, status SourceStatus) {
			states <- status
			if status.State == hcdc.PipelineFailed {
				select {
				case <-release:
				case <-callbackCtx.Done():
				}
			}
		})
	}()
	for {
		select {
		case status := <-states:
			require.Equal(t, "g01", status.Generation)
			if status.State != hcdc.PipelineFailed {
				continue
			}
			require.NotEmpty(t, status.Error)
		case <-ctx.Done():
			t.Fatal("监听未报告流水线失败")
		}
		break
	}
	waitForMonitorFailure(t, m, sourceID)

	config.Canals[0].Generation = "g02"
	config.Doris.SchemaPrefix = "report_next_"
	blockMonitorTestBootstrap(t, config.Canals[0], sourceID)
	require.NoError(t, m.Reload(ctx, config))
	require.NoError(t, m.Start(ctx, sourceID))
	finishCallback()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("监听在报告原流水线终态后没有退出")
	}
	require.Empty(t, states, "监听不能跟随被替换的流水线")
	require.Equal(t, "g02", waitForMonitorFailure(t, m, sourceID).Generation)
}

// TestMonitorDiscoversFirstSchema 验证空目录可开始发现，自动启动失败后保留原失败句柄。
func TestMonitorDiscoversFirstSchema(t *testing.T) {
	const id = "canal-70001-orders"
	var ready atomic.Bool
	var scans atomic.Int64
	config := monitorTestConfigWithCatalog(t, "^orders$", func() [][]any {
		scans.Add(1)
		if ready.Load() {
			return [][]any{{"orders", "events"}}
		}
		return nil
	})
	config.AutoDiscoverInterval = 10 * time.Millisecond
	blockMonitorTestBootstrap(t, config.Canals[0], id)
	m := New(t.Context())
	t.Cleanup(func() { require.NoError(t, m.Close(context.Background())) })
	require.NoError(t, m.Reload(t.Context(), config))
	require.Empty(t, m.Snapshot().Sources)
	ready.Store(true)
	status := waitForMonitorFailure(t, m, id)
	require.Equal(t, "report_orders", status.TargetDatabase)
	original, ok := m.manager.Pipeline(id)
	require.True(t, ok)
	previousScans := scans.Load()
	require.Eventually(t, func() bool { return scans.Load() >= previousScans+3 }, time.Second, time.Millisecond)
	after, ok := m.manager.Pipeline(id)
	require.True(t, ok)
	require.Same(t, original, after)
}

type monitorMetadata struct {
	mysqlserver.EmptyHandler
	catalog func() [][]any
}

// HandleQuery 为监控测试返回固定的源表目录。
func (m monitorMetadata) HandleQuery(query string) (*gomysql.Result, error) {
	if strings.HasPrefix(query, "SET NAMES ") {
		return nil, nil
	}
	if !strings.HasPrefix(query, "SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.TABLES") {
		return nil, fmt.Errorf("unexpected metadata query %q", query)
	}
	result, err := gomysql.BuildSimpleTextResultset([]string{"TABLE_SCHEMA", "TABLE_NAME"}, m.catalog())
	if err != nil {
		return nil, err
	}
	return gomysql.NewResult(result), nil
}

// monitorTestConfig 经由内存连接提供元数据，无需监听端口或连接真实数据库。
func monitorTestConfig(t *testing.T, includeDBs string) *hcdc.Config {
	t.Helper()
	return monitorTestConfigWithCatalog(t, includeDBs, func() [][]any {
		return [][]any{{"orders", "events"}, {"payments", "events"}, {"replacement", "events"}}
	})
}

// monitorTestConfigWithCatalog 让测试动态更新内存连接返回的源表目录。
func monitorTestConfigWithCatalog(t *testing.T, includeDBs string, catalog func() [][]any) *hcdc.Config {
	t.Helper()
	const sourceAddress = "monitor-fixture:3306"
	server := mysqlserver.NewServer("8.0.12", gomysql.DEFAULT_COLLATION_ID, gomysql.AUTH_NATIVE_PASSWORD, nil, nil)
	auth := mysqlserver.NewInMemoryAuthenticationHandler(gomysql.AUTH_NATIVE_PASSWORD)
	require.NoError(t, auth.AddUser("cdc", "source-secret"))
	var dialer net.Dialer
	dialTCP := func(ctx context.Context, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp", address)
	}
	mysql.RegisterDialContext("tcp", func(ctx context.Context, address string) (net.Conn, error) {
		if address != sourceAddress {
			return dialTCP(ctx, address)
		}
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			conn, err := server.NewCustomizedConn(peer, auth, monitorMetadata{catalog: catalog})
			if err != nil {
				return
			}
			for !conn.Closed() {
				if err := conn.HandleCommand(); err != nil {
					return
				}
			}
		}()
		return client, nil
	})
	t.Cleanup(func() { mysql.RegisterDialContext("tcp", dialTCP) })
	return &hcdc.Config{
		Doris: &hcdc.CanalDorisConfig{
			Host: "127.0.0.1:9030", Username: "cdc", Password: "doris-secret", SchemaPrefix: "report_",
			FrontendURL: "http://localhost:8030", LoadURL: "http://localhost:8040",
		},
		Canals: []hcdc.CanalConfig{{
			InstanceId: 70001, Host: sourceAddress, Username: "cdc", Password: "source-secret",
			IncludeDBs: []string{includeDBs}, LogDir: t.TempDir(), Generation: "g01",
		}},
	}
}

// blockMonitorTestBootstrap 使流水线在打开源连接前异步失败。
func blockMonitorTestBootstrap(t *testing.T, config hcdc.CanalConfig, sourceID string) {
	t.Helper()
	directory := filepath.Join(config.LogDir, sourceID, config.Generation)
	require.NoError(t, os.MkdirAll(directory, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "bootstrap"), nil, 0o600))
}

// waitForMonitorFailure 等待终态并确认流水线已经允许重新启动。
func waitForMonitorFailure(t *testing.T, m *Monitor, sourceID string) SourceStatus {
	t.Helper()
	var status SourceStatus
	require.Eventually(t, func() bool {
		var err error
		status, err = m.Status(sourceID)
		return err == nil && status.State == hcdc.PipelineFailed && len(status.Actions) == 1 && status.Actions[0] == ActionStart
	}, 2*time.Second, time.Millisecond)
	require.NotEmpty(t, status.Error)
	return status
}

const (
	initialSnapshotChild = "HCDC_INITIAL_SCHEMA_SNAPSHOT_CHILD"
	initialBaselineSQL   = "SELECT checkpoint_json FROM cdc_control.checkpoints WHERE stream_key=? AND sequence_no=0"
)

type snapshotCrashWindow string

const (
	beforeSnapshotSeal      snapshotCrashWindow = "before_seal"
	duringSnapshotLoad      snapshotCrashWindow = "during_load"
	afterSnapshotCheckpoint snapshotCrashWindow = "after_checkpoint"
)

// TestInitialSchemaSnapshotCrashIntegration 验证首次全量在三个关键窗口被强杀后能用原卷恢复。
func TestInitialSchemaSnapshotCrashIntegration(t *testing.T) {
	if os.Getenv(initialSnapshotChild) == "1" {
		runInitialSnapshotChild(t)
		return
	}
	for _, window := range []snapshotCrashWindow{beforeSnapshotSeal, duringSnapshotLoad, afterSnapshotCheckpoint} {
		t.Run(string(window), func(t *testing.T) { testInitialSchemaSnapshotCrash(t, window) })
	}
}

// testInitialSchemaSnapshotCrash 在同一子进程里验证两个源和各自独立运行的对照库。
func testInitialSchemaSnapshotCrash(t *testing.T, window snapshotCrashWindow) {
	dorisDSN, fe, be := os.Getenv("CDC_TEST_DORIS_DSN"), os.Getenv("CDC_TEST_DORIS_FE"), os.Getenv("CDC_TEST_DORIS_BE")
	mysqlDSN, mariaDSN := os.Getenv("CDC_TEST_MYSQL_DSN"), os.Getenv("CDC_TEST_MARIADB_DSN")
	if dorisDSN == "" || fe == "" || be == "" || mysqlDSN == "" || mariaDSN == "" {
		if os.Getenv("CDC_TEST_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("需要隔离的 MySQL、MariaDB 和 Doris 验收环境")
		}
		t.Skip("设置 CDC_TEST_MYSQL_DSN、CDC_TEST_MARIADB_DSN、CDC_TEST_DORIS_DSN、CDC_TEST_DORIS_FE 和 CDC_TEST_DORIS_BE 后运行")
	}

	ctx := t.Context()
	workspace := t.TempDir()
	testName := fmt.Sprintf("hcdc_init_%d", time.Now().UnixNano())
	oldSchema, newSchema := testName+"_old", testName+"_new"
	doris, dorisConnection := openInitialSchemaDatabase(t, dorisDSN)
	config := hcdc.Config{
		Doris: &hcdc.CanalDorisConfig{
			Host: dorisConnection.Addr, Username: dorisConnection.User, Password: dorisConnection.Passwd,
			FrontendURL: fe, SchemaPrefix: "it_{InstanceId}_",
		},
		AutoDiscoverInterval: 2 * time.Second,
	}
	type fixture struct {
		flavor               string
		db                   *sql.DB
		oldTarget, newTarget string
		sourceID, key        string
		sealQuery            string
		checkpointInsert     string
		checkpointWrites     atomic.Int32
		checkpointJSON       string
	}
	var fixtures []*fixture
	gate := initialSnapshotGate{
		holdLoad:       window == duringSnapshotLoad,
		firstWideLabel: make(map[string]string), blocked: make(map[string]string),
		requests: make(map[string]int), forwarded: make(map[string]int), checksums: make(map[string]string),
	}
	backend, err := url.Parse(be)
	require.NoError(t, err)
	reverseProxy := httputil.NewSingleHostReverseProxy(backend)
	reverseProxy.ModifyResponse = gate.holdResponse
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gate.serve(w, request, reverseProxy)
	}))
	defer proxy.Close()
	config.Doris.LoadURL = proxy.URL
	for i, endpoint := range []struct{ flavor, dsn string }{{"mysql", mysqlDSN}, {"mariadb", mariaDSN}} {
		source, connection := openInitialSchemaDatabase(t, endpoint.dsn)
		instanceID := int64(670001 + i)
		serverID := uint32(instanceID)
		config.Canals = append(config.Canals, hcdc.CanalConfig{
			InstanceId: instanceID, ServerID: &serverID, Flavor: endpoint.flavor,
			Host: connection.Addr, Username: connection.User, Password: connection.Passwd, LogDir: workspace,
			IncludeDBs: []string{"^(" + oldSchema + "|" + newSchema + ")$"},
		})
		oldTarget := fmt.Sprintf("it_%d_%s", instanceID, oldSchema)
		newTarget := fmt.Sprintf("it_%d_%s", instanceID, newSchema)
		sourceID := fmt.Sprintf("canal-%d-%s", instanceID, newSchema)
		streamKey := initialSnapshotStreamKey(sourceID)
		fixtures = append(fixtures, &fixture{
			flavor: endpoint.flavor, db: source, oldTarget: oldTarget, newTarget: newTarget, sourceID: sourceID, key: streamKey,
			sealQuery:        "SELECT 1 FROM `" + newTarget + "`.`zz_empty` LIMIT 1",
			checkpointInsert: "INSERT INTO `cdc_control`.`checkpoints` VALUES ('" + streamKey + "', 0, ",
		})
		for _, schema := range []string{oldSchema, newSchema} {
			execInitialSchemaSQL(t, source, "CREATE DATABASE "+schema)
			target := fmt.Sprintf("it_%d_%s", instanceID, schema)
			id := fmt.Sprintf("canal-%d-%s", instanceID, schema)
			t.Cleanup(func() {
				cleanupInitialSnapshot(t, source, doris, schema, target, initialSnapshotStreamKey(id))
			})
		}
		execInitialSchemaSQL(t, source, "CREATE TABLE "+oldSchema+".ledger (id INT PRIMARY KEY, amount INT NOT NULL) ENGINE=InnoDB")
		execInitialSchemaSQL(t, source, "INSERT INTO "+oldSchema+".ledger VALUES (1, 10)")
		execInitialSchemaSQL(t, source, "CREATE TABLE "+newSchema+".alpha (id INT PRIMARY KEY, amount INT NOT NULL) ENGINE=InnoDB")
		execInitialSchemaSQL(t, source, "INSERT INTO "+newSchema+".alpha VALUES (1, 10), (2, 20), (3, 30), (4, 40)")
		execInitialSchemaSQL(t, source, "CREATE TABLE "+newSchema+".wide (id INT PRIMARY KEY, amount INT NOT NULL, payload MEDIUMTEXT NOT NULL) ENGINE=InnoDB")
		execInitialSchemaSQL(t, source, "CREATE TABLE "+newSchema+".zz_empty (id INT PRIMARY KEY, amount INT NOT NULL) ENGINE=InnoDB")
		for first := 1; first <= 240; first += 40 {
			rows := make([]string, 0, 40)
			for id := first; id < first+40; id++ {
				rows = append(rows, fmt.Sprintf("(%d, %d, REPEAT('x', 98304))", id, id*10))
			}
			execInitialSchemaSQL(t, source, "INSERT INTO "+newSchema+".wide VALUES "+strings.Join(rows, ","))
		}
	}
	gate.beforeDoris = fixtures[1].newTarget
	sqlGate := newSnapshotSQLGate(t, dorisConnection.Addr, func(query string) snapshotSQLGateAction {
		for _, f := range fixtures {
			if strings.HasPrefix(query, f.checkpointInsert) {
				f.checkpointWrites.Add(1)
				if window == afterSnapshotCheckpoint {
					return snapshotSQLHoldResponse
				}
			}
			if window == beforeSnapshotSeal && query == f.sealQuery {
				return snapshotSQLHoldBefore
			}
		}
		return snapshotSQLPass
	})
	config.Doris.Host = sqlGate.address()

	startChild := func(name string) (*json.Encoder, func()) {
		t.Helper()
		log, err := os.Create(filepath.Join(workspace, name+".log"))
		require.NoError(t, err)
		defer log.Close()
		cmd := exec.Command(os.Args[0], "-test.run=^TestInitialSchemaSnapshotCrashIntegration$", "-test.timeout=2m", "-test.v")
		cmd.Env = append(os.Environ(), initialSnapshotChild+"=1")
		cmd.Stdout, cmd.Stderr = log, log
		stdin, err := cmd.StdinPipe()
		require.NoError(t, err)
		require.NoError(t, cmd.Start())
		finished := make(chan error, 1)
		go func() { finished <- cmd.Wait() }()
		killed := false
		kill := func() {
			t.Helper()
			if killed {
				return
			}
			killed = true
			err := cmd.Process.Kill()
			select {
			case waitErr := <-finished:
				if err != nil || waitErr == nil || !strings.Contains(waitErr.Error(), "signal: killed") {
					t.Errorf("CDC 子进程未按预期被 SIGKILL（kill=%v，wait=%v）", err, waitErr)
				}
				if t.Failed() {
					content, readErr := os.ReadFile(log.Name())
					require.NoError(t, readErr)
					diagnostics := string(content)
					for _, secret := range []string{config.Doris.Password, config.Canals[0].Password, config.Canals[1].Password} {
						if secret != "" {
							diagnostics = strings.ReplaceAll(diagnostics, secret, "[redacted]")
						}
					}
					lines := strings.Split(diagnostics, "\n")
					t.Logf("%s CDC 子进程末尾日志：\n%s", name, strings.Join(lines[max(0, len(lines)-20):], "\n"))
				}
			case <-time.After(5 * time.Second):
				t.Error("CDC 子进程收到 SIGKILL 后未退出")
			}
		}
		t.Cleanup(kill)
		control := json.NewEncoder(stdin)
		require.NoError(t, control.Encode(&config))
		return control, kill
	}

	control, kill := startChild("first")
	defer kill()
	for _, f := range fixtures {
		waitInitialSchemaCount(t, doris, f.oldTarget+".ledger", 1)
	}
	require.NoError(t, control.Encode(true))
	blockedQueries := make(map[string]bool)
	if window != duringSnapshotLoad {
		for range fixtures {
			select {
			case query := <-sqlGate.hit:
				blockedQueries[query] = true
			case <-time.After(35 * time.Second):
				t.Fatal("两个源未到达 SQL 故障点")
			}
		}
	}
	for i, f := range fixtures {
		volume := filepath.Join(workspace, f.sourceID, "1")
		if window == duringSnapshotLoad {
			waitInitialSnapshotLoadFailure(t, doris, &gate, f.newTarget, i == 1)
		}
		records, err := filepath.Glob(filepath.Join(volume, "snapshot-batches", "*.record"))
		require.NoError(t, err)
		require.Len(t, records, 5, "到达故障点时已保存全部快照批次")
		sealed := filepath.Join(volume, "snapshot-batches", "identity")
		prepared := filepath.Join(volume, "bootstrap", "00000000000000000001.record")
		require.NoFileExists(t, filepath.Join(volume, "bootstrap", "00000000000000000002.record"))
		if window == beforeSnapshotSeal {
			require.True(t, blockedQueries[f.sealQuery], "空表校验必须在封存清单发布前被阻断")
			require.NoFileExists(t, sealed)
			require.Equal(t, 0, initialSchemaCount(ctx, doris, f.newTarget+".alpha"))
			require.Equal(t, 0, initialSchemaCount(ctx, doris, f.newTarget+".wide"))
			var loadIntents int
			require.NoError(t, doris.QueryRowContext(ctx, "SELECT COUNT(*) FROM cdc_control.cdc_load_history WHERE stream_key=?", f.key).Scan(&loadIntents))
			require.Zero(t, loadIntents)
		} else {
			require.FileExists(t, sealed)
			require.Equal(t, 4, initialSchemaCount(ctx, doris, f.newTarget+".alpha"))
		}
		require.Equal(t, 0, initialSchemaCount(ctx, doris, f.newTarget+".zz_empty"))
		if window == afterSnapshotCheckpoint {
			require.FileExists(t, prepared)
			require.FileExists(t, filepath.Join(volume, "delivery", "identity"))
			require.Equal(t, 240, initialSchemaCount(ctx, doris, f.newTarget+".wide"))
			require.EqualValues(t, 1, f.checkpointWrites.Load())
			require.NoError(t, doris.QueryRowContext(ctx, initialBaselineSQL, f.key).Scan(&f.checkpointJSON))
			require.NotEmpty(t, f.checkpointJSON)
			verifyInitialSchemaReceipts(t, doris, &gate, f.key, f.newTarget, false)
		} else {
			require.NoFileExists(t, prepared)
			var published int
			require.NoError(t, doris.QueryRowContext(ctx, "SELECT COUNT(*) FROM cdc_control.checkpoints WHERE stream_key=?", f.key).Scan(&published))
			require.Zero(t, published, "封存或导入期间不得提前发布基线")
		}
		execInitialSchemaSQL(t, f.db, "INSERT INTO "+oldSchema+".ledger VALUES (2, 20)")
		execInitialSchemaSQL(t, f.db, "UPDATE "+newSchema+".alpha SET amount=111 WHERE id=1")
		execInitialSchemaSQL(t, f.db, "UPDATE "+newSchema+".wide SET amount=101, payload='updated' WHERE id=1")
		execInitialSchemaSQL(t, f.db, "DELETE FROM "+newSchema+".wide WHERE id IN (3, 240)")
		waitInitialSchemaCount(t, doris, f.oldTarget+".ledger", 2)
		t.Logf("%s 强杀前 alpha=%d/4、wide=%d/240、zz_empty=0，其他库事务已到达 Doris", f.flavor,
			initialSchemaCount(ctx, doris, f.newTarget+".alpha"), initialSchemaCount(ctx, doris, f.newTarget+".wide"))
	}
	kill()
	sqlGate.disarm()
	for _, f := range fixtures {
		execInitialSchemaSQL(t, f.db, "INSERT INTO "+newSchema+".wide VALUES (901, 9010, 'committed-after-crash')")
		execInitialSchemaSQL(t, f.db, "INSERT INTO "+newSchema+".zz_empty VALUES (1, 10)")
		execInitialSchemaSQL(t, f.db, "INSERT INTO "+oldSchema+".ledger VALUES (3, 30)")
	}

	startedAt := time.Now()
	control, stop := startChild("restart")
	defer stop()
	require.NoError(t, control.Encode(true))
	for _, f := range fixtures {
		waitInitialSchemaData(t, f.db, doris, newSchema, f.newTarget)
		completed := filepath.Join(workspace, f.sourceID, "1", "bootstrap", "00000000000000000002.record")
		require.Eventually(t, func() bool {
			_, err := os.Stat(completed)
			return err == nil
		}, 15*time.Second, 50*time.Millisecond, "%s 未写入本地完成记录", f.flavor)
		var baseline string
		require.NoError(t, doris.QueryRowContext(ctx, initialBaselineSQL, f.key).Scan(&baseline))
		require.EqualValues(t, 1, f.checkpointWrites.Load(), "同一个基线只能写入一次")
		if window == afterSnapshotCheckpoint {
			require.Equal(t, f.checkpointJSON, baseline, "恢复必须使用原基线")
		}
		t.Logf("%s 重启后 %.3fs 内确认完成标记并逐主键核对 alpha=4、wide=239、zz_empty=1", f.flavor, time.Since(startedAt).Seconds())
		execInitialSchemaSQL(t, f.db, "UPDATE "+newSchema+".alpha SET amount=444 WHERE id=4")
	}
	for _, f := range fixtures {
		waitInitialSchemaData(t, f.db, doris, newSchema, f.newTarget)
		records, err := filepath.Glob(filepath.Join(workspace, f.sourceID, "1", "snapshot-batches", "*.record"))
		require.NoError(t, err)
		require.Empty(t, records, "基线完成后应回收快照文件")
		waitInitialSchemaCount(t, doris, f.oldTarget+".ledger", 3)
		execInitialSchemaSQL(t, f.db, "UPDATE "+oldSchema+".ledger SET amount=31 WHERE id=3")
		require.Eventually(t, func() bool {
			var amount int
			return doris.QueryRowContext(ctx, "SELECT amount FROM "+f.oldTarget+".ledger WHERE id=3").Scan(&amount) == nil && amount == 31
		}, 20*time.Second, 100*time.Millisecond, "%s 其他库重启后未继续同步", f.flavor)

		verifyInitialSchemaReceipts(t, doris, &gate, f.key, f.newTarget, window == beforeSnapshotSeal)
		verifyInitialIncrementalReceipts(t, doris, &gate, f.key, window == beforeSnapshotSeal)
		t.Logf("%s 核对快照与增量回执，每批只转发一次，其他库增量已恢复", f.flavor)
	}
}

// waitInitialSnapshotLoadFailure 确认第二批停在目标接纳之前或确认响应返回之前。
func waitInitialSnapshotLoadFailure(t *testing.T, doris *sql.DB, gate *initialSnapshotGate, database string, beforeDoris bool) {
	t.Helper()
	wantWideRows := 158
	if beforeDoris {
		wantWideRows = 79
	}
	var label, checksum string
	require.Eventually(t, func() bool {
		gate.mu.Lock()
		label = gate.blocked[database]
		checksum = gate.checksums[label]
		gate.mu.Unlock()
		return label != "" && initialSchemaCount(t.Context(), doris, database+".wide") == wantWideRows
	}, 35*time.Second, 100*time.Millisecond, "%s 未到达第二批快照导入故障点", database)
	var originalChecksum string
	var verified bool
	require.NoError(t, doris.QueryRowContext(t.Context(),
		"SELECT payload_checksum, verified FROM cdc_control.load_receipts WHERE load_label=?", label).Scan(&originalChecksum, &verified))
	require.Equal(t, checksum, originalChecksum)
	require.False(t, verified, "强杀前调用方尚未确认第二批的导入状态")
}

// runInitialSnapshotChild 通过宿主正常接口安装同一份配置并启动首次发现的库。
func runInitialSnapshotChild(t *testing.T) {
	control := json.NewDecoder(os.Stdin)
	var config hcdc.Config
	require.NoError(t, control.Decode(&config))
	m := New(t.Context())
	require.NoError(t, m.Reload(t.Context(), &config))
	snapshot := m.Snapshot()
	require.Len(t, snapshot.Sources, 4, "四个库必须在首次加载配置时已被发现")
	for _, source := range snapshot.Sources {
		if strings.HasSuffix(source.ID, "_old") {
			require.NoError(t, m.Start(t.Context(), source.ID))
		}
	}
	var start bool
	require.NoError(t, control.Decode(&start))
	require.True(t, start)
	for _, source := range snapshot.Sources {
		if strings.HasSuffix(source.ID, "_new") {
			require.NoError(t, m.Start(t.Context(), source.ID))
		}
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// initialSnapshotGate 在 Doris 接纳第二批之前或之后阻断 HTTP 响应，直到子进程被强杀。
type initialSnapshotGate struct {
	mu             sync.Mutex
	holdLoad       bool
	beforeDoris    string
	firstWideLabel map[string]string
	blocked        map[string]string
	requests       map[string]int
	forwarded      map[string]int
	checksums      map[string]string
	changedPayload bool
}

// serve 识别宽表的第二个快照批次，并统计全量和增量实际转发的 Label。
func (g *initialSnapshotGate) serve(w http.ResponseWriter, request *http.Request, proxy *httputil.ReverseProxy) {
	label := request.Header.Get("label")
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 4 || !strings.HasPrefix(label, "cdc_") || request.Method != http.MethodPut {
		proxy.ServeHTTP(w, request)
		return
	}
	database, table := parts[1], parts[2]
	g.mu.Lock()
	checksum := request.Header.Get("comment")
	if g.requests[label] == 0 {
		g.checksums[label] = checksum
	} else if g.checksums[label] != checksum {
		g.changedPayload = true
	}
	g.requests[label]++
	wide := g.holdLoad && strings.HasPrefix(label, "cdc_snapshot_") && table == "wide" && g.blocked[database] == ""
	firstLabel := g.firstWideLabel[database]
	withhold := wide && firstLabel != "" && firstLabel != label
	if wide && firstLabel == "" {
		g.firstWideLabel[database] = label
	}
	if withhold {
		g.blocked[database] = label
	}
	beforeDoris := withhold && database == g.beforeDoris
	if !beforeDoris {
		g.forwarded[label]++
	}
	g.mu.Unlock()
	if beforeDoris {
		_, _ = io.Copy(io.Discard, request.Body)
		<-request.Context().Done()
		return
	}
	if withhold {
		ctx := context.WithValue(request.Context(), initialSnapshotHoldKey{}, true)
		request = request.WithContext(ctx)
	}
	proxy.ServeHTTP(w, request)
}

type initialSnapshotHoldKey struct{}

// holdResponse 让已提交导入的调用方无法收到 Doris 返回的结果。
func (g *initialSnapshotGate) holdResponse(response *http.Response) error {
	ctx := response.Request.Context()
	if ctx.Value(initialSnapshotHoldKey{}) == true {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// openInitialSchemaDatabase 校验测试连接并清除默认库，所有操作使用完整限定名。
func openInitialSchemaDatabase(t *testing.T, dsn string) (*sql.DB, *mysql.Config) {
	t.Helper()
	connection, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	connection.DBName = ""
	db, err := sql.Open("mysql", connection.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(t.Context()))
	return db, connection
}

// execInitialSchemaSQL 执行仅作用于本轮隔离库的测试 SQL。
func execInitialSchemaSQL(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), statement)
	require.NoError(t, err)
}

// initialSchemaCount 允许轮询时目标表尚未创建。
func initialSchemaCount(ctx context.Context, db *sql.DB, table string) int {
	var rows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&rows); err != nil {
		return -1
	}
	return rows
}

// waitInitialSchemaCount 等待 Doris 中的指定表可见期望行数。
func waitInitialSchemaCount(t *testing.T, db *sql.DB, table string, expected int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return initialSchemaCount(t.Context(), db, table) == expected
	}, 30*time.Second, 100*time.Millisecond, "%s 未达到 %d 行", table, expected)
}

// waitInitialSchemaData 逐主键比较所有字段，长字段同时比较长度和内容摘要。
func waitInitialSchemaData(t *testing.T, source, doris *sql.DB, schema, target string) {
	t.Helper()
	for _, table := range []string{"alpha", "wide", "zz_empty"} {
		columns := "id, amount, 0, ''"
		if table == "wide" {
			columns = "id, amount, LENGTH(payload), MD5(payload)"
		}
		query := "SELECT " + columns + " FROM "
		expected, err := readInitialSchemaData(t.Context(), source, query+schema+"."+table+" ORDER BY id")
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			actual, err := readInitialSchemaData(t.Context(), doris, query+target+"."+table+" ORDER BY id")
			return err == nil && slices.Equal(expected, actual)
		}, 30*time.Second, 100*time.Millisecond, "%s.%s 的行内容未追平", target, table)
	}
}

// readInitialSchemaData 读取稳定顺序的主键和字段值。
func readInitialSchemaData(ctx context.Context, db *sql.DB, statement string) ([]string, error) {
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var data []string
	for rows.Next() {
		var id, amount, size int
		var hash string
		if err := rows.Scan(&id, &amount, &size, &hash); err != nil {
			return nil, err
		}
		data = append(data, fmt.Sprintf("%d:%d:%d:%s", id, amount, size, hash))
	}
	return data, rows.Err()
}

// verifyInitialSchemaReceipts 对照 Doris 回执和代理，确认原快照 Label 的提交次数与总行数。
func verifyInitialSchemaReceipts(t *testing.T, db *sql.DB, gate *initialSnapshotGate, key, database string, rescanned bool) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT r.load_label, r.payload_checksum, r.expected_rows, r.verified, r.target_table "+
		"FROM cdc_control.load_receipts r JOIN cdc_control.cdc_load_history h ON r.load_label=h.load_label "+
		"WHERE h.stream_key=? AND h.sequence_no=0", key)
	require.NoError(t, err)
	defer rows.Close()
	var batchSizes []int
	rowsByTable := make(map[string]int)
	for rows.Next() {
		var label, checksum, targetTable string
		var batchRows int
		var verified bool
		require.NoError(t, rows.Scan(&label, &checksum, &batchRows, &verified, &targetTable))
		require.True(t, verified, "快照回执必须经过核对")
		gate.mu.Lock()
		transmissions, requests := gate.forwarded[label], gate.requests[label]
		expectedChecksum := gate.checksums[label]
		refill := database == gate.beforeDoris && label == gate.blocked[database]
		changedPayload := gate.changedPayload
		gate.mu.Unlock()
		require.False(t, changedPayload, "重新发送不得修改原 Label 的载荷摘要")
		require.Len(t, expectedChecksum, 64)
		require.Equal(t, expectedChecksum, checksum)
		require.Equal(t, 1, transmissions, "每个快照 Label 只能向 Doris 转发一次")
		if refill {
			require.Equal(t, 2, requests, "Doris 未收到的批次应按原 Label 补发")
		} else {
			require.Equal(t, 1, requests, "Doris 已收到的批次不应重新发送")
		}
		batchSizes = append(batchSizes, batchRows)
		rowsByTable[targetTable] += batchRows
	}
	require.NoError(t, rows.Err())
	if rescanned {
		require.Equal(t, map[string]int{database + ".alpha": 4, database + ".wide": 239, database + ".zz_empty": 1}, rowsByTable)
	} else {
		require.ElementsMatch(t, []int{4, 79, 79, 79, 3}, batchSizes)
		require.Equal(t, map[string]int{database + ".alpha": 4, database + ".wide": 240}, rowsByTable)
	}
	t.Logf("快照回执 %s %d 批，逐表行数 %v", database, len(batchSizes), rowsByTable)
}

// verifyInitialIncrementalReceipts 核对已交付的增量批次和行变更数，并确认每个 Label 只提交一次。
func verifyInitialIncrementalReceipts(t *testing.T, db *sql.DB, gate *initialSnapshotGate, key string, rescanned bool) {
	t.Helper()
	expectedBatches, expectedRows := 6, 7
	if rescanned {
		expectedBatches, expectedRows = 1, 1
	}
	const query = "FROM cdc_control.load_receipts r JOIN cdc_control.cdc_load_history h ON r.load_label=h.load_label " +
		"JOIN cdc_control.checkpoints c ON h.stream_key=c.stream_key AND h.sequence_no=c.sequence_no " +
		"WHERE h.stream_key=? AND h.sequence_no>0 AND r.verified"
	require.Eventually(t, func() bool {
		var count int
		err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) "+query, key).Scan(&count)
		return err == nil && count >= expectedBatches
	}, 30*time.Second, 100*time.Millisecond, "增量检查点未确认所有探针批次")
	rows, err := db.QueryContext(t.Context(), "SELECT r.load_label, r.expected_rows "+query, key)
	require.NoError(t, err)
	defer rows.Close()
	var batches, totalRows int
	for rows.Next() {
		var label string
		var rowCount int
		require.NoError(t, rows.Scan(&label, &rowCount))
		gate.mu.Lock()
		requests, forwarded := gate.requests[label], gate.forwarded[label]
		gate.mu.Unlock()
		require.Equal(t, 1, requests)
		require.Equal(t, 1, forwarded)
		batches++
		totalRows += rowCount
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expectedBatches, batches)
	require.Equal(t, expectedRows, totalRows)
	t.Logf("增量回执 %d 批，行变更 %d 次", batches, totalRows)
}

// initialSnapshotStreamKey 计算初始库在 Doris 控制表中的隔离测试键。
func initialSnapshotStreamKey(id string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("10:hcdc-canal%d:%s1:1", len(id), id))))
}

// cleanupInitialSnapshot 只删除本轮测试创建的库、流回执和写入权。
func cleanupInitialSnapshot(t *testing.T, source, target *sql.DB, schema, database, key string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	execute := func(db *sql.DB, statement string, args ...any) {
		if _, err := db.ExecContext(ctx, statement, args...); err != nil {
			t.Logf("清理隔离对象：%v", err)
		}
	}
	execute(source, "DROP DATABASE IF EXISTS "+schema)
	execute(target, "DROP DATABASE IF EXISTS "+database)
	for _, table := range []string{"load_recheck_deadlines", "load_receipts"} {
		execute(target, "DELETE FROM cdc_control."+table+" USING cdc_control.cdc_load_history h "+
			"WHERE "+table+".load_label=h.load_label AND h.stream_key=?", key)
	}
	for _, table := range []string{"cdc_load_history", "checkpoints", "schema_versions"} {
		execute(target, "DELETE FROM cdc_control."+table+" WHERE stream_key=?", key)
	}
	for kind, value := range map[string]string{"target": database, "stream": "cdc_control/" + key} {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
		execute(target, "DROP VIEW IF EXISTS cdc_writer_registry."+kind+"_"+hash[:48])
	}
}

type snapshotSQLGateAction int

const (
	snapshotSQLPass snapshotSQLGateAction = iota
	snapshotSQLHoldBefore
	snapshotSQLHoldResponse
)

type snapshotSQLGate struct {
	listener net.Listener
	hit      chan string
	released chan struct{}
	release  sync.Once
}

// newSnapshotSQLGate 转发隔离测试的明文 MySQL 协议连接。匹配函数可能并发执行。
// Doris SQL 驱动会将参数内插到 COM_QUERY，故可区分同一个控制表上的不同流。
func newSnapshotSQLGate(t *testing.T, upstream string, decide func(string) snapshotSQLGateAction) *snapshotSQLGate {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gate := &snapshotSQLGate{
		listener: listener,
		hit:      make(chan string, 16),
		released: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer client.Close()
				dialer := net.Dialer{Timeout: 5 * time.Second}
				server, err := dialer.DialContext(ctx, "tcp", upstream)
				if err != nil {
					return
				}
				defer server.Close()
				stop := context.AfterFunc(ctx, func() {
					_ = client.Close()
					_ = server.Close()
				})
				defer stop()
				gate.forward(client, server, decide)
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		cancel()
		gate.disarm()
		workers.Wait()
	})
	return gate
}

// address 返回供 CDC 子进程连接的固定测试端点。
func (g *snapshotSQLGate) address() string { return g.listener.Addr().String() }

// disarm 释放被阻断的请求。重启后的连接继续通过同一地址转发。
func (g *snapshotSQLGate) disarm() { g.release.Do(func() { close(g.released) }) }

// hold 在父测试收到实际 SQL 后保留故障窗口，直到子进程退出后解除。
func (g *snapshotSQLGate) hold(query string) {
	select {
	case g.hit <- query:
	case <-g.released:
		return
	}
	<-g.released
}

// forward 仅拦截命令起始包中的 COM_QUERY，其他认证、命令和结果仍按原字节传输。
func (g *snapshotSQLGate) forward(client, server net.Conn, decide func(string) snapshotSQLGateAction) {
	var blockedReply atomic.Pointer[string]
	var upstream sync.WaitGroup
	upstream.Go(func() {
		defer server.Close()
		copySnapshotSQLPackets(server, client, func(packet []byte) {
			if len(packet) <= 4 || packet[3] != 0 || packet[4] != 0x03 {
				return
			}
			query := string(packet[5:])
			action := decide(query)
			select {
			case <-g.released:
				return
			default:
			}
			switch action {
			case snapshotSQLHoldBefore:
				g.hold(query)
			case snapshotSQLHoldResponse:
				blockedReply.Store(&query)
			}
		})
	})
	copySnapshotSQLPackets(client, server, func(packet []byte) {
		query := blockedReply.Swap(nil)
		// INSERT 的首个响应包为 OK 时，Doris 已接收该请求。父侧另行读回检查点。
		if query != nil && len(packet) > 4 && packet[4] == 0x00 {
			g.hold(*query)
		}
	})
	_ = client.Close()
	upstream.Wait()
}

// copySnapshotSQLPackets 转发完整的 MySQL 包，在写出前交给故障注入检查。
func copySnapshotSQLPackets(dst, src net.Conn, beforeWrite func([]byte)) {
	var header [4]byte
	for {
		if _, err := io.ReadFull(src, header[:]); err != nil {
			return
		}
		size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
		packet := make([]byte, len(header)+size)
		copy(packet, header[:])
		if _, err := io.ReadFull(src, packet[len(header):]); err != nil {
			return
		}
		beforeWrite(packet)
		if count, err := dst.Write(packet); err != nil || count != len(packet) {
			return
		}
	}
}
