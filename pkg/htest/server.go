package htest

import (
	"context"
	"embed"
	"net/http"
	"time"
)

//go:embed static/*
var uiFS embed.FS

type Server struct {
	engine *Engine
	server *http.Server
	router *http.ServeMux
}

func NewServer(engine *Engine, addr string) *Server {
	s := &Server{
		engine: engine,
	}
	s.router = http.NewServeMux()
	s.setupRoutes()

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return s
}

func (s *Server) setupRoutes() {
	s.router.Handle("/", http.FileServer(http.FS(uiFS)))
	s.router.HandleFunc("/api/start", s.startTest)
	s.router.HandleFunc("/api/stop", s.stopTest)
	s.router.HandleFunc("/api/status", s.getStatus)
	s.router.HandleFunc("/api/distribution", s.getDistribution)
	s.router.HandleFunc("/api/detailed-stats", s.getDetailedStats)
	s.router.HandleFunc("/api/reset", s.resetStats)
	s.router.HandleFunc("/ws", s.handleWebSocket)
	s.router.HandleFunc("/api/list", s.apiList)
}

func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
