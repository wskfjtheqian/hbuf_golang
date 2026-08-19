package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/schema"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ==========================================
// 1. 全局配置与状态定义
// ==========================================
const (
	LogDir             = "./cdc_data_buffer"
	DorisFE_JDBC       = "admin:doris_password@tcp(192.168.1.20:9030)/ods_db"
	DorisStreamLoadURL = "http://192.168.1.20:8030/api/ods_db/%s/_stream_load"
	PrometheusPort     = ":9090"
)

// MySQLInstanceConfig 物理 MySQL 实例源结构
type MySQLInstanceConfig struct {
	InstanceID string   // 物理实例唯一标识
	Addr       string   // IP:Port
	User       string   // 账号
	Password   string   // 密码
	Regex      []string // 正则匹配规则
}

// LiveTableState 单张目标表在内存中的运行状态
type LiveTableState struct {
	mu           sync.Mutex
	ActiveFile   *os.File
	ActivePath   string
	KnownColumns []string // 内存表结构字典缓存
	LastRotated  time.Time
}

// SourceWorker 每一个物理 MySQL 实例的独立守护线程
type SourceWorker struct {
	Config      MySQLInstanceConfig
	CanalEngine *canal.Canal
	ActiveFiles map[string]*os.File // key: Doris目标表名
	mu          sync.Mutex
}

// MultiSourceManager 全局多源管理器
type MultiSourceManager struct {
	workers map[string]*SourceWorker
	mu      sync.Mutex
}

var (
	manager        = &MultiSourceManager{workers: make(map[string]*SourceWorker)}
	globalWG       sync.WaitGroup
	dorisDB        *sql.DB
	isShuttingDown bool
	shutdownMu     sync.RWMutex

	// Prometheus 指标定义
	cdcRowsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cdc_rows_total",
			Help: "Total number of MySQL binlog rows parsed by Golang CDC engine.",
		},
		[]string{"instance_id", "db", "table", "action"},
	)
	cdcDelaySeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cdc_delay_seconds",
			Help: "Real-time synchronization delay in seconds per MySQL instance.",
		},
		[]string{"instance_id"},
	)
)

func init() {
	prometheus.MustRegister(cdcRowsTotal)
	prometheus.MustRegister(cdcDelaySeconds)
}

// ==========================================
// 2. 主入口函数 (Main & Bootstrap)
// ==========================================
func main() {
	log.Println("Initializing Golang Multi-Source CDC Engine...")
	_ = os.MkdirAll(LogDir, 0755)

	var err error
	dorisDB, err = sql.Open("mysql", DorisFE_JDBC)
	if err != nil {
		log.Fatalf("❌ Connect to Doris FE JDBC failed: %v", err)
	}
	defer dorisDB.Close()

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Printf("📊 Prometheus exporter metrics listening on %s/metrics", PrometheusPort)
		if err := http.ListenAndServe(PrometheusPort, nil); err != nil {
			log.Printf("⚠️  Prometheus metrics HTTP server failed: %v", err)
		}
	}()

	instances := []MySQLInstanceConfig{
		{InstanceID: "ins_order_east", Addr: "192.168.1.101:3306", User: "cdc_user", Password: "cdc_password", Regex: []string{"order_db_.*\\.t_order"}},
		{InstanceID: "ins_order_west", Addr: "192.168.2.102:3306", User: "cdc_user", Password: "cdc_password", Regex: []string{"order_db_.*\\.t_order"}},
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go handleGracefulShutdown(sigChan)

	go globalDorisUploaderLoop()

	manager.StartAllWorkers(instances)

	globalWG.Wait()
	log.Println("🏁 [Success] All resources safely released. System shut down gracefully.")
}

// ==========================================
// 3. 多源 Worker 管理与启动恢复状态机
// ==========================================
func (m *MultiSourceManager) StartAllWorkers(instances []MySQLInstanceConfig) {
	for _, config := range instances {
		globalWG.Add(1)

		go func(cfg MySQLInstanceConfig) {
			defer globalWG.Done()

			worker := &SourceWorker{
				Config:      cfg,
				ActiveFiles: make(map[string]*os.File),
			}

			m.mu.Lock()
			m.workers[cfg.InstanceID] = worker
			m.mu.Unlock()

			targetTable := "ods_t_order_all"
			knownCols, err := fetchDorisLiveSchema(targetTable)
			if err != nil {
				log.Printf("⚠️  [Worker-%s] Fetch Doris schema failed: %v. Will init with empty state.", cfg.InstanceID, err)
			}

			worker.initTableBuffer(targetTable, knownCols)
			startPos := worker.loadInstanceBinlogPosition()
			worker.LaunchEngine(startPos)
		}(config)
	}
}

func (w *SourceWorker) initTableBuffer(tableName string, knownCols []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	fileName := fmt.Sprintf("%s_%s_%d.csv.active", w.Config.InstanceID, tableName, time.Now().UnixNano())
	path := filepath.Join(LogDir, fileName)
	f, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	state := &LiveTableState{
		ActiveFile:   f,
		ActivePath:   path,
		KnownColumns: knownCols,
		LastRotated:  time.Now(),
	}

	globalMu.Lock()
	tableStates[fmt.Sprintf("%s_%s", w.Config.InstanceID, tableName)] = state
	globalMu.Unlock()
}

func (w *SourceWorker) LaunchEngine(pos *mysql.Position) {
	cfg := canal.NewDefaultConfig()
	cfg.Addr = w.Config.Addr
	cfg.User = w.Config.User
	cfg.Password = w.Config.Password
	cfg.IncludeTableRegex = w.Config.Regex
	cfg.ServerID = uint32(time.Now().UnixNano() & 0x7FFFFFFF)

	c, err := canal.NewCanal(cfg)
	if err != nil {
		log.Printf("❌ [Worker-%s] Create canal failed: %v", w.Config.InstanceID, err)
		return
	}
	w.CanalEngine = c
	w.CanalEngine.SetEventHandler(&SourceEventHandler{worker: w})

	if pos != nil && pos.Name != "" && pos.Pos > 0 {
		log.Printf("🔄 [Worker-%s] Attaching to saved position -> %s:%d", w.Config.InstanceID, pos.Name, pos.Pos)
		err = w.CanalEngine.RunFrom(*pos)
	} else {
		log.Printf("🆕 [Worker-%s] Start capturing binlog from LATEST position", w.Config.InstanceID)
		err = w.CanalEngine.Run()
	}

	if err != nil {
		shutdownMu.RLock()
		defer shutdownMu.RUnlock()
		if !isShuttingDown {
			log.Printf("🚨 [Worker-%s] Runtime crashed! Connection lost: %v. Triggering auto-reconnect...", w.Config.InstanceID, err)
			w.triggerAutoReconnectLoop()
		}
	}
}

func (w *SourceWorker) triggerAutoReconnectLoop() {
	globalWG.Add(1)
	go func() {
		defer globalWG.Done()
		delay := 5 * time.Second
		for {
			shutdownMu.RLock()
			if isShuttingDown {
				shutdownMu.RUnlock()
				return
			}
			shutdownMu.RUnlock()

			log.Printf("🔄 [Worker-%s] Retrying connection in %v...", w.Config.InstanceID, delay)
			time.Sleep(delay)

			pos := w.loadInstanceBinlogPosition()
			w.LaunchEngine(pos)
			return
		}
	}()
}

// ==========================================
// 4. Binlog 事件监听与动态 DDL 感知内核
// ==========================================
type SourceEventHandler struct {
	canal.DummyEventHandler
	worker *SourceWorker
}

var tableStates = make(map[string]*LiveTableState)
var globalMu sync.Mutex

func (h *SourceEventHandler) OnRow(e *canal.RowsEvent) error {
	if e.Action == canal.DeleteAction {
		return nil
	}

	shutdownMu.RLock()
	if isShuttingDown {
		shutdownMu.RUnlock()
		return fmt.Errorf("engine frozen for system shutdown")
	}
	shutdownMu.RUnlock()

	instanceID := h.worker.Config.InstanceID
	targetTable := "ods_t_order_all"
	stateKey := fmt.Sprintf("%s_%s", instanceID, targetTable)

	globalMu.Lock()
	state := tableStates[stateKey]
	globalMu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(e.Table.Columns) > len(state.KnownColumns) {
		log.Printf("🔔 [DDL Shift Detected] Instance [%s] table [%s.%s] structural changes noticed!", instanceID, e.Table.Schema, e.Table.Name)
		newCols := identifyNewColumns(state.KnownColumns, e.Table.Columns)

		h.worker.rotateActiveFile(targetTable, state)

		for _, col := range newCols {
			dorisType := mapToDorisType(col.RawType)
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s DEFAULT ''", targetTable, col.Name, dorisType)
			_, err := dorisDB.Exec(alterSQL)
			if err != nil {
				log.Printf("❌ [Doris DDL Alter Error] Failed to propagate DDL: %v", err)
			} else {
				log.Printf("✅ [Doris DDL Alter Success] Broadcast schema expansion: %s", alterSQL)
			}
		}
		state.KnownColumns = getColumnNames(e.Table.Columns)
	}

	var sb strings.Builder
	for _, row := range e.Rows {
		var rowValues []string
		for _, val := range row {
			if val == nil {
				rowValues = append(rowValues, "")
			} else {
				s := fmt.Sprintf("%v", val)
				s = strings.ReplaceAll(s, ",", " ")
				rowValues = append(rowValues, s)
			}
		}
		rowValues = append(rowValues, instanceID, e.Table.Schema, e.Table.Name)
		sb.WriteString(strings.Join(rowValues, ",") + "\n")
	}

	_, _ = state.ActiveFile.WriteString(sb.String())

	cdcRowsTotal.WithLabelValues(instanceID, e.Table.Schema, e.Table.Name, strings.ToUpper(e.Action)).Add(float64(len(e.Rows)))

	if e.Header != nil && e.Header.Timestamp > 0 {
		delay := time.Since(time.Unix(int64(e.Header.Timestamp), 0)).Seconds()
		if delay < 0 {
			delay = 0
		}
		cdcDelaySeconds.WithLabelValues(instanceID).Set(delay)
	}

	return nil
}

func (w *SourceWorker) rotateActiveFile(tableName string, state *LiveTableState) {
	if state.ActiveFile != nil {
		_ = state.ActiveFile.Sync()
		_ = state.ActiveFile.Close()
		readyPath := strings.Replace(state.ActivePath, ".active", ".ready", 1)
		_ = os.Rename(state.ActivePath, readyPath)
	}
	fileName := fmt.Sprintf("%s_%s_%d.csv.active", w.Config.InstanceID, tableName, time.Now().UnixNano())
	state.ActivePath = filepath.Join(LogDir, fileName)
	state.ActiveFile, _ = os.OpenFile(state.ActivePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	state.LastRotated = time.Now()
}

// ==========================================
// 5. 异步高吞吐磁盘落盘缓冲投递引擎 (Uploader)
// ==========================================
func globalDorisUploaderLoop() {
	go func() {
		for {
			time.Sleep(10 * time.Second)
			shutdownMu.RLock()
			if isShuttingDown {
				shutdownMu.RUnlock()
				return
			}
			shutdownMu.RUnlock()

			globalMu.Lock()
			for stateKey, state := range tableStates {
				state.mu.Lock()
				fi, err := state.ActiveFile.Stat()
				if err == nil && fi.Size() > 0 && time.Since(state.LastRotated) >= 10*time.Second {
					parts := strings.Split(stateKey, "_")
					instanceID := parts[0] + "_" + parts[1] + "_" + parts[2]
					if len(parts) >= 2 {
						instanceID = strings.Join(parts[0:len(parts)-4], "_")
					}
					// Dynamic mapping proxy
					for _, w := range manager.workers {
						if strings.HasPrefix(stateKey, w.Config.InstanceID) {
							w.rotateActiveFile("ods_t_order_all", state)
							break
						}
					}
				}
				state.mu.Unlock()
			}
			globalMu.Unlock()
		}
	}()

	for {
		files, err := filepath.Glob(filepath.Join(LogDir, "*.ready"))
		if err != nil || len(files) == 0 {
			time.Sleep(1 * time.Second)
			continue
		}

		for _, file := range files {
			targetTable := "ods_t_order_all"
			colsHeader := fetchDorisTableColumnsHeader(targetTable)
			if colsHeader == "" {
				time.Sleep(2 * time.Second)
				continue
			}

			if uploadToDorisCluster(file, targetTable, colsHeader) {
				_ = os.Remove(file)
			} else {
				time.Sleep(5 * time.Second)
				break
			}
		}
	}
}

// ==========================================
// 6. 倒序流式高可用优雅停机引擎 (Shutdown)
// ==========================================
func handleGracefulShutdown(sigChan chan os.Signal) {
	sig := <-sigChan
	log.Printf("⚠️  [Graceful Shutdown Triggered] Signal captured: %v. Freezing workers...", sig)

	shutdownMu.Lock()
	isShuttingDown = true
	shutdownMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	manager.mu.Lock()
	workersList := make([]*SourceWorker, 0, len(manager.workers))
	for _, w := range manager.workers {
		workersList = append(workersList, w)
	}
	manager.mu.Unlock()

	var workerWG sync.WaitGroup
	for _, w := range workersList {
		workerWG.Add(1)
		go func(worker *SourceWorker) {
			defer workerWG.Done()

			log.Printf("⏳ [Shutdown] Disconnecting binary channel for: %s", worker.Config.InstanceID)
			if worker.CanalEngine != nil {
				worker.CanalEngine.Close()
			}

			worker.mu.Lock()
			for targetTable, fileHandle := range worker.ActiveFiles {
				if fileHandle != nil {
					_ = fileHandle.Sync()
					_ = fileHandle.Close()

					activePattern := filepath.Join(LogDir, fmt.Sprintf("%s_%s_*.csv.active", worker.Config.InstanceID, targetTable))
					matches, _ := filepath.Glob(activePattern)
					for _, p := range matches {
						readyPath := strings.Replace(p, ".active", ".ready", 1)
						_ = os.Rename(p, readyPath)
					}
				}
			}
			worker.mu.Unlock()

			if worker.CanalEngine != nil {
				pos := worker.CanalEngine.SyncedPosition()
				worker.saveInstanceBinlogPosition(pos)
			}
			log.Printf("✅ [Shutdown] Worker [%s] has closed cleanly.", worker.Config.InstanceID)
		}(w)
	}

	workerWG.Wait()
	log.Println("⏳ [Shutdown Phase 1/2] All physical MySQL connections closed. File buffers sealed.")

	log.Println("⏳ [Shutdown Phase 2/2] Flushing final residual file packets into Doris warehouse...")

	for {
		files, _ := filepath.Glob(filepath.Join(LogDir, "*.ready"))
		if len(files) == 0 {
			break
		}
		for _, file := range files {
			targetTable := "ods_t_order_all"
			colsHeader := fetchDorisTableColumnsHeader(targetTable)
			if uploadToDorisCluster(file, targetTable, colsHeader) {
				_ = os.Remove(file)
				log.Printf("🚀 Residual package [%s] successfully committed to Doris.", filepath.Base(file))
			} else {
				time.Sleep(2 * time.Second)
			}
		}

		select {
		case <-ctx.Done():
			log.Println("❌ [Shutdown Timeout] Force exiting.")
			os.Exit(1)
		default:
		}
	}

	globalWG.Done()
}

// ==========================================
// 7. 工具底层适配函数集 (Utils)
// ==========================================
func fetchDorisLiveSchema(tableName string) ([]string, error) {
	query := `SELECT COLUMN_NAME FROM information_schema.columns WHERE table_schema = 'ods_db' AND table_name = ? ORDER BY ORDINAL_POSITION ASC`
	rows, err := dorisDB.Query(query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		cols = append(cols, name)
	}
	return cols, nil
}

func fetchDorisTableColumnsHeader(tableName string) string {
	cols, err := fetchDorisLiveSchema(tableName)
	if err != nil || len(cols) == 0 {
		return ""
	}
	return strings.Join(cols, ",")
}

func uploadToDorisCluster(path string, table string, columns string) bool {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return false
	}
	url := fmt.Sprintf(DorisStreamLoadURL, table)
	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(data))
	req.SetBasicAuth("admin", "doris_password")
	req.Header.Set("Expect", "100-continue")
	req.Header.Set("column_separator", ",")
	req.Header.Set("columns", columns)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	defer resp.Body.Close()
	return true
}

func (w *SourceWorker) saveInstanceBinlogPosition(pos mysql.Position) {
	if pos.Name == "" || pos.Pos == 0 {
		return
	}
	metaFile := filepath.Join(LogDir, fmt.Sprintf("meta_position_%s.config", w.Config.InstanceID))
	content := fmt.Sprintf("%s,%d", pos.Name, pos.Pos)
	_ = ioutil.WriteFile(metaFile, []byte(content), 0644)
}

func (w *SourceWorker) loadInstanceBinlogPosition() *mysql.Position {
	metaFile := filepath.Join(LogDir, fmt.Sprintf("meta_position_%s.config", w.Config.InstanceID))
	data, err := ioutil.ReadFile(metaFile)
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(string(data)), ",")
	if len(parts) != 2 {
		return nil
	}
	var pos uint32
	_, _ = fmt.Sscanf(parts[1], "%d", &pos)
	return &mysql.Position{Name: parts[0], Pos: pos}
}

func identifyNewColumns(known []string, current []schema.TableColumn) []schema.TableColumn {
	var diff []schema.TableColumn
	km := make(map[string]bool)
	for _, n := range known {
		km[n] = true
	}
	for _, c := range current {
		if !km[c.Name] {
			diff = append(diff, c)
		}
	}
	return diff
}

func getColumnNames(cols []schema.TableColumn) []string {
	var names []string
	for _, c := range cols {
		names = append(names, c.Name)
	}
	return names
}

func mapToDorisType(rawType string) string {
	rt := strings.ToLower(rawType)
	if strings.Contains(rt, "varchar") || strings.Contains(rt, "char") {
		return "VARCHAR(255)"
	}
	if strings.Contains(rt, "bigint") {
		return "BIGINT"
	}
	if strings.Contains(rt, "int") {
		return "INT"
	}
	if strings.Contains(rt, "text") {
		return "STRING"
	}
	if strings.Contains(rt, "decimal") {
		return "DECIMAL(10,2)"
	}
	return "VARCHAR(255)"
}
