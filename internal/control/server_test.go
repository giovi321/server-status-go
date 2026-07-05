package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
