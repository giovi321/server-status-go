# server-status webhook and HTTP control (Plan 07) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver MQTT/webhook parity: a webhook sink that POSTs the same normalized snapshot MQTT publishes (so n8n can consume it unchanged), plus a read-only HTTP control surface (`/health`, `/snapshot`) for pull-based consumers. Wire multiple sinks so the agent publishes to any combination.

**Architecture:** The webhook sink implements the existing `sink.Sink` interface and POSTs `json.Marshal(snapshot)` to a URL. Parity is structural: both the MQTT and webhook sinks consume the identical `model.Snapshot`, so a metric added anywhere appears in both. `model` types gain snake_case JSON tags (also cleaning up `--dump-detected`). A small `internal/control` HTTP server holds the latest snapshot for `/snapshot` pulls. `main` is refactored from single-sink to a sink list plus the optional control server. Control COMMANDS (refresh/update/restart) and self-update are deferred to Plan 08.

**Tech Stack:** Go 1.22+ stdlib (`net/http`, `encoding/json`, `net/http/httptest` for tests). No new dependencies.

## Global Constraints

- Build/test only in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows with the two-line `giovi321` / `Claude-Session:` footer.
- `gofmt -w` new/changed Go files before committing; `gofmt -l .` stays empty.
- Webhook payload = `json.Marshal(snapshot)` with snake_case keys: `{"device":{"node","name","identifier","parent","model","manufacturer","sw_version",...},"ts":<rfc3339>,"metrics":[{"key","component","instance","name","value","unit","device_class","state_class","kind","category",...}]}`.
- Parity is a hard requirement: the webhook sink and the MQTT sink both iterate `snap.Metrics` — do NOT filter or transform metrics differently between them. A golden/parity test asserts the webhook payload's metric set equals the snapshot's.
- Non-regression: the MQTT sink and all existing discovery output are unchanged. Adding JSON tags to `model` must not alter `ha.Discovery` output (it has its own payload struct) — the existing `ha` golden tests must stay green.
- HTTP control is READ-ONLY here (`/health`, `/snapshot`); `POST /command` and MQTT command topics come in Plan 08. `/snapshot` requires the bearer token when one is configured; `/health` is public.
- Secrets (`token`) support `${ENV}` interpolation (already handled by `config.ExpandEnv`).

## Prerequisites

- Plans 01-06 complete on `main` (repo giovi321/server-status-go). `sink.Sink`, `config.SinkConfig`, `config.Load`, `model.Snapshot`, and the `main` run loop exist.

## File structure

```
internal/model/metric.go        # MODIFY: add snake_case json tags to Metric/Device/Snapshot
internal/model/metric_test.go   # MODIFY: json-shape test
internal/config/config.go       # MODIFY: SinkConfig webhook fields; Control/HTTP config
internal/config/config_test.go  # MODIFY: webhook + control config test
internal/sink/webhook.go        # CREATE: Webhook sink
internal/sink/webhook_test.go   # CREATE: httptest + parity test
internal/control/server.go      # CREATE: read-only HTTP control server
internal/control/server_test.go # CREATE: httptest test
cmd/server-status/main.go       # MODIFY: multi-sink + control server wiring
```

---

### Task 1: JSON tags on model + config fields

**Files:**
- Modify: `internal/model/metric.go`, `internal/model/metric_test.go`, `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces: snake_case json tags on `model.Metric`/`Device`/`Snapshot`; `config.SinkConfig` gains `URL`, `Token`, `OnChange`; `config.Config` gains `Control config.ControlConfig`; new `config.ControlConfig{HTTP config.HTTPConfig}` and `config.HTTPConfig{Enabled bool; Bind string; Port int; Token string}`

- [ ] **Step 1: Add json tags to model**

In `internal/model/metric.go`, add json tags to the structs (keep field names/types/order):
```go
type Metric struct {
	Key           string `json:"key"`
	Component     string `json:"component,omitempty"`
	ComponentName string `json:"component_name,omitempty"`
	Instance      string `json:"instance,omitempty"`
	Name          string `json:"name"`
	Value         any    `json:"value"`
	Unit          string `json:"unit,omitempty"`
	DeviceClass   string `json:"device_class,omitempty"`
	StateClass    string `json:"state_class,omitempty"`
	Kind          Kind   `json:"kind"`
	Category      string `json:"category,omitempty"`
	Icon          string `json:"icon,omitempty"`
}

type Device struct {
	Node         string `json:"node"`
	Name         string `json:"name"`
	Identifier   string `json:"identifier"`
	Parent       string `json:"parent,omitempty"`
	Hierarchy    string `json:"hierarchy,omitempty"`
	Model        string `json:"model,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	SWVersion    string `json:"sw_version,omitempty"`
}

type Snapshot struct {
	Device  Device    `json:"device"`
	TS      time.Time `json:"ts"`
	Metrics []Metric  `json:"metrics"`
}
```

- [ ] **Step 2: Write failing model json test**

Append to `internal/model/metric_test.go`:
```go
import "encoding/json" // add to the import block if not present

func TestSnapshotJSONSnakeCase(t *testing.T) {
	snap := Snapshot{
		Device:  Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr", SWVersion: "dev"},
		Metrics: []Metric{{Key: "cpu_usage", Name: "CPU usage", Value: 5, Unit: "%", DeviceClass: "", StateClass: "measurement", Kind: KindSensor}},
	}
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"device"`, `"node":"gc01srvr"`, `"sw_version":"dev"`, `"metrics"`, `"key":"cpu_usage"`, `"state_class":"measurement"`} {
		if !contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	// omitempty: empty device_class must be absent
	if contains(s, `"device_class"`) {
		t.Fatalf("empty device_class should be omitted: %s", s)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (stringIndex(s, sub) >= 0) }
func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```
(If a `contains`/`stringIndex` helper already exists in the test package, reuse `strings.Contains` instead and import `strings`.)

- [ ] **Step 3: Add config fields**

In `internal/config/config.go`, add to `SinkConfig`:
```go
	URL      string `yaml:"url"`
	Token    string `yaml:"token"`
	OnChange bool   `yaml:"on_change"`
```
and add to `Config`:
```go
	Control ControlConfig `yaml:"control"`
```
and define:
```go
// ControlConfig configures the inbound control surface.
type ControlConfig struct {
	HTTP HTTPConfig `yaml:"http"`
}

// HTTPConfig configures the read-only HTTP control server.
type HTTPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Bind    string `yaml:"bind"`
	Port    int    `yaml:"port"`
	Token   string `yaml:"token"`
}
```

- [ ] **Step 4: Config test**

Create `internal/config/testdata/webhook.yaml`:
```yaml
node: gc01srvr
sinks:
  - type: mqtt
    host: 192.168.1.65
    password: ${TEST_MQTT_PASSWORD}
  - type: webhook
    url: https://n8n.example/webhook/ss
    token: ${TEST_MQTT_PASSWORD}
    on_change: true
control:
  http:
    enabled: true
    bind: 0.0.0.0
    port: 9971
    token: ${TEST_MQTT_PASSWORD}
```
Append to `internal/config/config_test.go`:
```go
func TestLoadWebhookAndControl(t *testing.T) {
	t.Setenv("TEST_MQTT_PASSWORD", "s3cret")
	cfg, err := Load("testdata/webhook.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sinks) != 2 {
		t.Fatalf("sinks: %d", len(cfg.Sinks))
	}
	wh := cfg.Sinks[1]
	if wh.Type != "webhook" || wh.URL != "https://n8n.example/webhook/ss" || wh.Token != "s3cret" || !wh.OnChange {
		t.Fatalf("webhook: %+v", wh)
	}
	if !cfg.Control.HTTP.Enabled || cfg.Control.HTTP.Port != 9971 || cfg.Control.HTTP.Token != "s3cret" {
		t.Fatalf("control http: %+v", cfg.Control.HTTP)
	}
}
```

- [ ] **Step 5: gofmt, run, verify — and confirm ha golden tests still pass**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ && go build ./... && go test ./internal/model/ ./internal/config/ ./internal/ha/ && go vet ./...'`
Expected: PASS. Critically, `internal/ha` golden tests must still pass (adding model json tags does not change `ha.Discovery`, which marshals its own struct).

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/model/ internal/config/ && git commit -F - <<'EOF'
feat: snake_case model JSON tags and webhook/control config

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: Webhook sink

**Files:**
- Create: `internal/sink/webhook.go`, `internal/sink/webhook_test.go`

**Interfaces:**
- Consumes: `sink.Sink`, `config.SinkConfig`, `model.Snapshot`
- Produces: `sink.NewWebhook(sc config.SinkConfig) *sink.Webhook` implementing `Sink` (Connect no-op; Publish POSTs `json.Marshal(snap)` with a Bearer token and retry; Close no-op; `on_change` skips a POST when the metrics are unchanged)

- [ ] **Step 1: Write the failing test (httptest + parity)**

Create `internal/sink/webhook_test.go`:
```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/sink/ -run Webhook'`
Expected: FAIL, `undefined: NewWebhook`.

- [ ] **Step 3: Implement**

Create `internal/sink/webhook.go`:
```go
package sink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

// Webhook POSTs the normalized snapshot JSON to a URL. It consumes the same
// model.Snapshot as the MQTT sink, so the two are metric-for-metric identical.
type Webhook struct {
	sc          config.SinkConfig
	client      *http.Client
	lastMetrics string
}

// NewWebhook builds a webhook sink.
func NewWebhook(sc config.SinkConfig) *Webhook {
	return &Webhook{sc: sc, client: &http.Client{Timeout: 10 * time.Second}}
}

// Connect is a no-op; the webhook sink is connectionless.
func (w *Webhook) Connect() error { return nil }

// Publish POSTs the snapshot as JSON with retry. With on_change it skips a POST
// when the metrics are unchanged since the last publish (ignoring the timestamp).
func (w *Webhook) Publish(snap model.Snapshot) error {
	if w.sc.OnChange {
		mj, _ := json.Marshal(snap.Metrics)
		if string(mj) == w.lastMetrics {
			return nil
		}
		w.lastMetrics = string(mj)
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodPost, w.sc.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if w.sc.Token != "" {
			req.Header.Set("Authorization", "Bearer "+w.sc.Token)
		}
		resp, err := w.client.Do(req)
		if err == nil {
			code := resp.StatusCode
			resp.Body.Close()
			if code < 300 {
				return nil
			}
			lastErr = fmt.Errorf("webhook POST to %s returned %d", w.sc.URL, code)
		} else {
			lastErr = err
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	return lastErr
}

// Close is a no-op.
func (w *Webhook) Close() error { return nil }
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/sink/webhook.go && go build ./... && go test ./internal/sink/ -run Webhook && go vet ./...'`
Expected: build clean, PASS (both webhook tests, including on_change).

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/sink/webhook.go internal/sink/webhook_test.go && git commit -F - <<'EOF'
feat: webhook sink (snapshot POST, on_change, MQTT parity)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 3: HTTP control server (read-only)

**Files:**
- Create: `internal/control/server.go`, `internal/control/server_test.go`

**Interfaces:**
- Produces: `control.NewServer(cfg config.HTTPConfig, version string) *control.Server`; `(*Server).Update(snap model.Snapshot)` (thread-safe latest-snapshot store); `(*Server).Handler() http.Handler` (`GET /health` public, `GET /snapshot` token-gated); `(*Server).Start() error` (starts the listener in a goroutine)

- [ ] **Step 1: Write the failing test**

Create `internal/control/server_test.go`:
```go
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
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/control/ -run HealthAndSnapshot'`
Expected: FAIL, `undefined: NewServer` (package doesn't exist yet).

- [ ] **Step 3: Implement**

Create `internal/control/server.go`:
```go
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
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/control/server.go && go build ./... && go test ./internal/control/ && go vet ./...'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/control/ && git commit -F - <<'EOF'
feat: read-only HTTP control server (health, snapshot)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 4: Wire multi-sink and control server; gate; live validation

**Files:**
- Modify: `cmd/server-status/main.go`

**Interfaces:**
- Consumes: `sink.NewMQTT`, `sink.NewWebhook`, `control.NewServer`
- Produces: the run loop builds ALL configured sinks, publishes each snapshot to all of them and to the control server, and starts the HTTP control server when enabled

- [ ] **Step 1: Refactor main to multiple sinks + control**

In `cmd/server-status/main.go`, replace the single-sink selection with a sink list. Read the current file, then:
- Build a `[]sink.Sink` from `cfg.Sinks`: for each, `case "mqtt": sink.NewMQTT(sc, dev)`, `case "webhook": sink.NewWebhook(sc)`. Fatal only if the list is empty.
- `Connect()` each sink; `defer` `Close()` on each. A sink whose `Connect` fails is logged and dropped (do not fatal on a webhook/one-sink failure), but if ALL sinks fail, fatal.
- If `cfg.Control.HTTP.Enabled`, `ctrl := control.NewServer(cfg.Control.HTTP, version.Version); ctrl.Start()`.
- In `cycle()`: build the snapshot once, `for _, sk := range sinks { if err := sk.Publish(snap); err != nil { log.Printf("publish: %v", err) } }`, and if `ctrl != nil` `ctrl.Update(snap)`.
The exact refactor (adapt to the current file's variable names):
```go
	var sinks []sink.Sink
	for _, sc := range cfg.Sinks {
		switch sc.Type {
		case "mqtt":
			sinks = append(sinks, sink.NewMQTT(sc, dev))
		case "webhook":
			sinks = append(sinks, sink.NewWebhook(sc))
		default:
			log.Printf("unknown sink type %q, skipping", sc.Type)
		}
	}
	if len(sinks) == 0 {
		log.Fatal("no usable sinks configured")
	}
	connected := sinks[:0]
	for _, sk := range sinks {
		if err := sk.Connect(); err != nil {
			log.Printf("sink connect failed, dropping: %v", err)
			continue
		}
		connected = append(connected, sk)
	}
	sinks = connected
	if len(sinks) == 0 {
		log.Fatal("all sinks failed to connect")
	}
	defer func() {
		for _, sk := range sinks {
			_ = sk.Close()
		}
	}()

	var ctrl *control.Server
	if cfg.Control.HTTP.Enabled {
		ctrl = control.NewServer(cfg.Control.HTTP, version.Version)
		_ = ctrl.Start()
	}

	cycle := func() {
		snap := detect.Snapshot(ctx, dev, cols)
		for _, sk := range sinks {
			if err := sk.Publish(snap); err != nil {
				log.Printf("publish: %v", err)
			}
		}
		if ctrl != nil {
			ctrl.Update(snap)
		}
	}
```
Add the `control` import. Keep `--dump-detected`, `--once`, `--version`, and the ticker loop as they are.

- [ ] **Step 2: Build + full gate**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w cmd/ internal/ && go build ./... && go vet ./... && go test ./... && gofmt -l .'`
Expected: build/vet clean, all tests pass, gofmt empty.

- [ ] **Step 3: Live validation on WSL (no hardware needed)**

This validates the whole outbound-parity + HTTP surface end to end using a throwaway local webhook listener:
```bash
wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go build -o server-status ./cmd/server-status && \
  ( { printf "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"; } | nc -l -p 18080 -q1 > /tmp/webhook_body.txt 2>/dev/null & ) ; \
  printf "node: whtest\nsinks:\n  - type: webhook\n    url: http://127.0.0.1:18080/hook\ncontrol:\n  http:\n    enabled: true\n    bind: 127.0.0.1\n    port: 18081\n" > /tmp/ss.yaml ; \
  ( ./server-status -c /tmp/ss.yaml --interval 2 & echo $! > /tmp/ss.pid ) ; sleep 4 ; \
  echo "=== GET /health ===" ; curl -s http://127.0.0.1:18081/health ; echo ; \
  echo "=== GET /snapshot (first 200 chars) ===" ; curl -s http://127.0.0.1:18081/snapshot | head -c 200 ; echo ; \
  echo "=== webhook received body (first 200 chars) ===" ; head -c 200 /tmp/webhook_body.txt ; echo ; \
  kill "$(cat /tmp/ss.pid)" 2>/dev/null ; rm -f /tmp/ss.yaml /tmp/ss.pid /tmp/webhook_body.txt server-status'
```
Expected: `/health` returns `{"status":"ok","version":"dev"}`; `/snapshot` returns a JSON snapshot with `"device"` and `"metrics"` (snake_case keys); the webhook listener captured a POSTed snapshot body. If `nc` is unavailable or the listener race is flaky, at minimum confirm `/health` and `/snapshot` respond correctly (that alone proves the HTTP surface and the running loop). Capture the output.

- [ ] **Step 4: Commit**

```bash
cd "Z:/git/server-status" && git add cmd/server-status/main.go && git commit -F - <<'EOF'
feat: publish to all configured sinks and start the HTTP control server

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

## Self-review against the spec

Spec Phase 6 (webhook + HTTP control, parity): webhook sink → Task 2; read-only HTTP control (health/snapshot) → Task 3; multi-sink wiring → Task 4; parity → the `TestWebhookPublishPostsSnapshot` parity assertion plus the structural fact that both sinks consume the same `model.Snapshot`.

Deferred to Plan 08 (control commands + self-update): `POST /command/{name}`, MQTT command topics, HA button/update entities, the command dispatcher. Documented in the roadmap.

JSON tags: adding snake_case tags to `model` gives n8n a clean payload and also cleans up `--dump-detected`; the `ha` package marshals its own discovery struct, so discovery output is unchanged (asserted by keeping the `ha` golden tests green in Task 1).

Live-validatable: unlike Plans 04-06, Task 3's HTTP surface and the webhook POST run fully on WSL (no hardware), so this plan is validated end to end.

Placeholder scan: every code and test step contains complete content.

## Roadmap: subsequent plans

- Plan 08: control commands (refresh/update/restart) over MQTT command topics + `POST /command`, the command dispatcher, GitHub-Releases self-update, and the HA update entity/buttons
- Plan 09: reliability hardening (advanced reconnect/cached replay already partly present, systemd watchdog, per-collector isolation/cadence, uninstall purge) + migration cutover
- Optional: btrfs collector
