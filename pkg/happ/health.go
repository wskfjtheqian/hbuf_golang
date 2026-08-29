package happ

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// HealthStatus 健康状态
type HealthStatus struct {
	Status    string          `json:"status"`            // ready | not_ready | shutting_down | starting
	Message   string          `json:"message,omitempty"` // 详细信息
	Timestamp int64           `json:"timestamp"`         // 时间戳
	Checks    map[string]bool `json:"checks,omitempty"`  // 依赖检查
}

// Health K8s 健康检查
type Health struct {
	isReady        atomic.Bool
	isLive         atomic.Bool
	isShuttingDown atomic.Bool
	isStarted      atomic.Bool // 新增：标记是否已启动完成

	// 依赖检查函数
	readinessChecks []func() error
	livenessChecks  []func() error

	mux http.ServeMux
}

func NewHealth() *Health {
	h := &Health{
		readinessChecks: make([]func() error, 0),
		livenessChecks:  make([]func() error, 0),
	}
	h.isReady.Store(false)   // 初始未就绪
	h.isLive.Store(true)     // 初始存活
	h.isStarted.Store(false) // 初始未启动
	h.isShuttingDown.Store(false)

	h.mux.HandleFunc("/health/ready", h.ready)
	h.mux.HandleFunc("/health/live", h.live)
	h.mux.HandleFunc("/health/startup", h.startup)
	h.mux.HandleFunc("/health/", h.healthSummary)

	return h
}

func (h *Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 支持 Accept 头
	w.Header().Set("Content-Type", "application/json")
	h.mux.ServeHTTP(w, r)
}

// ====== 就绪检查 ======
// 判断 Pod 是否准备好接收流量
func (h *Health) ready(w http.ResponseWriter, r *http.Request) {
	status := h.getHealthStatus()

	// 1. 检查是否正在关闭
	if h.isShuttingDown.Load() {
		h.writeResponse(w, http.StatusServiceUnavailable, HealthStatus{
			Status:    "shutting_down",
			Message:   "Pod is shutting down, please retry",
			Timestamp: time.Now().Unix(),
			Checks:    status.Checks,
		})
		return
	}

	// 2. 检查是否就绪
	if !h.isReady.Load() {
		h.writeResponse(w, http.StatusServiceUnavailable, HealthStatus{
			Status:    "not_ready",
			Message:   "Pod is not ready to receive traffic",
			Timestamp: time.Now().Unix(),
			Checks:    status.Checks,
		})
		return
	}

	// 3. 执行就绪检查（依赖检查）
	if err := h.runReadinessChecks(); err != nil {
		h.writeResponse(w, http.StatusServiceUnavailable, HealthStatus{
			Status:    "not_ready",
			Message:   err.Error(),
			Timestamp: time.Now().Unix(),
			Checks:    status.Checks,
		})
		return
	}

	// 4. 全部通过
	h.writeResponse(w, http.StatusOK, HealthStatus{
		Status:    "ready",
		Message:   "Pod is ready to receive traffic",
		Timestamp: time.Now().Unix(),
		Checks:    status.Checks,
	})
}

// ====== 存活检查 ======
// 判断 Pod 是否需要重启
func (h *Health) live(w http.ResponseWriter, r *http.Request) {
	status := h.getHealthStatus()

	// 1. 检查是否存活
	if !h.isLive.Load() {
		h.writeResponse(w, http.StatusInternalServerError, HealthStatus{
			Status:    "unhealthy",
			Message:   "Pod is not healthy, will be restarted",
			Timestamp: time.Now().Unix(),
			Checks:    status.Checks,
		})
		return
	}

	// 2. 执行存活检查
	if err := h.runLivenessChecks(); err != nil {
		h.writeResponse(w, http.StatusInternalServerError, HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			Timestamp: time.Now().Unix(),
			Checks:    status.Checks,
		})
		return
	}

	h.writeResponse(w, http.StatusOK, HealthStatus{
		Status:    "alive",
		Message:   "Pod is healthy",
		Timestamp: time.Now().Unix(),
		Checks:    status.Checks,
	})
}

// ====== 启动检查 ======
// 判断应用是否启动完成
func (h *Health) startup(w http.ResponseWriter, r *http.Request) {
	// 1. 检查是否已启动
	if !h.isStarted.Load() {
		h.writeResponse(w, http.StatusServiceUnavailable, HealthStatus{
			Status:    "starting",
			Message:   "Pod is still starting up",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// 2. 检查是否正在关闭（启动检查也应该考虑关闭状态）
	if h.isShuttingDown.Load() {
		h.writeResponse(w, http.StatusServiceUnavailable, HealthStatus{
			Status:    "shutting_down",
			Message:   "Pod is shutting down",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// 3. 检查是否就绪（启动完成 = 就绪）
	if !h.isReady.Load() {
		h.writeResponse(w, http.StatusServiceUnavailable, HealthStatus{
			Status:    "not_ready",
			Message:   "Pod started but not ready",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	h.writeResponse(w, http.StatusOK, HealthStatus{
		Status:    "started",
		Message:   "Pod started successfully",
		Timestamp: time.Now().Unix(),
	})
}

// ====== 健康汇总 ======
func (h *Health) healthSummary(w http.ResponseWriter, r *http.Request) {
	status := h.getHealthStatus()

	httpStatus := http.StatusOK
	if !h.isReady.Load() || h.isShuttingDown.Load() {
		httpStatus = http.StatusServiceUnavailable
	}

	h.writeResponse(w, httpStatus, status)
}

// ====== 辅助方法 ======

func (h *Health) writeResponse(w http.ResponseWriter, statusCode int, status HealthStatus) {
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(status)
}

func (h *Health) getHealthStatus() HealthStatus {
	checks := make(map[string]bool)

	checks["ready"] = h.isReady.Load()
	checks["live"] = h.isLive.Load()
	checks["started"] = h.isStarted.Load()
	checks["shutting_down"] = h.isShuttingDown.Load()

	return HealthStatus{
		Status:    "ok",
		Timestamp: time.Now().Unix(),
		Checks:    checks,
	}
}

// ====== 依赖检查 ======

// AddReadinessCheck 添加就绪检查
func (h *Health) AddReadinessCheck(name string, fn func() error) {
	h.readinessChecks = append(h.readinessChecks, fn)
}

// AddLivenessCheck 添加存活检查
func (h *Health) AddLivenessCheck(name string, fn func() error) {
	h.livenessChecks = append(h.livenessChecks, fn)
}

func (h *Health) runReadinessChecks() error {
	for _, fn := range h.readinessChecks {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func (h *Health) runLivenessChecks() error {
	for _, fn := range h.livenessChecks {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// ====== 状态设置 ======

// SetReady 标记就绪
func (h *Health) SetReady(ready bool) {
	h.isReady.Store(ready)
	if ready {
		h.isStarted.Store(true) // 就绪意味着已启动
	}
}

// SetLive 标记存活
func (h *Health) SetLive(live bool) {
	h.isLive.Store(live)
}

// SetStarted 标记启动完成
func (h *Health) SetStarted(started bool) {
	h.isStarted.Store(started)
	if started {
		// 启动完成时，如果 ready 未设置，设为 true
		if !h.isReady.Load() && !h.isShuttingDown.Load() {
			h.isReady.Store(true)
		}
	}
}

// SetShuttingDown 标记正在关闭
func (h *Health) SetShuttingDown() {
	h.isShuttingDown.Store(true)
	h.isReady.Store(false)   // 立即摘除流量
	h.isStarted.Store(false) // 不再接受新请求
}

// Shutdown 完全关闭健康检查
func (h *Health) Shutdown() {
	h.isShuttingDown.Store(true)
	h.isReady.Store(false)
	h.isLive.Store(false)
	h.isStarted.Store(false)
}

func (h *Health) IsShuttingDown() bool {
	return h.isShuttingDown.Load()
}
