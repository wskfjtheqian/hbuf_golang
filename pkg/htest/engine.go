package htest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

func NewT(t *testing.T) *T {
	return &T{
		Context: context.Background(),
		T:       t,
	}
}

type T struct {
	T       *testing.T
	success bool
	context.Context
}

func (t *T) Call(info any, err error) {
	if err != nil {
		if t.T != nil {
			marshal, err := json.MarshalIndent(err, "", "\t")
			if err != nil {
				t.T.Error(err)
				return
			}
			t.T.Error(string(marshal))
		}
		t.success = false
	} else {
		if t.T != nil {
			marshal, err := json.MarshalIndent(info, "", "\t")
			if err != nil {
				t.T.Error(err)
				return
			}
			t.T.Log(string(marshal))
		}
		t.success = true
	}
}

func (t *T) IsSuccess() bool {
	return t.success
}

func (t *T) Error(err error) {
	if t.T != nil {
		marshal, err := json.MarshalIndent(err, "", "\t")
		if err != nil {
			t.T.Error(err)
			return
		}
		t.T.Error(string(marshal))
	}
}

func (t *T) Log(err error) {
	if t.T != nil {
		marshal, err := json.MarshalIndent(err, "", "\t")
		if err != nil {
			t.T.Log(err)
			return
		}
		t.T.Log(string(marshal))
	}
}

// API 定义
type API struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"` // 权重
	//Timeout time.Duration
	method func(t *T)
}

// StressConfig 压测配置
type StressConfig struct {
	Duration    time.Duration // 持续时间
	Concurrency int           // 并发数
	RateLimit   int           // QPS限制
}

// 测试结果结构体
type TestResult struct {
	Timestamp   time.Time
	Success     int64
	Failure     int64
	Total       int64
	AvgLatency  time.Duration
	TPS         float64
	MinLatency  time.Duration
	MaxLatency  time.Duration
	SuccessRate float64 // 添加这个字段
}

// Engine  在Engine结构体中添加详细统计
type Engine struct {
	APIs          []*API
	config        StressConfig
	isRunning     atomic.Bool
	cancelFunc    context.CancelFunc
	stats         *Statistics
	detailedStats *DetailedStats
	mu            sync.RWMutex
	resultsChan   chan TestResult

	// TPS计算窗口
	tpsWindowSize  time.Duration
	requestTimes   []time.Time
	requestTimesMu sync.RWMutex

	// HTTP客户端池
	clientPool sync.Pool

	// 调试模式
	debugMode   bool
	logger      *log.Logger
	WithContext func(ctx context.Context) context.Context
}

func NewEngine() *Engine {
	return &Engine{
		stats:         NewStatistics(),
		detailedStats: NewDetailedStats(),
		resultsChan:   make(chan TestResult, 1000),
		tpsWindowSize: 1 * time.Second,
		requestTimes:  make([]time.Time, 0),
		debugMode:     false,
		logger:        log.New(os.Stdout, "[Engine] ", log.LstdFlags|log.Lmicroseconds),
		clientPool: sync.Pool{
			New: func() interface{} {
				return &http.Client{
					Timeout: 30 * time.Second,
					Transport: &http.Transport{
						MaxIdleConns:        100,
						MaxIdleConnsPerHost: 100,
						IdleConnTimeout:     90 * time.Second,
					},
				}
			},
		},
	}
}

// 执行单个请求
func (e *Engine) executeRequest(ctx context.Context, api API) {
	start := htime.NowTime()

	// 记录请求开始时间（用于计算TPS）
	e.requestTimesMu.Lock()
	e.requestTimes = append(e.requestTimes, start)
	// 清理超过窗口的时间记录
	cutoff := start.Add(-e.tpsWindowSize)
	i := 0
	for ; i < len(e.requestTimes); i++ {
		if e.requestTimes[i].After(cutoff) {
			break
		}
	}
	e.requestTimes = e.requestTimes[i:]
	e.requestTimesMu.Unlock()

	client := e.clientPool.Get().(*http.Client)
	defer e.clientPool.Put(client)
	if e.WithContext != nil {
		ctx = e.WithContext(ctx)
	}

	t := &T{
		Context: ctx,
	}

	api.method(t)

	latency := time.Since(start)
	if t.IsSuccess() {
		e.stats.RecordSuccess(api.Name, latency)
		e.detailedStats.RecordRequest(api.Name, true, latency)
	} else {
		e.stats.RecordFailure(api.Name, latency)
		e.detailedStats.RecordRequest(api.Name, false, latency)
	}
}

// 计算当前TPS
// 改进的TPS计算方法
func (e *Engine) calculateCurrentTPS() float64 {
	e.requestTimesMu.RLock()
	defer e.requestTimesMu.RUnlock()

	if len(e.requestTimes) == 0 {
		return 0
	}

	now := htime.NowTime()
	cutoff := now.Add(-e.tpsWindowSize)

	// 统计窗口内的请求数
	count := 0
	// 从最新到最旧遍历
	for i := len(e.requestTimes) - 1; i >= 0; i-- {
		if e.requestTimes[i].After(cutoff) {
			count++
		} else {
			break
		}
	}

	return float64(count)
}

// 获取详细统计（在collectStats中已经调用了TakeSnapshot）
func (e *Engine) GetDetailedStats() map[string]interface{} {
	// 直接获取详细统计的当前快照
	apiStats := e.detailedStats.GetAPIStats()
	historicalData := e.detailedStats.GetHistoricalData()

	// 准备历史数据
	timestamps := make([]int64, len(historicalData))
	successRates := make([]float64, len(historicalData))
	avgLatencies := make([]float64, len(historicalData))
	tpsData := make([]float64, len(historicalData))

	for i, data := range historicalData {
		timestamps[i] = data.Timestamp.Unix()
		successRates[i] = data.SuccessRate
		avgLatencies[i] = data.AvgLatency
		tpsData[i] = data.TotalTPS
	}

	return map[string]interface{}{
		"api_stats": apiStats,
		"historical": map[string]interface{}{
			"timestamps":    timestamps,
			"success_rates": successRates,
			"avg_latencies": avgLatencies,
			"tps":           tpsData,
		},
	}
}

// ResetStats 重置统计
func (e *Engine) ResetStats() {
	e.stats.Reset()
	e.detailedStats.Reset()
}

// 启动压测
func (e *Engine) Start(config StressConfig) error {
	if e.isRunning.Load() {
		return fmt.Errorf("压测已在运行中")
	}

	e.mu.Lock()
	e.config = config
	e.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFunc = cancel
	e.isRunning.Store(true)

	// 重置所有统计
	e.stats.Reset()
	e.detailedStats.Reset()

	// 清空请求时间记录
	e.requestTimesMu.Lock()
	e.requestTimes = make([]time.Time, 0)
	e.requestTimesMu.Unlock()

	// 启动压测协程
	go e.runStressTest(ctx)

	// 启动统计收集协程
	go e.collectStats(ctx)

	return nil
}

// Stop 停止压测
func (e *Engine) Stop() {
	if e.isRunning.Load() && e.cancelFunc != nil {
		e.cancelFunc()
		e.isRunning.Store(false)
	}
}

// runStressTest 运行压测
func (e *Engine) runStressTest(ctx context.Context) {
	defer func() {
		e.isRunning.Store(false)
	}()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, e.config.Concurrency)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// 启动统计协程
	go e.collectStats(ctx)

	endTime := htime.NowTime().Add(e.config.Duration)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if htime.NowTime().After(endTime) {
				return
			}

			// 根据权重分配请求
			for i := 0; i < e.config.RateLimit; i++ {
				select {
				case <-ctx.Done():
					return
				default:
					semaphore <- struct{}{}
					wg.Add(1)

					go func() {
						defer wg.Done()
						defer func() { <-semaphore }()

						api := e.selectAPIByWeight()
						e.executeRequest(ctx, *api)
					}()
				}
			}
		}
	}

	wg.Wait()
}

// selectAPIByWeight 根据权重选择API
func (e *Engine) selectAPIByWeight() *API {
	e.mu.RLock()
	defer e.mu.RUnlock()

	totalWeight := 0
	for _, api := range e.APIs {
		totalWeight += api.Weight
	}

	r := rand.Intn(totalWeight)
	current := 0

	for _, api := range e.APIs {
		current += api.Weight
		if r < current {
			return api
		}
	}

	return e.APIs[0]
}

// 收集统计信息
// 收集统计信息
func (e *Engine) collectStats(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	lastTime := htime.NowTime()
	lastCount := int64(0)

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			// 获取基础统计
			stats := e.stats.GetSnapshot()

			// 计算当前TPS
			currentTPS := e.calculateCurrentTPS()

			// 计算增量TPS作为备用
			totalRequests := stats.TotalSuccess + stats.TotalFailure
			elapsed := now.Sub(lastTime).Seconds()
			incrementalTPS := 0.0
			if elapsed > 0 {
				incrementalTPS = float64(totalRequests-lastCount) / elapsed
			}
			lastTime = now
			lastCount = totalRequests

			// 使用两种方法中的较大值
			finalTPS := currentTPS
			if incrementalTPS > finalTPS {
				finalTPS = incrementalTPS
			}

			// 获取详细统计
			detailedStats := e.detailedStats.TakeSnapshot()

			// 计算总体最小/最大延迟
			var minLatency, maxLatency time.Duration
			for _, apiStats := range detailedStats.APIStats {
				apiMin := time.Duration(apiStats.MinLatency * 1e6)
				apiMax := time.Duration(apiStats.MaxLatency * 1e6)

				if minLatency == 0 || apiMin < minLatency {
					minLatency = apiMin
				}
				if apiMax > maxLatency {
					maxLatency = apiMax
				}
			}

			// 计算成功率 - 确保计算正确
			successRate := 0.0
			total := stats.TotalSuccess + stats.TotalFailure
			if total > 0 {
				successRate = float64(stats.TotalSuccess) / float64(total) * 100
			}

			// 确保所有字段都有有效值
			result := TestResult{
				Timestamp:   now,
				Success:     stats.TotalSuccess,
				Failure:     stats.TotalFailure,
				Total:       total,
				AvgLatency:  stats.AvgLatency,
				TPS:         finalTPS,
				MinLatency:  minLatency,
				MaxLatency:  maxLatency,
				SuccessRate: successRate,
			}

			// 调试日志
			if e.debugMode {
				e.logger.Printf("Collecting stats: TPS=%.2f, SuccessRate=%.2f%%, Total=%d, Success=%d, Failure=%d",
					finalTPS, successRate, total, stats.TotalSuccess, stats.TotalFailure)
			}

			// 发送结果到通道
			select {
			case e.resultsChan <- result:
			default:
				// 通道满时处理
				if len(e.resultsChan) == cap(e.resultsChan) {
					// 丢弃最旧的数据
					select {
					case <-e.resultsChan:
						// 丢弃一个数据
					default:
					}
				}
			}
		}
	}
}

// GetResultsChan 获取实时结果通道
func (e *Engine) GetResultsChan() <-chan TestResult {
	return e.resultsChan
}

// 获取API分布（实际调用次数，不是配置权重）
func (e *Engine) GetAPIDistribution() map[string]interface{} {
	// 从详细统计中获取实际的调用次数
	apiStats := e.detailedStats.GetAPIStats()

	distribution := make(map[string]interface{})
	total := int64(0)

	// 计算总调用次数
	for _, stats := range apiStats {
		total += stats.Total
	}

	// 计算每个API的百分比
	for apiName, stats := range apiStats {
		percentage := 0.0
		if total > 0 {
			percentage = float64(stats.Total) / float64(total) * 100
		}

		distribution[apiName] = map[string]interface{}{
			"count":      stats.Total,
			"percentage": percentage,
			"success":    stats.Success,
			"failure":    stats.Failure,
		}
	}

	return distribution
}

// IsRunning 获取运行状态
func (e *Engine) IsRunning() bool {
	return e.isRunning.Load()
}

// AddApi 新增API
func (e *Engine) AddApi(name string, weight int, method func(t *T)) {
	e.APIs = append(e.APIs, &API{
		Name:   name,
		Weight: weight,
		method: method,
		//Timeout: 30 * time.Second,
	})
}

func (e *Engine) GetApiList() []*API {
	return e.APIs
}
