// Package control serves an HTTP surface (health, latest snapshot, and
// command dispatch) for the agent.
package control

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/giovi321/server-status/internal/command"
	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

// Server holds the latest snapshot and serves it over HTTP.
type Server struct {
	cfg     config.HTTPConfig
	version string
	mu      sync.RWMutex
	snap    *model.Snapshot
	disp    *command.Dispatcher
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

// SetDispatcher wires the command dispatcher for POST /command/{name}.
func (s *Server) SetDispatcher(d *command.Dispatcher) {
	s.mu.Lock()
	s.disp = d
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
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": s.version})
	})
	mux.HandleFunc("GET /snapshot", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST /command/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !s.authOK(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if s.cfg.Token == "" {
			http.Error(w, "commands require a control token", http.StatusForbidden)
			return
		}
		s.mu.RLock()
		d := s.disp
		s.mu.RUnlock()
		if d == nil {
			http.Error(w, "commands disabled", http.StatusServiceUnavailable)
			return
		}
		res := d.Run(r.Context(), r.PathValue("name"))
		w.Header().Set("Content-Type", "application/json")
		if !res.OK {
			w.WriteHeader(http.StatusBadRequest)
		}
		_ = json.NewEncoder(w).Encode(res)
	})
	return mux
}

// Start binds the listener synchronously (so a bind failure — e.g. the port is
// already in use — is returned to the caller) and serves in a background goroutine.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler()}
	go func() { _ = srv.Serve(ln) }()
	return nil
}
