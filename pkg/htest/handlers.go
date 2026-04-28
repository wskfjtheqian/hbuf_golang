package htest

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/wskfjtheqian/hbuf_golang/pkg/htime"
)

type StartRequest struct {
	Duration    string `json:"duration"`
	Concurrency int    `json:"concurrency"`
	RateLimit   int    `json:"rateLimit"`
}

func (s *Server) startTest(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	duration, err := time.ParseDuration(req.Duration)
	if err != nil {
		http.Error(w, "Invalid duration format", http.StatusBadRequest)
		return
	}

	config := StressConfig{
		Duration:    duration,
		Concurrency: req.Concurrency,
		RateLimit:   req.RateLimit,
	}

	if err := s.engine.Start(config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *Server) stopTest(w http.ResponseWriter, r *http.Request) {
	s.engine.Stop()
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"running": s.engine.IsRunning(),
	}
	json.NewEncoder(w).Encode(status)
}

func (s *Server) getDistribution(w http.ResponseWriter, r *http.Request) {
	distribution := s.engine.GetAPIDistribution()
	json.NewEncoder(w).Encode(distribution)
}

// 新增处理详细统计的接口
func (s *Server) getDetailedStats(w http.ResponseWriter, r *http.Request) {
	detailedStats := s.engine.GetDetailedStats()
	json.NewEncoder(w).Encode(detailedStats)
}

// 新增重置统计接口
func (s *Server) resetStats(w http.ResponseWriter, r *http.Request) {
	s.engine.ResetStats()
	json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
}

// apiList 获得所有API列表
func (s *Server) apiList(writer http.ResponseWriter, request *http.Request) {
	list := s.engine.GetApiList()
	err := json.NewEncoder(writer).Encode(list)
	if err != nil {
		log.Println(err)
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUpgradeRequired), http.StatusUpgradeRequired)
		return
	}
	defer conn.Close()

	// 发送初始数据
	detailedStats := s.engine.GetDetailedStats()
	apiDistribution := s.engine.GetAPIDistribution()

	buffer, err := json.Marshal(map[string]interface{}{
		"type":         "init",
		"data":         detailedStats,
		"distribution": apiDistribution,
		"timestamp":    htime.NowTime().Unix(),
	})
	if err != nil {
		return
	}
	err = ws.WriteFrame(conn, ws.NewBinaryFrame(buffer))
	if err != nil {
		return
	}

	// 监听结果通道
	resultsChan := s.engine.GetResultsChan()
	statsTicker := time.NewTicker(200 * time.Millisecond)
	defer statsTicker.Stop()

	// 保持连接活跃
	keepaliveTicker := time.NewTicker(10 * time.Second)
	defer keepaliveTicker.Stop()

	var lastResult TestResult
	lastSent := htime.NowTime()

	for {
		select {
		case result, ok := <-resultsChan:
			if !ok {
				return
			}

			lastResult = result

			// 只有当需要发送时才发送
			currentTime := htime.NowTime()
			if currentTime.Sub(lastSent) >= 200*time.Millisecond {
				s.sendResult(conn, result)
				lastSent = currentTime
			}

		case <-statsTicker.C:
			// 定期发送最新结果
			currentTime := htime.NowTime()
			if currentTime.Sub(lastSent) >= 200*time.Millisecond {
				s.sendResult(conn, lastResult)
				lastSent = currentTime
			}

		case <-keepaliveTicker.C:
			// 保持连接活跃
			err := ws.WriteFrame(conn, ws.NewPingFrame(nil))
			if err != nil {
				return
			}
		}
	}
}

// 发送结果的辅助函数
func (s *Server) sendResult(conn net.Conn, result TestResult) {
	// 获取最新统计
	detailedStats := s.engine.GetDetailedStats()
	apiDistribution := s.engine.GetAPIDistribution()

	// 确保时间戳是合理的（当前时间）
	timestamp := result.Timestamp.Unix()
	now := htime.NowTime().Unix()

	// 如果时间戳不合理的旧或未来时间，使用当前时间
	if timestamp <= 0 || timestamp > now+3600 || timestamp < now-3600 {
		timestamp = now
	}

	successRate := result.SuccessRate
	if successRate < 0 {
		successRate = 0
	} else if successRate > 100 {
		successRate = 100
	}

	data := map[string]interface{}{
		"type": "realtime",
		"realtime": map[string]interface{}{
			"timestamp":   timestamp, // 确保是合理的时间戳
			"success":     result.Success,
			"failure":     result.Failure,
			"total":       result.Total,
			"tps":         result.TPS,
			"avgLatency":  result.AvgLatency.Milliseconds(),
			"minLatency":  result.MinLatency.Milliseconds(),
			"maxLatency":  result.MaxLatency.Milliseconds(),
			"successRate": successRate,
		},
		"detailed":     detailedStats,
		"distribution": apiDistribution,
	}

	buffer, err := json.Marshal(data)
	if err != nil {
		log.Printf("WebSocket write error: %v", err)
	}
	err = ws.WriteFrame(conn, ws.NewBinaryFrame(buffer))
	if err != nil {
		return
	}
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./web/static/index.html")
}
