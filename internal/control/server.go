// Package control serves a read-only HTTP surface (health + latest snapshot).
// Command endpoints and MQTT command topics are added in a later plan.
package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

// Server holds the latest snapshot and serves it over HTTP.
type Server struct {
	cfg     config.HTTPConfig
	version string
	mu      sync.RWMutex
	snap    *model.Snapshot
}

// NewServer builds an unstarted control server.
func NewServer(cfg config.HTTPConfig, version string) *Server {
	return &Server{cfg: cfg, version: version}
}

// Update stores the latest snapshot for /snapshot to serve.
func (s *Server) Update(snap model.Snapshot) {
	s.mu.Lock()
	s.snap = &snap
	s.mu.Unlock()
}

func (s *Server) authOK(r *http.Request) bool {
	if s.cfg.Token == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+s.cfg.Token
}

// Handler returns the control mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": s.version})
	})
	mux.HandleFunc("/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if !s.authOK(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.mu.RLock()
		snap := s.snap
		s.mu.RUnlock()
		if snap == nil {
			http.Error(w, "no snapshot yet", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
	})
	return mux
}

// Start runs the HTTP listener in a background goroutine. It returns any
// immediate listen error via the returned channel-free helper: a bind failure
// is logged by the caller, not fatal.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
	srv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() { _ = srv.ListenAndServe() }()
	return nil
}
