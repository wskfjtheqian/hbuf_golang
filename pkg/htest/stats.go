package htest

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

type Statistics struct {
	totalSuccess atomic.Int64
	totalFailure atomic.Int64
	totalLatency atomic.Int64
	requestCount atomic.Int64

	// API分布统计
	apiStats   map[string]*apiStat
	apiStatsMu sync.RWMutex

	// 滑动窗口统计
	windowSuccess atomic.Int64
	windowFailure atomic.Int64
	windowStart   atomic.Int64
}

type apiStat struct {
	success atomic.Int64
	failure atomic.Int64
}

func NewStatistics() *Statistics {
	return &Statistics{
		apiStats:    make(map[string]*apiStat),
		windowStart: atomic.Int64{},
	}
}

func (s *Statistics) Reset() {
	s.totalSuccess.Store(0)
	s.totalFailure.Store(0)
	s.totalLatency.Store(0)
	s.requestCount.Store(0)
	s.windowSuccess.Store(0)
	s.windowFailure.Store(0)
	s.windowStart.Store(htime.NowTime().Unix())

	s.apiStatsMu.Lock()
	s.apiStats = make(map[string]*apiStat)
	s.apiStatsMu.Unlock()
}

func (s *Statistics) RecordSuccess(apiName string, latency time.Duration) {
	s.totalSuccess.Add(1)
	s.totalLatency.Add(int64(latency))
	s.requestCount.Add(1)
	s.windowSuccess.Add(1)

	s.recordAPIStat(apiName, true)
}

func (s *Statistics) RecordFailure(apiName string, latency time.Duration) {
	s.totalFailure.Add(1)
	s.totalLatency.Add(int64(latency))
	s.requestCount.Add(1)
	s.windowFailure.Add(1)

	s.recordAPIStat(apiName, false)
}

func (s *Statistics) recordAPIStat(apiName string, success bool) {
	s.apiStatsMu.Lock()
	defer s.apiStatsMu.Unlock()

	stat, exists := s.apiStats[apiName]
	if !exists {
		stat = &apiStat{}
		s.apiStats[apiName] = stat
	}

	if success {
		stat.success.Add(1)
	} else {
		stat.failure.Add(1)
	}
}

type StatsSnapshot struct {
	TotalSuccess int64
	TotalFailure int64
	TotalLatency time.Duration
	AvgLatency   time.Duration
	TPS          float64
}

func (s *Statistics) GetSnapshot() StatsSnapshot {
	now := htime.NowTime().Unix()
	windowStart := s.windowStart.Load()

	// 每秒重置窗口
	if now > windowStart {
		s.windowStart.Store(now)
		s.windowSuccess.Store(0)
		s.windowFailure.Store(0)
	}

	total := s.requestCount.Load()
	avgLatency := time.Duration(0)
	if total > 0 {
		avgLatency = time.Duration(s.totalLatency.Load() / total)
	}

	return StatsSnapshot{
		TotalSuccess: s.totalSuccess.Load(),
		TotalFailure: s.totalFailure.Load(),
		TotalLatency: time.Duration(s.totalLatency.Load()),
		AvgLatency:   avgLatency,
		TPS:          float64(s.windowSuccess.Load() + s.windowFailure.Load()),
	}
}

func (s *Statistics) GetAPIDistribution() map[string]int64 {
	distribution := make(map[string]int64)

	s.apiStatsMu.RLock()
	defer s.apiStatsMu.RUnlock()

	for apiName, stat := range s.apiStats {
		distribution[apiName] = stat.success.Load() + stat.failure.Load()
	}

	return distribution
}
