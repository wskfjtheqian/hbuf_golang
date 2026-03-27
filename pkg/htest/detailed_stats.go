package htest

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

// 接口详细统计
type APIDetailedStats struct {
	Name         string
	TotalCount   atomic.Int64
	SuccessCount atomic.Int64
	FailureCount atomic.Int64
	MinLatency   atomic.Int64 // 存储纳秒
	MaxLatency   atomic.Int64 // 存储纳秒
	TotalLatency atomic.Int64 // 存储纳秒
	mu           sync.RWMutex

	// 当前窗口的统计（用于计算实时TPS）
	windowStart   atomic.Int64
	windowCount   atomic.Int64
	windowLatency atomic.Int64
}

type APISnapshot struct {
	Name        string
	Total       int64
	Success     int64
	Failure     int64
	SuccessRate float64
	MinLatency  float64 // 毫秒
	MaxLatency  float64 // 毫秒
	AvgLatency  float64 // 毫秒
	CurrentTPS  float64
}

// 详细统计管理器
type DetailedStats struct {
	apiStats  map[string]*APIDetailedStats
	mu        sync.RWMutex
	history   []AggregatedStats
	historyMu sync.RWMutex
}

type AggregatedStats struct {
	Timestamp   time.Time
	TotalTPS    float64
	AvgLatency  float64
	SuccessRate float64
	APIStats    map[string]APISnapshot
}

func NewDetailedStats() *DetailedStats {
	return &DetailedStats{
		apiStats: make(map[string]*APIDetailedStats),
		history:  make([]AggregatedStats, 0, 3600),
	}
}

func (ds *DetailedStats) RecordRequest(apiName string, success bool, latency time.Duration) {
	stats := ds.getOrCreateAPIStats(apiName)

	now := hutl.NowTime().Unix()
	windowStart := stats.windowStart.Load()

	// 如果窗口过期（超过1秒），重置窗口计数
	if now > windowStart {
		stats.windowStart.Store(now)
		stats.windowCount.Store(0)
		stats.windowLatency.Store(0)
	}

	stats.TotalCount.Add(1)
	latencyNs := latency.Nanoseconds()
	stats.TotalLatency.Add(latencyNs)

	// 更新窗口统计
	stats.windowCount.Add(1)
	stats.windowLatency.Add(latencyNs)

	if success {
		stats.SuccessCount.Add(1)
	} else {
		stats.FailureCount.Add(1)
	}

	// 更新最小延迟（确保不是0或极小值）
	if latencyNs > 0 {
		for {
			old := stats.MinLatency.Load()
			if old == 0 || latencyNs < old {
				if stats.MinLatency.CompareAndSwap(old, latencyNs) {
					break
				}
			} else {
				break
			}
		}
	}

	// 更新最大延迟
	for {
		old := stats.MaxLatency.Load()
		if latencyNs > old {
			if stats.MaxLatency.CompareAndSwap(old, latencyNs) {
				break
			}
		} else {
			break
		}
	}
}

func (ds *DetailedStats) getOrCreateAPIStats(apiName string) *APIDetailedStats {
	ds.mu.RLock()
	stats, exists := ds.apiStats[apiName]
	ds.mu.RUnlock()

	if exists {
		return stats
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// 再次检查，防止重复创建
	if stats, exists := ds.apiStats[apiName]; exists {
		return stats
	}

	stats = &APIDetailedStats{
		Name:       apiName,
		MinLatency: atomic.Int64{},
		MaxLatency: atomic.Int64{},
	}
	stats.windowStart.Store(hutl.NowTime().Unix())
	ds.apiStats[apiName] = stats

	return stats
}

func (ds *DetailedStats) TakeSnapshot() AggregatedStats {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	snapshot := AggregatedStats{
		Timestamp: hutl.NowTime(),
		APIStats:  make(map[string]APISnapshot),
	}

	totalRequests := int64(0)
	totalSuccess := int64(0)
	//totalLatencyNs := int64(0)

	for apiName, stats := range ds.apiStats {
		total := stats.TotalCount.Load()
		success := stats.SuccessCount.Load()
		failure := stats.FailureCount.Load()
		minLatencyNs := stats.MinLatency.Load()
		maxLatencyNs := stats.MaxLatency.Load()
		totalLatencyNs := stats.TotalLatency.Load()

		// 计算当前TPS（基于最近1秒窗口）
		windowCount := stats.windowCount.Load()
		currentTPS := float64(windowCount) // 每秒请求数

		// 计算成功率
		successRate := 0.0
		if total > 0 {
			successRate = float64(success) / float64(total) * 100
		}

		// 计算平均延迟（单位：毫秒）
		avgLatencyMs := 0.0
		if total > 0 {
			avgLatencyMs = float64(totalLatencyNs) / float64(total) / 1e6 // 纳秒转毫秒
		}

		// 转换最小延迟（单位：毫秒）
		minLatencyMs := 0.0
		if minLatencyNs > 0 {
			minLatencyMs = float64(minLatencyNs) / 1e6
		}

		// 转换最大延迟（单位：毫秒）
		maxLatencyMs := 0.0
		if maxLatencyNs > 0 {
			maxLatencyMs = float64(maxLatencyNs) / 1e6
		}

		apiSnapshot := APISnapshot{
			Name:        apiName,
			Total:       total,
			Success:     success,
			Failure:     failure,
			SuccessRate: successRate,
			MinLatency:  minLatencyMs,
			MaxLatency:  maxLatencyMs,
			AvgLatency:  avgLatencyMs,
			CurrentTPS:  currentTPS,
		}

		snapshot.APIStats[apiName] = apiSnapshot

		totalRequests += total
		totalSuccess += success
	}

	// 计算总体统计
	if totalRequests > 0 {
		snapshot.SuccessRate = float64(totalSuccess) / float64(totalRequests) * 100
	}

	// 添加到历史记录
	ds.historyMu.Lock()
	ds.history = append(ds.history, snapshot)
	// 保留最近1小时数据
	if len(ds.history) > 3600 {
		ds.history = ds.history[len(ds.history)-3600:]
	}
	ds.historyMu.Unlock()

	return snapshot
}

func (ds *DetailedStats) GetHistoricalData() []AggregatedStats {
	ds.historyMu.RLock()
	defer ds.historyMu.RUnlock()

	data := make([]AggregatedStats, len(ds.history))
	copy(data, ds.history)

	return data
}

func (ds *DetailedStats) GetAPIStats() map[string]APISnapshot {
	snapshot := ds.TakeSnapshot()
	return snapshot.APIStats
}

func (ds *DetailedStats) Reset() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.apiStats = make(map[string]*APIDetailedStats)

	ds.historyMu.Lock()
	ds.history = make([]AggregatedStats, 0, 3600)
	ds.historyMu.Unlock()
}
