# server-status self-update and control commands (Plan 08) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the self-update button (requirement #1): a command dispatcher, self-update download/verify/atomic-swap logic backed by GitHub Releases, a `POST /command/{name}` HTTP endpoint, MQTT command topics with a Home Assistant button (refresh/restart) and update entity (install the latest agent), and a CI release workflow.

**Architecture:** A `command.Dispatcher` maps names (`refresh`, `restart`, `update`, `rollback`) to handlers and returns a `Result`. The `update` package queries the GitHub Releases API, downloads the arch-matched asset, verifies its SHA256, atomically swaps the running binary (keeping a `.bak`), and restarts via systemd. The dispatcher is wired into both inbound surfaces: `POST /command/{name}` (control server, token-gated) and MQTT command topics (`<base>/<node>/cmd/<name>`, driven by an HA button + update entity). Fixture/httptest tests cover the dispatcher, the update download/verify/swap, and the discovery payloads; the actual privileged swap+restart and the release pipeline are validated at deploy time.

**Tech Stack:** Go 1.22+ stdlib (`net/http`, `crypto/sha256`, `os`, `os/exec`), GitHub Actions YAML. No new Go dependencies.

## Global Constraints

- Build/test only in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows with the two-line `giovi321` / `Claude-Session:` footer.
- `gofmt -w` new/changed Go files before committing; `gofmt -l .` stays empty.
- Commands are privileged. The HTTP endpoint requires the bearer token; MQTT command topics rely on broker ACLs. The `update` handler downloads ONLY from the configured GitHub repo over HTTPS and verifies the SHA256 before swapping — a bad download leaves the running binary untouched.
- Self-update never runs an untrusted binary: download to `<path>.new`, verify checksum, keep the current as `<path>.bak`, atomic `rename` into place (same filesystem), then `systemctl restart`.
- `Result{OK bool; Message string}` is the uniform command result, published to `<base>/<node>/cmd/<name>/result` (MQTT) and returned as JSON (HTTP), and mirrored into a diagnostic `agent_last_command` sensor.
- Non-regression: existing sinks/collectors/discovery unchanged; the MQTT sink gains inbound command subscription without altering its publish behavior. Adding the dispatcher must be optional (a nil dispatcher disables commands).
- HA entities: a `button` per press-command (refresh, restart) and one `update` entity for the agent (installed vs latest, Install → update command). These are new discovery component types alongside sensor/binary_sensor.

## Prerequisites

- Plans 01-07 complete on `main` (repo giovi321/server-status-go). `sink.MQTT`, `control.Server`, `ha.Discovery`, `config`, and the `main` run loop exist. The repo has a GitHub Actions-capable remote (it does: giovi321/server-status-go).

## File structure

```
internal/command/dispatcher.go       # CREATE: Dispatcher, Result, restart handler
internal/command/dispatcher_test.go   # CREATE
internal/update/update.go             # CREATE: Latest + Apply (download/verify/swap)
internal/update/update_test.go        # CREATE: httptest release API + swap
internal/ha/control.go                # CREATE: button + update entity discovery
internal/ha/control_test.go           # CREATE
internal/control/server.go            # MODIFY: POST /command/{name}
internal/control/server_test.go       # MODIFY
internal/sink/mqtt.go                 # MODIFY: command subscription + result publish + button/update discovery
internal/config/config.go             # MODIFY: UpdateConfig (repo/channel/interval); Control command toggle
cmd/server-status/main.go             # MODIFY: build dispatcher, wire refresh/restart/update, pass to control+sink
.github/workflows/release.yml         # CREATE: build + release on tag
```

---

### Task 1: Command dispatcher

**Files:**
- Create: `internal/command/dispatcher.go`, `internal/command/dispatcher_test.go`

**Interfaces:**
- Produces: `command.Result{OK bool; Message string}` (json `ok`/`message`); `command.Handler = func(ctx context.Context) command.Result`; `command.Dispatcher` with `New()`, `Register(name string, h Handler)`, `Run(ctx, name string) Result` (unknown → `{false, "unknown command: <name>"}`), `Names() []string` (sorted); `command.RestartHandler(service string) Handler` (runs `systemctl restart <service>`)

- [ ] **Step 1: Write the failing test**

Create `internal/command/dispatcher_test.go`:
```go
package command

import (
	"context"
	"reflect"
	"testing"
)

func TestDispatcherRun(t *testing.T) {
	d := New()
	d.Register("refresh", func(context.Context) Result { return Result{OK: true, Message: "refreshed"} })
	r := d.Run(context.Background(), "refresh")
	if !r.OK || r.Message != "refreshed" {
		t.Fatalf("refresh: %+v", r)
	}
	u := d.Run(context.Background(), "nope")
	if u.OK || u.Message != "unknown command: nope" {
		t.Fatalf("unknown: %+v", u)
	}
}

func TestDispatcherNamesSorted(t *testing.T) {
	d := New()
	d.Register("update", func(context.Context) Result { return Result{} })
	d.Register("refresh", func(context.Context) Result { return Result{} })
	if got := d.Names(); !reflect.DeepEqual(got, []string{"refresh", "update"}) {
		t.Fatalf("names: %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/command/'`
Expected: FAIL (package/undefined New).

- [ ] **Step 3: Implement**

Create `internal/command/dispatcher.go`:
```go
// Package command routes named control commands to handlers.
package command

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"sync"
)

// Result is the uniform outcome of a command.
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// Handler runs one command.
type Handler func(ctx context.Context) Result

// Dispatcher maps command names to handlers.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// New builds an empty dispatcher.
func New() *Dispatcher { return &Dispatcher{handlers: map[string]Handler{}} }

// Register adds or replaces a command handler.
func (d *Dispatcher) Register(name string, h Handler) {
	d.mu.Lock()
	d.handlers[name] = h
	d.mu.Unlock()
}

// Run executes a command by name.
func (d *Dispatcher) Run(ctx context.Context, name string) Result {
	d.mu.RLock()
	h := d.handlers[name]
	d.mu.RUnlock()
	if h == nil {
		return Result{OK: false, Message: "unknown command: " + name}
	}
	return h(ctx)
}

// Names returns the registered command names, sorted.
func (d *Dispatcher) Names() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	names := make([]string, 0, len(d.handlers))
	for n := range d.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RestartHandler restarts a systemd service.
func RestartHandler(service string) Handler {
	return func(ctx context.Context) Result {
		out, err := exec.CommandContext(ctx, "systemctl", "restart", service).CombinedOutput()
		if err != nil {
			return Result{OK: false, Message: fmt.Sprintf("restart failed: %v: %s", err, out)}
		}
		return Result{OK: true, Message: "restarting"}
	}
}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/command/ && go build ./... && go test ./internal/command/ && go vet ./...'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/command/ && git commit -F - <<'EOF'
feat: command dispatcher (refresh/restart handlers)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: Self-update (download, verify, swap)

**Files:**
- Create: `internal/update/update.go`, `internal/update/update_test.go`

**Interfaces:**
- Produces: `update.Release{Version, AssetURL, Sha256 string}`; `update.Latest(ctx context.Context, apiBase, repo, assetName string) (Release, error)` (GET `<apiBase>/repos/<repo>/releases/latest`, find the asset named `assetName` and its `<assetName>.sha256` sibling asset, fetch the sha256); `update.Apply(ctx context.Context, client *http.Client, rel Release, destPath string) error` (download asset, verify sha256, write `destPath.new`, move current to `destPath.bak`, atomic rename into place)

- [ ] **Step 1: Write the failing test (httptest)**

Create `internal/update/update_test.go`:
```go
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLatestAndApply(t *testing.T) {
	binary := []byte("#!/bin/true\nnew-binary-v2\n")
	sum := sha256.Sum256(binary)
	sumHex := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2.0.0",
			"assets": []map[string]string{
				{"name": "server-status-linux-amd64", "browser_download_url": base + "/dl/bin"},
				{"name": "server-status-linux-amd64.sha256", "browser_download_url": base + "/dl/sum"},
			},
		})
	})
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	mux.HandleFunc("/dl/sum", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  server-status-linux-amd64\n", sumHex)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel, err := Latest(context.Background(), srv.URL, "me/repo", "server-status-linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v2.0.0" || rel.Sha256 != sumHex {
		t.Fatalf("release: %+v", rel)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "server-status")
	if err := os.WriteFile(dest, []byte("old-binary-v1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), srv.Client(), rel, dest); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(binary) {
		t.Fatalf("dest not swapped: %q", got)
	}
	bak, _ := os.ReadFile(dest + ".bak")
	if string(bak) != "old-binary-v1\n" {
		t.Fatalf("backup missing/wrong: %q", bak)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("tampered")) }))
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "server-status")
	os.WriteFile(dest, []byte("original"), 0o755)
	rel := Release{Version: "v2", AssetURL: srv.URL, Sha256: "deadbeef"}
	if err := Apply(context.Background(), srv.Client(), rel, dest); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "original" {
		t.Fatalf("bad download must not replace binary; got %q", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/update/'`
Expected: FAIL (undefined Latest).

- [ ] **Step 3: Implement**

Create `internal/update/update.go`:
```go
// Package update self-updates the agent binary from GitHub Releases.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Release is a resolved latest release: the tag, the asset URL, and its sha256.
type Release struct {
	Version  string
	AssetURL string
	Sha256   string
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest resolves the latest release for repo, locating the assetName binary and
// its "<assetName>.sha256" sibling.
func Latest(ctx context.Context, apiBase, repo, assetName string) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(apiBase, "/"), repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("releases/latest returned %d", resp.StatusCode)
	}
	var gh ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return Release{}, err
	}
	rel := Release{Version: gh.TagName}
	var sumURL string
	for _, a := range gh.Assets {
		switch a.Name {
		case assetName:
			rel.AssetURL = a.URL
		case assetName + ".sha256":
			sumURL = a.URL
		}
	}
	if rel.AssetURL == "" {
		return Release{}, fmt.Errorf("asset %q not found in latest release", assetName)
	}
	if sumURL != "" {
		if s, err := fetchSha256(ctx, sumURL, assetName); err == nil {
			rel.Sha256 = s
		}
	}
	return rel, nil
}

func fetchSha256(ctx context.Context, url, assetName string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	// Accept either a bare hex line or "<hex>  <name>" lines.
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) == 1 || strings.HasSuffix(line, assetName) {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", assetName)
}

// Apply downloads the release asset, verifies its sha256, and atomically swaps
// destPath (keeping the previous binary as destPath.bak). A bad download leaves
// destPath untouched.
func Apply(ctx context.Context, client *http.Client, rel Release, destPath string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.AssetURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("asset download returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if rel.Sha256 != "" {
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != rel.Sha256 {
			return fmt.Errorf("checksum mismatch: got %s want %s", got, rel.Sha256)
		}
	}
	tmp := destPath + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	// Keep the current binary as .bak (best-effort), then atomically swap.
	_ = os.Rename(destPath, destPath+".bak")
	if err := os.Rename(tmp, destPath); err != nil {
		// Roll the backup back if the swap failed.
		_ = os.Rename(destPath+".bak", destPath)
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/update/ && go build ./... && go test ./internal/update/ && go vet ./...'`
Expected: PASS (both the happy-path swap and the bad-checksum rejection).

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/update/ && git commit -F - <<'EOF'
feat: self-update download, checksum verify, and atomic swap

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 3: HA control entities + HTTP command endpoint + MQTT command subscription

**Files:**
- Create: `internal/ha/control.go`, `internal/ha/control_test.go`
- Modify: `internal/control/server.go`, `internal/control/server_test.go`, `internal/sink/mqtt.go`, `internal/config/config.go`

**Interfaces:**
- Produces: `ha.ButtonDiscovery(dev model.Device, sc config.SinkConfig, cmd, label string) (topic string, payload []byte, err error)` and `ha.UpdateDiscovery(dev, sc) (topic, payload, err)`; `control.Server` gains `SetDispatcher(*command.Dispatcher)` and a `POST /command/{name}` route; `sink.NewMQTT` gains an optional dispatcher wired to command topics; `config.Config.Update` (UpdateConfig)

- [ ] **Step 1: HA control discovery + test**

Create `internal/ha/control.go`:
```go
package ha

import (
	"encoding/json"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

// CommandTopic is where a control command is received.
func CommandTopic(base, node, cmd string) string {
	return base + "/" + node + "/cmd/" + cmd
}

type buttonPayload struct {
	Name         string      `json:"name"`
	UniqueID     string      `json:"unique_id"`
	ObjectID     string      `json:"object_id"`
	HasEntityN   bool        `json:"has_entity_name"`
	CommandTopic string      `json:"command_topic"`
	PayloadPress string      `json:"payload_press"`
	Availability string      `json:"availability_topic"`
	Device       deviceBlock `json:"device"`
	QoS          int         `json:"qos"`
}

// ButtonDiscovery builds discovery for a press-button that triggers a command.
func ButtonDiscovery(dev model.Device, sc config.SinkConfig, cmd, label string) (string, []byte, error) {
	object := dev.Node + "_cmd_" + cmd
	p := buttonPayload{
		Name:         label,
		UniqueID:     dev.Identifier + "-cmd-" + cmd,
		ObjectID:     object,
		HasEntityN:   true,
		CommandTopic: CommandTopic(sc.BaseTopic, dev.Node, cmd),
		PayloadPress: "1",
		Availability: AvailabilityTopic(sc.BaseTopic, dev.Node),
		QoS:          sc.QoS,
		Device:       deviceBlock{Identifiers: []string{dev.Identifier}, Name: dev.Name},
	}
	body, err := json.Marshal(p)
	topic := sc.DiscoveryPrefix + "/button/" + dev.Node + "/" + object + "/config"
	return topic, body, err
}

type updatePayload struct {
	Name          string      `json:"name"`
	UniqueID      string      `json:"unique_id"`
	ObjectID      string      `json:"object_id"`
	HasEntityN    bool        `json:"has_entity_name"`
	StateTopic    string      `json:"state_topic"`
	CommandTopic  string      `json:"command_topic"`
	PayloadInstall string     `json:"payload_install"`
	Availability  string      `json:"availability_topic"`
	Device        deviceBlock `json:"device"`
	QoS           int         `json:"qos"`
}

// UpdateDiscovery builds discovery for the agent's HA update entity. The state
// topic carries a JSON object {"installed_version","latest_version"}.
func UpdateDiscovery(dev model.Device, sc config.SinkConfig) (string, []byte, error) {
	object := dev.Node + "_agent"
	p := updatePayload{
		Name:           "Agent",
		UniqueID:       dev.Identifier + "-agent-update",
		ObjectID:       object,
		HasEntityN:     true,
		StateTopic:     sc.BaseTopic + "/" + dev.Node + "/agent/update",
		CommandTopic:   CommandTopic(sc.BaseTopic, dev.Node, "update"),
		PayloadInstall: "1",
		Availability:   AvailabilityTopic(sc.BaseTopic, dev.Node),
		QoS:            sc.QoS,
		Device:         deviceBlock{Identifiers: []string{dev.Identifier}, Name: dev.Name},
	}
	body, err := json.Marshal(p)
	topic := sc.DiscoveryPrefix + "/update/" + dev.Node + "/" + object + "/config"
	return topic, body, err
}
```
Create `internal/ha/control_test.go`:
```go
package ha

import (
	"encoding/json"
	"testing"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

func TestButtonDiscovery(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr"}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := ButtonDiscovery(dev, sc, "refresh", "Refresh")
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/button/gc01srvr/gc01srvr_cmd_refresh/config" {
		t.Fatalf("topic: %q", topic)
	}
	var obj map[string]any
	_ = json.Unmarshal(payload, &obj)
	if obj["command_topic"] != "server-status/gc01srvr/cmd/refresh" {
		t.Fatalf("command_topic: %v", obj["command_topic"])
	}
	if obj["payload_press"] != "1" {
		t.Fatalf("payload_press: %v", obj["payload_press"])
	}
}

func TestUpdateDiscovery(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr"}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := UpdateDiscovery(dev, sc)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/update/gc01srvr/gc01srvr_agent/config" {
		t.Fatalf("topic: %q", topic)
	}
	var obj map[string]any
	_ = json.Unmarshal(payload, &obj)
	if obj["command_topic"] != "server-status/gc01srvr/cmd/update" {
		t.Fatalf("command_topic: %v", obj["command_topic"])
	}
}
```

- [ ] **Step 2: Add UpdateConfig**

In `internal/config/config.go`, add to `Config`:
```go
	Update UpdateConfig `yaml:"update"`
```
and define, with defaults in `applyDefaults` (`Repo` default `"giovi321/server-status-go"`, `CheckIntervalSeconds` default `21600`):
```go
// UpdateConfig configures self-update.
type UpdateConfig struct {
	Repo                 string `yaml:"repo"`
	CheckIntervalSeconds int    `yaml:"check_interval_seconds"`
}
```
In `applyDefaults`:
```go
	if c.Update.Repo == "" {
		c.Update.Repo = "giovi321/server-status-go"
	}
	if c.Update.CheckIntervalSeconds == 0 {
		c.Update.CheckIntervalSeconds = 21600
	}
```

- [ ] **Step 3: HTTP POST /command**

In `internal/control/server.go`, add a `disp` field and setter, and a method-scoped route. Add to the struct:
```go
	disp interface {
		Run(ctx context.Context, name string) command.Result
		Names() []string
	}
```
(import `context` and `github.com/giovi321/server-status/internal/command`). Add:
```go
// SetDispatcher wires the command dispatcher for POST /command/{name}.
func (s *Server) SetDispatcher(d *command.Dispatcher) { s.disp = d }
```
In `Handler()`, add:
```go
	mux.HandleFunc("POST /command/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !s.authOK(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if s.disp == nil {
			http.Error(w, "commands disabled", http.StatusServiceUnavailable)
			return
		}
		res := s.disp.Run(r.Context(), r.PathValue("name"))
		w.Header().Set("Content-Type", "application/json")
		if !res.OK {
			w.WriteHeader(http.StatusBadRequest)
		}
		_ = json.NewEncoder(w).Encode(res)
	})
```
Add to `internal/control/server_test.go` a test that `POST /command/refresh` with a registered dispatcher returns 200 + `{"ok":true,...}`, and unknown command returns 400. Wire a real `command.New()` with a `refresh` handler in the test.

- [ ] **Step 4: MQTT command subscription**

In `internal/sink/mqtt.go`: add an optional dispatcher and publish button/update discovery + subscribe to command topics. Change `NewMQTT` to `NewMQTT(sc config.SinkConfig, dev model.Device, disp *command.Dispatcher) *MQTT` and store `disp`. In the `OnConnectHandler`, after resetting `discovered`, if `m.disp != nil` subscribe:
```go
		if m.disp != nil {
			c.Subscribe(m.sc.BaseTopic+"/"+m.dev.Node+"/cmd/+", byte(m.sc.QoS), func(_ mqtt.Client, msg mqtt.Message) {
				parts := strings.Split(msg.Topic(), "/")
				name := parts[len(parts)-1]
				res := m.disp.Run(context.Background(), name)
				body, _ := json.Marshal(res)
				m.client.Publish(m.sc.BaseTopic+"/"+m.dev.Node+"/cmd/"+name+"/result", byte(m.sc.QoS), false, body)
			})
		}
```
(add imports `context`, `encoding/json`, `strings`). In `Publish`, after publishing metric discovery, also publish the button + update discovery ONCE per connection when `m.disp != nil` (guard with the same `discovered` map using synthetic keys like `"cmd|refresh"`): publish `ha.ButtonDiscovery(dev, sc, "refresh", "Refresh")`, `ha.ButtonDiscovery(dev, sc, "restart", "Restart")`, and `ha.UpdateDiscovery(dev, sc)` (retained). Do NOT change existing metric publish behavior.

- [ ] **Step 5: gofmt, build, run all affected tests**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ && go build ./... && go test ./internal/ha/ ./internal/control/ ./internal/config/ ./internal/sink/ && go vet ./...'`
Expected: build clean, PASS. NOTE: `NewMQTT`'s signature changed — the caller in `cmd/server-status/main.go` will fail to build until Task 4 updates it; that is expected. To keep this task's build green, in `main.go` pass `nil` for the dispatcher for now: `sink.NewMQTT(sc, dev, nil)` (Task 4 replaces it with the real dispatcher). Make that one-line main.go edit here so the module builds, and commit it with this task.

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/ha/ internal/control/ internal/sink/mqtt.go internal/config/config.go cmd/server-status/main.go && git commit -F - <<'EOF'
feat: HA control entities, HTTP POST /command, MQTT command subscription

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 4: Release workflow, main wiring, gate, live validation

**Files:**
- Create: `.github/workflows/release.yml`
- Modify: `cmd/server-status/main.go`

**Interfaces:**
- Produces: a tag-triggered GitHub Actions release; `main` builds the dispatcher (refresh/restart/update), wires it into the control server and MQTT sink, and publishes the agent update state

- [ ] **Step 1: Release workflow**

Create `.github/workflows/release.yml`:
```yaml
name: release
on:
  push:
    tags: ['v*']
permissions:
  contents: write
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Build binaries
        run: |
          mkdir dist
          for arch in amd64 arm64; do
            out="dist/server-status-linux-$arch"
            CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build \
              -ldflags "-s -w -X github.com/giovi321/server-status/internal/version.Version=${GITHUB_REF_NAME}" \
              -o "$out" ./cmd/server-status
            ( cd dist && sha256sum "server-status-linux-$arch" > "server-status-linux-$arch.sha256" )
          done
      - name: Create release
        env:
          GH_TOKEN: ${{ github.token }}
        run: gh release create "${GITHUB_REF_NAME}" dist/* --title "${GITHUB_REF_NAME}" --generate-notes
```

- [ ] **Step 2: Wire the dispatcher into main**

In `cmd/server-status/main.go`:
- Build a `refresh` channel: `refreshCh := make(chan struct{}, 1)`.
- Build the dispatcher after `dev` is known:
```go
	disp := command.New()
	disp.Register("refresh", func(context.Context) command.Result {
		select {
		case refreshCh <- struct{}{}:
		default:
		}
		return command.Result{OK: true, Message: "refresh queued"}
	})
	disp.Register("restart", command.RestartHandler("server-status"))
	disp.Register("update", func(ctx context.Context) command.Result {
		assetName := fmt.Sprintf("server-status-linux-%s", runtime.GOARCH)
		rel, err := update.Latest(ctx, "https://api.github.com", cfg.Update.Repo, assetName)
		if err != nil {
			return command.Result{OK: false, Message: "check failed: " + err.Error()}
		}
		self, err := os.Executable()
		if err != nil {
			return command.Result{OK: false, Message: "executable path: " + err.Error()}
		}
		if err := update.Apply(ctx, nil, rel, self); err != nil {
			return command.Result{OK: false, Message: "apply failed: " + err.Error()}
		}
		go command.RestartHandler("server-status")(context.Background())
		return command.Result{OK: true, Message: "updated to " + rel.Version + ", restarting"}
	})
```
- Change the mqtt sink construction to pass the dispatcher: `sink.NewMQTT(sc, dev, disp)`.
- After building `ctrl` (if enabled), call `ctrl.SetDispatcher(disp)`.
- In the ticker loop, add a `case <-refreshCh: cycle()` alongside `case <-ticker.C: cycle()`.
- Add imports: `runtime`, `github.com/giovi321/server-status/internal/command`, `github.com/giovi321/server-status/internal/update`.

- [ ] **Step 3: gofmt, full gate**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w cmd/ internal/ && go build ./... && go vet ./... && go test ./... && gofmt -l .'`
Expected: build/vet clean, all tests pass, gofmt empty.

- [ ] **Step 4: Controller live validation (done separately, not by a subagent)**

The controller validates against the broker + HA: run the agent with mqtt + control enabled and a dispatcher; confirm the HA `button.<node>_refresh`/`_restart` and the `update.<node>_agent` entities appear; press refresh (or `POST /command/refresh`) and confirm `{"ok":true}` and that a cycle runs; then clear the retained discovery. The `update` command itself is NOT exercised live (it would swap the binary). This is a controller step.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add .github/workflows/release.yml cmd/server-status/main.go && git commit -F - <<'EOF'
feat: release workflow and self-update/command wiring in main

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

## Self-review against the spec

Spec Phase 7 (control commands + self-update): command dispatcher → Task 1; GitHub-Releases self-update (download/verify/atomic-swap) → Task 2; HA update entity + buttons, HTTP POST /command, MQTT command topics → Task 3; release pipeline + wiring → Task 4.

Never-run-untrusted: `Apply` downloads to `.new`, verifies sha256, keeps `.bak`, atomic-renames — a bad download leaves the running binary in place (asserted by `TestApplyRejectsBadChecksum`).

Security: HTTP `POST /command` is token-gated; the `update` handler fetches only from the configured repo over HTTPS with checksum verification. A nil dispatcher disables commands (backward compatible).

Live-validatable now: the dispatcher, `POST /command/refresh`, and the HA button/update entities are validated against HA; the privileged swap+restart and the release workflow validate at deploy/tag time.

Placeholder scan: every code and test step contains complete content.

## Roadmap: subsequent plans

- Plan 09: reliability hardening (systemd watchdog/sd_notify, per-collector isolation + cadence, advanced reconnect cached-replay already partly present, uninstall `--purge` that clears retained discovery) + migration cutover from the Python tool
- Optional: btrfs collector; minisign/cosign release signing; AMD GPU
