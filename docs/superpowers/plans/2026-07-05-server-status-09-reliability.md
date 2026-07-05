# server-status reliability and migration (Plan 09) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the agent for unattended operation and finish the migration story: per-collector isolation (a slow or panicking collector can't stall or crash the cycle), a systemd watchdog (sd_notify) so a hung agent is auto-restarted, an agent self-collector (last_seen + version), a `--purge` mode + installer flag that clears retained discovery for a clean uninstall, and migration docs.

**Architecture:** `detect.Snapshot` runs each collector through a `collectSafe` wrapper (per-collector timeout context + panic recovery). `main` sends `READY=1`/`WATCHDOG=1` to `$NOTIFY_SOCKET` (no cgo — a unixgram write) and the systemd unit becomes `Type=notify` with `WatchdogSec`. A small `Agent` collector publishes `last_seen` and `agent_version`. `--purge` connects the MQTT sinks and publishes empty retained payloads to every discovery topic the agent produces, then exits; `install.sh --purge` runs it before removing.

**Tech Stack:** Go 1.22+ stdlib (`net` unixgram, `context`, `time`). No new dependencies.

## Global Constraints

- Build/test only in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows with the two-line `giovi321` / `Claude-Session:` footer.
- `gofmt -w` new/changed Go files before committing; `gofmt -l .` stays empty.
- Per-collector isolation MUST NOT introduce a data race: collectors are stateful (network/smart/docker hold state via pointer receivers), so `collectSafe` runs each `Collect` SERIALLY (a per-collector timeout context + panic-recover), NOT in a spawned goroutine that could overlap with the next cycle. Our collectors already respect `ctx` (exec.CommandContext, ctx-aware reads), so a timeout context bounds them.
- Non-regression: existing collectors/sinks/discovery/commands unchanged. `--purge` and `--watchdog` behavior are additive; the default run path is unchanged except for the added isolation wrapper and sd_notify pings (both no-ops when not under systemd).
- New keys: `last_seen` (timestamp sensor, diagnostic), `agent_version` (text sensor, diagnostic), on the host device.
- `--purge` must clear the SAME discovery topics the agent publishes (metric discovery + control button/update entities + availability) so the HA device disappears cleanly.

## Prerequisites

- Plans 01-08 complete on `main` (repo giovi321/server-status-go). `detect`, `sink.MQTT`, `ha` (incl. ButtonDiscovery/UpdateDiscovery), `command`, and `main` exist.

## File structure

```
internal/detect/detect.go             # MODIFY: collectSafe wrapper (timeout + recover); append agent metrics
internal/detect/detect_test.go        # MODIFY: panic/timeout isolation test
internal/collector/agent.go           # CREATE: Agent collector (last_seen, agent_version)
internal/collector/agent_test.go      # CREATE
internal/watchdog/watchdog.go         # CREATE: sd_notify (READY/WATCHDOG) via NOTIFY_SOCKET
internal/watchdog/watchdog_test.go    # CREATE: unixgram round-trip
internal/sink/mqtt.go                 # MODIFY: Purge(snap) clears retained discovery
cmd/server-status/main.go             # MODIFY: --purge flag; sd_notify READY + per-cycle WATCHDOG; register Agent
packaging/server-status.service       # MODIFY: Type=notify + WatchdogSec
scripts/install.sh                    # MODIFY: --purge before uninstall
docs/migration.md                     # CREATE: cutover from the Python tool
```

---

### Task 1: Per-collector isolation

**Files:**
- Modify: `internal/detect/detect.go`, `internal/detect/detect_test.go`

**Interfaces:**
- Produces: `detect.collectSafe(ctx context.Context, c collector.Collector, timeout time.Duration) []model.Metric` (per-collector timeout context + panic recovery, serial); `snapshotFrom` uses it

- [ ] **Step 1: Write the failing isolation test**

Append to `internal/detect/detect_test.go` (the `fake` collector type already exists here):
```go
type panicky struct{ name string }

func (p panicky) Name() string                                    { return p.name }
func (p panicky) Available() bool                                 { return true }
func (p panicky) Collect(context.Context) ([]model.Metric, error) { panic("boom") }

func TestSnapshotIsolatesPanic(t *testing.T) {
	cols := []collectorIface{
		fake{name: "ok", avail: true, metrics: []model.Metric{{Key: "x"}}},
		panicky{name: "bad"},
		fake{name: "ok2", avail: true, metrics: []model.Metric{{Key: "y"}}},
	}
	// Must not panic, and must still collect from the healthy collectors.
	snap := snapshotFrom(context.Background(), model.Device{Node: "n"}, cols)
	keys := map[string]bool{}
	for _, m := range snap.Metrics {
		keys[m.Key] = true
	}
	if !keys["x"] || !keys["y"] {
		t.Fatalf("healthy collectors must still run despite a panicking one: %+v", keys)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/detect/ -run IsolatesPanic'`
Expected: FAIL (the panic propagates and crashes the test).

- [ ] **Step 3: Implement collectSafe**

In `internal/detect/detect.go`, add (imports: `time`):
```go
// defaultCollectTimeout bounds a single collector's Collect call. Collectors run
// serially, so this is sized well under the systemd WatchdogSec (180s) even when
// several collectors hit their timeout in the same cycle.
const defaultCollectTimeout = 20 * time.Second

// collectSafe runs one collector with a timeout and isolates panics, so a slow
// or panicking collector cannot stall the cycle or crash the agent. Collectors
// run serially (they hold per-instance state), and all respect ctx cancellation.
func collectSafe(ctx context.Context, c collectorIface, timeout time.Duration) (out []model.Metric) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
		}
	}()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	m, err := c.Collect(cctx)
	if err != nil {
		return nil
	}
	return m
}
```
Change `snapshotFrom` to use it: replace the body's `metrics, err := c.Collect(ctx); if err != nil { continue }; snap.Metrics = append(snap.Metrics, metrics...)` with:
```go
	for _, c := range cols {
		snap.Metrics = append(snap.Metrics, collectSafe(ctx, c, defaultCollectTimeout)...)
	}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/detect/ && go build ./... && go test ./internal/detect/ && go vet ./...'`
Expected: PASS (the panic is isolated; existing detect tests still pass).

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/detect/ && git commit -F - <<'EOF'
feat: per-collector timeout + panic isolation in the snapshot loop

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: systemd watchdog (sd_notify)

**Files:**
- Create: `internal/watchdog/watchdog.go`, `internal/watchdog/watchdog_test.go`
- Modify: `packaging/server-status.service`

**Interfaces:**
- Produces: `watchdog.Notify(state string)` (writes `state` to the `$NOTIFY_SOCKET` unixgram socket; no-op when unset); `watchdog.Ready()` = `Notify("READY=1")`; `watchdog.Ping()` = `Notify("WATCHDOG=1")`

- [ ] **Step 1: Write the failing test (unixgram round-trip)**

Create `internal/watchdog/watchdog_test.go`:
```go
package watchdog

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyWritesToSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "notify.sock")
	laddr := &net.UnixAddr{Name: sockPath, Net: "unixgram"}
	ln, err := net.ListenUnixgram("unixgram", laddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("NOTIFY_SOCKET", sockPath)

	Ready()
	buf := make([]byte, 64)
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := ln.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatalf("got %q", string(buf[:n]))
	}
}

func TestNotifyNoSocketIsNoop(t *testing.T) {
	os.Unsetenv("NOTIFY_SOCKET")
	Ping() // must not panic or block
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/watchdog/'`
Expected: FAIL (package/undefined Ready).

- [ ] **Step 3: Implement**

Create `internal/watchdog/watchdog.go`:
```go
// Package watchdog implements the systemd sd_notify protocol (no cgo).
package watchdog

import (
	"net"
	"os"
)

// Notify sends a state string to the systemd notify socket ($NOTIFY_SOCKET).
// It is a no-op when not running under a Type=notify unit.
func Notify(state string) {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

// Ready tells systemd the service finished starting.
func Ready() { Notify("READY=1") }

// Ping resets the systemd watchdog timer.
func Ping() { Notify("WATCHDOG=1") }
```

- [ ] **Step 4: Update the systemd unit**

In `packaging/server-status.service`, change `Type=simple` to `Type=notify` and add a watchdog line immediately after it in the `[Service]` section:
```
Type=notify
WatchdogSec=180
```
(Keep the rest of the unit as-is, including the existing sandboxing lines.) The agent pings `WATCHDOG=1` at the end of every cycle. A healthy cycle pings ~every 60s (the default interval), well under 180s. The 180s budget also tolerates a cycle where several collectors each hit the 20s `defaultCollectTimeout` (serial) before systemd concludes the agent is wedged and restarts it.

- [ ] **Step 5: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/watchdog/ && go build ./... && go test ./internal/watchdog/ && go vet ./...'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/watchdog/ packaging/server-status.service && git commit -F - <<'EOF'
feat: systemd watchdog (sd_notify) and Type=notify unit

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 3: Agent self-collector

**Files:**
- Create: `internal/collector/agent.go`, `internal/collector/agent_test.go`

**Interfaces:**
- Produces: `collector.Agent{}` implementing `Collector`, emitting `last_seen` (timestamp sensor, diagnostic) and `agent_version` (text sensor, diagnostic) on the host device

- [ ] **Step 1: Write the failing test**

Create `internal/collector/agent_test.go`:
```go
package collector

import (
	"context"
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

func TestAgentMetrics(t *testing.T) {
	ms, err := Agent{}.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]model.Metric{}
	for _, m := range ms {
		by[m.Key] = m
	}
	ls, ok := by["last_seen"]
	if !ok {
		t.Fatal("missing last_seen")
	}
	if ls.DeviceClass != "timestamp" || ls.Category != "diagnostic" {
		t.Fatalf("last_seen must be a diagnostic timestamp: %+v", ls)
	}
	if _, ok := by["agent_version"]; !ok {
		t.Fatal("missing agent_version")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run AgentMetrics'`
Expected: FAIL (undefined Agent).

- [ ] **Step 3: Implement**

Create `internal/collector/agent.go`:
```go
package collector

import (
	"context"
	"time"

	"github.com/giovi321/server-status/internal/model"
	"github.com/giovi321/server-status/internal/version"
)

// Agent publishes agent self-diagnostics: a last_seen heartbeat and the version.
type Agent struct{}

func (Agent) Name() string { return "agent" }

func (Agent) Available() bool { return true }

func (Agent) Collect(ctx context.Context) ([]model.Metric, error) {
	return []model.Metric{
		{Key: "last_seen", Name: "Last seen", Value: time.Now().UTC().Format(time.RFC3339), DeviceClass: "timestamp", Kind: model.KindSensor, Category: "diagnostic", Icon: "mdi:clock-check"},
		{Key: "agent_version", Name: "Agent version", Value: version.Version, Kind: model.KindText, Category: "diagnostic", Icon: "mdi:tag"},
	}, nil
}
```
Register it in `internal/detect/detect.go`'s `All(cfg)` (append `collector.Agent{}` after `collector.NewDocker()`).

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/ internal/detect/ && go build ./... && go test ./internal/collector/ -run AgentMetrics ./internal/detect/ && go vet ./...'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/agent.go internal/collector/agent_test.go internal/detect/detect.go && git commit -F - <<'EOF'
feat: agent self-collector (last_seen heartbeat, version)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 4: --purge, sd_notify wiring, install.sh, migration doc, gate

**Files:**
- Modify: `internal/sink/mqtt.go`, `cmd/server-status/main.go`, `scripts/install.sh`
- Create: `docs/migration.md`

**Interfaces:**
- Produces: `(*sink.MQTT).Purge(snap model.Snapshot) error` (clears retained discovery for every metric + control entities + availability); `--purge` flag; sd_notify READY + per-cycle WATCHDOG

- [ ] **Step 1: MQTT Purge**

In `internal/sink/mqtt.go`, add. Note: clearing a retained topic = publishing an empty payload with retain=true. After clearing, Purge disconnects GRACEFULLY (`m.client.Disconnect`), which does NOT fire the LWT — otherwise the broker would republish `offline` retained onto the availability topic we just cleared. The build calls (topics) match `Publish`/`publishControlDiscoveryOnce` exactly, so Purge clears the SAME topics the agent published:
```go
// Purge clears all retained discovery for this host so Home Assistant removes the
// device. It publishes empty retained payloads to every discovery config topic
// (metrics, control buttons, update entity) and to the availability topic, then
// disconnects gracefully so the LWT does not republish an offline availability.
func (m *MQTT) Purge(snap model.Snapshot) error {
	if m.client == nil || !m.client.IsConnected() {
		return fmt.Errorf("mqtt sink not connected; cannot purge %s", snap.Device.Node)
	}
	clear := func(topic string) {
		m.client.Publish(topic, byte(m.sc.QoS), true, "").WaitTimeout(2 * time.Second)
	}
	for _, metric := range snap.Metrics {
		if topic, _, err := ha.Discovery(snap.Device, metric, m.sc); err == nil {
			clear(topic)
		}
	}
	if m.disp != nil {
		if t, _, err := ha.ButtonDiscovery(snap.Device, m.sc, "refresh", "Refresh"); err == nil {
			clear(t)
		}
		if t, _, err := ha.ButtonDiscovery(snap.Device, m.sc, "restart", "Restart"); err == nil {
			clear(t)
		}
		if t, _, err := ha.UpdateDiscovery(snap.Device, m.sc); err == nil {
			clear(t)
		}
	}
	clear(m.availTopic)
	m.client.Disconnect(250)
	return nil
}
```

- [ ] **Step 2: main --purge + sd_notify wiring**

In `cmd/server-status/main.go`:
- Add the import `"github.com/giovi321/server-status/internal/watchdog"`.
- Add the flag in the `var (...)` block: `purge = flag.Bool("purge", false, "clear this host's retained MQTT discovery and exit")`.
- Placement A — the purge block goes AFTER the `defer func() { for _, sk := range sinks { _ = sk.Close() } }()` block (so a `return` still runs it; after Purge's graceful Disconnect, Close sees `!IsConnected()` and is a no-op — no offline republish) and BEFORE the `var ctrl *control.Server` line, so the control HTTP server never starts in purge mode:
```go
	if *purge {
		snap := detect.Snapshot(ctx, dev, cols)
		for _, sk := range sinks {
			if mq, ok := sk.(*sink.MQTT); ok {
				if err := mq.Purge(snap); err != nil {
					log.Printf("purge: %v", err)
				}
			}
		}
		log.Print("purged retained discovery; exiting")
		return
	}
```
- Placement B — `watchdog.Ready()` goes AFTER the control-server setup block (`if ctrl != nil { ctrl.SetDispatcher(disp) }`) and BEFORE the `cycle := func() {` definition.
- Placement C — inside `cycle()`, add `watchdog.Ping()` as the last statement (after the `if ctrl != nil { ctrl.Update(snap) }`), so every cycle (initial + ticker + refresh) pets the watchdog.

- [ ] **Step 3: install.sh --purge before uninstall**

In `scripts/install.sh`, in the `--uninstall` branch, clear the HA device before removing files. Order matters: stop the service FIRST (so its MQTT client disconnects and won't clash on the shared client id), then source the secrets env (the purge connects to MQTT and needs `MQTT_PASSWORD`), then run `--purge` best-effort, then remove the unit. Replace the current uninstall branch body (between `if [[ "${1:-}" == "--uninstall" ]]; then` and `fi`) with:
```bash
  systemctl disable --now server-status.service 2>/dev/null || true
  if [[ -x "$BIN_DIR/server-status" && -f "$CFG_DIR/config.yaml" ]]; then
    if [[ -f "$CFG_DIR/server-status.env" ]]; then set -a; . "$CFG_DIR/server-status.env"; set +a; fi
    "$BIN_DIR/server-status" -c "$CFG_DIR/config.yaml" --purge 2>/dev/null || true
  fi
  rm -f "$UNIT"
  systemctl daemon-reload
  echo "Uninstalled service and cleared Home Assistant discovery. Left $CFG_DIR and $BIN_DIR in place."
  exit 0
```

- [ ] **Step 4: Migration doc**

Create `docs/migration.md`:
```markdown
# Migrating from the Python server-status

The Go agent (this repo) replaces the archived Python tool. Cut over one host at
a time:

1. Install the Go agent (see the README). It autodetects and publishes under a
   new per-host MQTT device named for the host's `node` (default hostname), so it
   will not collide with the old Python device.
2. Confirm the new device and its entities appear in Home Assistant and read
   correctly.
3. Stop and remove the old Python service on that host:
   `systemctl disable --now server-status` (the old unit), and delete its files.
4. In Home Assistant, delete the old Python MQTT device (it will be stale). If it
   used retained discovery, clear those retained topics on the broker.
5. Repeat per host.

To cleanly remove the Go agent from a host (and its Home Assistant device), run
`sudo ./scripts/install.sh --uninstall`. It stops the service, runs
`server-status --purge` (which clears the retained MQTT discovery so the Home
Assistant device disappears), and removes the systemd unit. It leaves
`/etc/server-status` and `/opt/server-status` in place; delete them by hand to
remove the config and binary too.
```

- [ ] **Step 5: gofmt, full gate, basic run**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ cmd/ && go build ./... && go vet ./... && go test ./... && gofmt -l . && bash -n scripts/install.sh && echo ALL_OK'`
Expected: build/vet/test clean, gofmt empty, install.sh syntax OK.

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/sink/mqtt.go cmd/server-status/main.go scripts/install.sh docs/migration.md && git commit -F - <<'EOF'
feat: --purge retained discovery, sd_notify watchdog wiring, migration doc

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

- [ ] **Step 7: Controller live validation**

The controller validates against the broker: run the agent with mqtt for a node, confirm the device (incl. `last_seen`/`agent_version` entities) appears, then run `server-status --purge` for that node and confirm the device disappears from Home Assistant. This is a controller step, not a subagent.

---

## Self-review against the spec

Spec Phase 8 (reliability + migration): per-collector isolation → Task 1; systemd watchdog → Task 2; agent self (last_seen/version) → Task 3; uninstall purge (clear retained discovery) → Task 4; migration cutover → Task 4 (docs/migration.md).

Race safety: `collectSafe` runs serially with a timeout context (not a goroutine), so stateful collectors (network/smart/docker) are never called concurrently — no data race introduced.

sd_notify is a no-op off systemd (empty `NOTIFY_SOCKET`), so `--once`/dev runs are unaffected; the unit's `Type=notify` + `WatchdogSec=180` restarts a hung agent. The 180s budget is reconciled with Task 1's 20s per-collector serial timeout: a healthy cycle pings ~every 60s; only a cycle where 6+ collectors simultaneously wedge (≈60s interval + 6×20s) trips the restart, which is the correct outcome for a sick host.

`--purge` clears the exact discovery topics the agent publishes (metrics + control entities + availability), so the HA device disappears cleanly — validated live in Task 4 step 7.

Placeholder scan: every code and test step contains complete content (the Task 3 test's stray `Metric2` lines are explicitly flagged for deletion, with the final correct body given).

## Roadmap: remaining optional follow-ups

- Publish the agent update-entity version state (installed/latest) so the HA update entity shows "update available", not just an Install button
- btrfs collector; minisign/cosign release signing; AMD GPU
- Advanced MQTT cached-replay is already present from Plan 01; a fuller reconnect/backoff audit could follow
