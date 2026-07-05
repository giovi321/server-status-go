package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/giovi321/server-status/internal/command"
	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

func TestHealthAndSnapshot(t *testing.T) {
	s := NewServer(config.HTTPConfig{Token: "tok"}, "v1")
	h := s.Handler()

	// /health is public
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health: %d", rr.Code)
	}

	// /snapshot before any update -> 503
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/snapshot", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("snapshot before update: %d", rr.Code)
	}

	// /snapshot without token -> 401
	s.Update(model.Snapshot{Device: model.Device{Node: "n"}, TS: time.Now(), Metrics: []model.Metric{{Key: "cpu_usage", Value: 5, Kind: model.KindSensor}}})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/snapshot", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("snapshot no token: %d", rr.Code)
	}

	// /snapshot with token -> 200 + the snapshot
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/snapshot", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("snapshot with token: %d", rr.Code)
	}
	var snap model.Snapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatalf("snapshot body: %v", err)
	}
	if snap.Device.Node != "n" || len(snap.Metrics) != 1 {
		t.Fatalf("snapshot content: %+v", snap)
	}

	// method-scoped: POST /health is not allowed
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /health should be 405, got %d", rr.Code)
	}
}

func TestPostCommandDisabledWithoutDispatcher(t *testing.T) {
	s := NewServer(config.HTTPConfig{}, "v1")
	h := s.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/command/refresh", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /command without dispatcher: %d", rr.Code)
	}
}

func TestPostCommand(t *testing.T) {
	s := NewServer(config.HTTPConfig{Token: "tok"}, "v1")
	disp := command.New()
	disp.Register("refresh", func(context.Context) command.Result {
		return command.Result{OK: true, Message: "refreshed"}
	})
	s.SetDispatcher(disp)
	h := s.Handler()

	// no token -> 401
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/command/refresh", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("POST /command/refresh no token: %d", rr.Code)
	}

	// known command -> 200 {"ok":true,...}
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/command/refresh", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /command/refresh: %d", rr.Code)
	}
	var res command.Result
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("body: %v", err)
	}
	if !res.OK || res.Message != "refreshed" {
		t.Fatalf("result: %+v", res)
	}

	// unknown command -> 400
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/command/nope", nil)
	req.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST /command/nope: %d", rr.Code)
	}
}
