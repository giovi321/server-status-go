package sink

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

func testSnapshot() model.Snapshot {
	return model.Snapshot{
		Device:  model.Device{Node: "n", Name: "n", Identifier: "server-status-n"},
		TS:      time.Unix(1700000000, 0).UTC(),
		Metrics: []model.Metric{{Key: "cpu_usage", Name: "CPU usage", Value: 5, Unit: "%", Kind: model.KindSensor}, {Key: "uptime", Name: "Uptime", Value: 2, Unit: "d", Kind: model.KindSensor}},
	}
}

func TestWebhookPublishPostsSnapshot(t *testing.T) {
	var mu sync.Mutex
	var gotBody []byte
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := NewWebhook(config.SinkConfig{Type: "webhook", URL: srv.URL, Token: "tok"})
	if err := wh.Publish(testSnapshot()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth: %q", gotAuth)
	}
	// Parity: the webhook payload's metric keys equal the snapshot's metric keys.
	var payload model.Snapshot
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("payload not valid snapshot json: %v; body=%s", err, gotBody)
	}
	if len(payload.Metrics) != 2 || payload.Metrics[0].Key != "cpu_usage" || payload.Metrics[1].Key != "uptime" {
		t.Fatalf("parity: webhook metrics differ from snapshot: %+v", payload.Metrics)
	}
	if payload.Device.Identifier != "server-status-n" {
		t.Fatalf("device: %+v", payload.Device)
	}
	if payload.Metrics[0].Unit != "%" || payload.Metrics[0].Name != "CPU usage" {
		t.Fatalf("parity: metric fields not preserved: %+v", payload.Metrics[0])
	}
}

func TestWebhookOnChangeSkipsUnchanged(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	wh := NewWebhook(config.SinkConfig{Type: "webhook", URL: srv.URL, OnChange: true})
	snap := testSnapshot()
	_ = wh.Publish(snap)
	_ = wh.Publish(snap) // identical metrics -> skipped
	changed := testSnapshot()
	changed.Metrics[0].Value = 99
	_ = wh.Publish(changed) // changed -> posted
	mu.Lock()
	defer mu.Unlock()
	if posts != 2 {
		t.Fatalf("on_change: expected 2 posts (first + changed), got %d", posts)
	}
}

func TestWebhookOnChangeResendsAfterFailedDelivery(t *testing.T) {
	var mu sync.Mutex
	fail := true
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		f := fail
		mu.Unlock()
		if f {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := NewWebhook(config.SinkConfig{Type: "webhook", URL: srv.URL, OnChange: true})
	snap := testSnapshot()
	if err := wh.Publish(snap); err == nil {
		t.Fatal("expected an error when the endpoint returns 500")
	}
	// Endpoint recovers. The SAME metrics MUST be re-sent (not skipped by on_change),
	// because the prior delivery failed and lastMetrics must not have been committed.
	mu.Lock()
	fail = false
	before := posts
	mu.Unlock()
	if err := wh.Publish(snap); err != nil {
		t.Fatalf("recovery publish failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts <= before {
		t.Fatal("on_change must re-send after a prior failed delivery (lastMetrics committed too early)")
	}
}
