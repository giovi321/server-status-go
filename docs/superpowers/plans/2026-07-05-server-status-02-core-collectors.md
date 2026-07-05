# server-status core collectors (Plan 02) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the remaining Phase 2 "always/auto" collectors to the Go agent — load average, swap, filesystems, network interfaces, hwmon temperatures, apt updates + reboot-required, and systemd failed units — each autodetected and published to MQTT/HA on the host device.

**Architecture:** Same as Plan 01. Each collector splits a pure parser (fixture-tested, cross-platform) from a thin Linux reader. This plan first adds an `Instance` field to the metric model so multi-instance host metrics (many mounts, many NICs, many temp sensors) get unique, readable entities without becoming sub-devices, then adds the collectors and wires them into `detect.All()`.

**Tech Stack:** Go 1.22+, existing deps (paho, yaml). Filesystem stats use `golang.org/x/sys/unix` (Statfs). Tests use fixtures under `testdata/` and per-collector fixture strings.

## Global Constraints

- Build/test only run in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows; commit messages end with the two-line `giovi321` / `Claude-Session:` footer.
- Run `gofmt -w` on new/changed Go files before committing; `gofmt -l .` must stay empty.
- Agent is a pure state publisher — no thresholds, no notifications. All new metrics attach to the HOST device (`Component == ""`); no sub-devices in this plan (those are Plan 03+).
- Canonical snake_case keys introduced here (fixed vocabulary): `load_1m`, `load_5m`, `load_15m`, `swap_used`, `fs_usage`, `fs_used_bytes`, `fs_total_bytes`, `fs_inode_usage`, `fs_type`, `fs_read_only`, `net_rx_rate`, `net_tx_rate`, `net_operstate`, `temperature`, `apt_updates`, `apt_security_updates`, `reboot_required`, `systemd_failed_units`, `systemd_failed_list`.
- Multi-instance metrics (filesystems, network, temps) set `Metric.Instance` to a stable instance id (mount slug, interface name, sensor label slug) and set `Metric.Name` to the human leaf INCLUDING the instance (e.g. "Root usage", "eth0 rx rate", "Package temperature"). `Key` stays canonical and shared across instances.
- Collector interface stays exactly `Name()`, `Available()`, `Collect(ctx)` — NO `Interval()` (deferred to Plan 08). Slow collectors (apt, systemd) cache to `/var/lib/server-status/` with an internal min-interval, mirroring Plan 01's approach is NOT needed yet; keep it simple: apt/systemd run each cycle but are cheap enough, EXCEPT apt which caches (see Task 8).
- Filesystem detection filters pseudo/virtual filesystems; only real block-backed mounts are published. Network detection skips loopback and virtual interfaces (lo, docker0, veth*, br-*) by default.
- Do not add webhook, HTTP control, sub-devices, SMART/docker/GPU, or self-update — later plans.

## Prerequisites

- Plan 01 merged/committed on branch `server-status-revamp` (HEAD at or after `9f5a0ca`). The packages `model`, `config`, `ident`, `collector` (cpu/memory/uptime), `detect`, `ha`, `sink` exist and are green.
- Go 1.22+ in WSL Debian; live MQTT/HA validation reachable at the user's broker for the final task.

## Design note: multi-instance metrics (decided)

Filesystems, network interfaces, and temperature sensors are multi-instance but live on the host device (per spec §9, not sub-devices). To give each instance a unique, human-readable entity without spawning a sub-device per mount/NIC, `Metric` gains an `Instance` field. The HA discovery builder folds the instance slug into `unique_id`, `object_id`, and the state topic (keeping them unique and stable across reboots), while the entity's display name — set by the collector in `Metric.Name` — carries the human instance label (e.g. "Root usage"). Home Assistant composes the entity as "<device> <name>", so a mount reads "gc01srvr Root usage" with entity_id `sensor.gc01srvr_root_usage`. Single-instance metrics leave `Instance` empty and are unaffected.

## File structure

```
internal/model/metric.go                 # MODIFY: add Instance field
internal/ha/topics.go                    # MODIFY: instance in ObjectID/UniqueID/StateTopic + slug
internal/ha/topics_test.go               # MODIFY: instance cases
internal/ha/discovery.go                 # MODIFY: pass Instance through
internal/ha/discovery_test.go            # MODIFY: instance discovery test
internal/sink/mqtt.go                    # MODIFY: pass metric.Instance to StateTopic
internal/collector/load.go               # CREATE
internal/collector/load_test.go          # CREATE
internal/collector/swap.go               # CREATE
internal/collector/swap_test.go          # CREATE
internal/collector/filesystem.go         # CREATE
internal/collector/filesystem_test.go    # CREATE
internal/collector/network.go            # CREATE
internal/collector/network_test.go       # CREATE
internal/collector/temperature.go        # CREATE
internal/collector/temperature_test.go   # CREATE
internal/collector/apt.go                # CREATE
internal/collector/apt_test.go           # CREATE
internal/collector/systemd.go            # CREATE
internal/collector/systemd_test.go       # CREATE
internal/detect/detect.go                # MODIFY: register new collectors in All()
```

---

### Task 1: Add Instance to the metric model and discovery

**Files:**
- Modify: `internal/model/metric.go`
- Modify: `internal/ha/topics.go`, `internal/ha/topics_test.go`
- Modify: `internal/ha/discovery.go`, `internal/ha/discovery_test.go`
- Modify: `internal/sink/mqtt.go`

**Interfaces:**
- Produces: `model.Metric.Instance string`; `ha.InstanceSlug(s string) string`; updated signatures `ha.ObjectID(node, component, key, instance string) string`, `ha.UniqueID(dev model.Device, m model.Metric) string` (now folds Instance), `ha.StateTopic(base, node, component, key, instance string) string`; `ha.Discovery` unchanged signature but folds Instance internally

- [ ] **Step 1: Add the Instance field to the model**

In `internal/model/metric.go`, add `Instance` to the `Metric` struct (place it right after `Component`):
```go
	Component   string
	Instance    string
```

- [ ] **Step 2: Write failing topics tests for instances**

In `internal/ha/topics_test.go`, add:
```go
func TestInstanceSlug(t *testing.T) {
	cases := map[string]string{
		"root":                "root",
		"/dev/sda1":           "dev-sda1",
		"Package id 0":        "package-id-0",
		"eth0":                "eth0",
	}
	for in, want := range cases {
		if got := InstanceSlug(in); got != want {
			t.Errorf("InstanceSlug(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTopicsWithInstance(t *testing.T) {
	if got := StateTopic("server-status", "gc01srvr", "", "fs_usage", "root"); got != "server-status/gc01srvr/fs_usage/root" {
		t.Fatalf("instance state topic: %q", got)
	}
	if got := ObjectID("gc01srvr", "", "fs_usage", "root"); got != "gc01srvr_fs_usage_root" {
		t.Fatalf("instance object id: %q", got)
	}
	dev := model.Device{Identifier: "server-status-gc01srvr"}
	if got := UniqueID(dev, model.Metric{Key: "fs_usage", Instance: "root"}); got != "server-status-gc01srvr-fs_usage-root" {
		t.Fatalf("instance unique id: %q", got)
	}
}
```
Update the EXISTING `TestTopics` calls to `StateTopic`/`ObjectID` to pass the new empty-instance argument: `StateTopic("server-status", "gc01srvr", "", "cpu_usage", "")` and `StateTopic("server-status", "gc01srvr", "disk-abc", "disk_temperature", "")`; `ObjectID("gc01srvr", "", "cpu_usage", "")` and `ObjectID("gc01srvr", "disk-abc", "disk_temperature", "")`.

- [ ] **Step 3: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/ha/ -run Instance'`
Expected: FAIL, `undefined: InstanceSlug` (and the compile error from the changed signatures).

- [ ] **Step 4: Update topics.go**

In `internal/ha/topics.go`, add the slug helper and thread instance through. Replace the `StateTopic`, `ObjectID`, and `UniqueID` funcs with:
```go
import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

var instNonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// InstanceSlug turns an instance label (mount, interface, sensor) into a stable [a-z0-9-] slug.
func InstanceSlug(s string) string {
	s = strings.ToLower(s)
	s = instNonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// StateTopic is where a metric's value is published.
func StateTopic(base, node, component, key, instance string) string {
	parts := base + "/" + node
	if component != "" {
		parts += "/" + component
	}
	parts += "/" + key
	if instance != "" {
		parts += "/" + InstanceSlug(instance)
	}
	return parts
}

// ObjectID is the human-readable slug used to build the entity_id.
func ObjectID(node, component, key, instance string) string {
	id := node + "_" + key
	if component != "" {
		id = node + "_" + component + "_" + key
	}
	if instance != "" {
		id += "_" + InstanceSlug(instance)
	}
	return id
}

// UniqueID is the hidden, stable id. It may contain serials/instances via component/instance.
func UniqueID(dev model.Device, m model.Metric) string {
	id := dev.Identifier + "-" + m.Key
	if m.Component != "" {
		id = dev.Identifier + "-" + m.Component + "-" + m.Key
	}
	if m.Instance != "" {
		id += "-" + InstanceSlug(m.Instance)
	}
	return id
}
```
Keep `AvailabilityTopic`, `Component`, `DiscoveryTopic`, and `StateValue` as they were (StateValue still needs `strconv`/`fmt`).

- [ ] **Step 5: Update discovery.go to fold Instance**

In `internal/ha/discovery.go`, change the two calls inside `Discovery`:
```go
	objectID := ObjectID(dev.Node, m.Component, m.Key, m.Instance)
```
and
```go
		StateTopic:        StateTopic(sc.BaseTopic, dev.Node, m.Component, m.Key, m.Instance),
```
(`UniqueID(dev, m)` already picks up Instance from the metric.)

- [ ] **Step 6: Update the MQTT sink's StateTopic call**

In `internal/sink/mqtt.go`, in `Publish`, change:
```go
		stateTopic := ha.StateTopic(m.sc.BaseTopic, snap.Device.Node, metric.Component, metric.Key, metric.Instance)
```

- [ ] **Step 7: Add an instance discovery test**

In `internal/ha/discovery_test.go`, add:
```go
func TestDiscoveryInstance(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Identifier: "server-status-gc01srvr", Name: "gc01srvr"}
	m := model.Metric{Key: "fs_usage", Instance: "root", Name: "Root usage", Value: 42, Unit: "%", StateClass: "measurement", Kind: model.KindSensor}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/sensor/gc01srvr/gc01srvr_fs_usage_root/config" {
		t.Fatalf("topic: %q", topic)
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["state_topic"] != "server-status/gc01srvr/fs_usage/root" {
		t.Fatalf("state_topic: %v", obj["state_topic"])
	}
	if obj["unique_id"] != "server-status-gc01srvr-fs_usage-root" {
		t.Fatalf("unique_id: %v", obj["unique_id"])
	}
	if obj["name"] != "Root usage" {
		t.Fatalf("name: %v", obj["name"])
	}
}
```

- [ ] **Step 8: gofmt, run all ha + sink + model tests**

Run:
```bash
wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ && go build ./... && go test ./internal/ha/ ./internal/sink/ ./internal/model/ && go vet ./...'
```
Expected: build clean, ha tests PASS (including new instance tests and the updated existing ones), vet clean.

- [ ] **Step 9: Commit**

```bash
cd "Z:/git/server-status" && git add internal/model/metric.go internal/ha/ internal/sink/mqtt.go && git commit -F - <<'EOF'
feat: metric Instance field for multi-instance host metrics

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: Load average collector

**Files:**
- Create: `internal/collector/load.go`, `internal/collector/load_test.go`

**Interfaces:**
- Produces: `collector.parseLoadAvg(data string) ([3]float64, bool)`; `collector.Load{}` implementing `Collector`, emitting `load_1m`/`load_5m`/`load_15m`

- [ ] **Step 1: Write the failing test**

`internal/collector/load_test.go`:
```go
package collector

import (
	"math"
	"testing"
)

func TestParseLoadAvg(t *testing.T) {
	v, ok := parseLoadAvg("0.52 0.58 0.59 1/834 12345")
	if !ok {
		t.Fatal("expected ok")
	}
	if math.Abs(v[0]-0.52) > 1e-9 || math.Abs(v[1]-0.58) > 1e-9 || math.Abs(v[2]-0.59) > 1e-9 {
		t.Fatalf("got %v", v)
	}
	if _, ok := parseLoadAvg("garbage"); ok {
		t.Fatal("garbage should not parse")
	}
}

func TestLoadMetrics(t *testing.T) {
	m := loadMetrics([3]float64{0.5, 0.6, 0.7})
	got := map[string]any{}
	for _, mt := range m {
		got[mt.Key] = mt.Value
	}
	if got["load_1m"] != 0.5 || got["load_5m"] != 0.6 || got["load_15m"] != 0.7 {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Load'`
Expected: FAIL, `undefined: parseLoadAvg`.

- [ ] **Step 3: Implement**

`internal/collector/load.go`:
```go
package collector

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseLoadAvg parses the first three fields of /proc/loadavg.
func parseLoadAvg(data string) ([3]float64, bool) {
	f := strings.Fields(data)
	if len(f) < 3 {
		return [3]float64{}, false
	}
	var out [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(f[i], 64)
		if err != nil {
			return [3]float64{}, false
		}
		out[i] = v
	}
	return out, true
}

func loadMetrics(v [3]float64) []model.Metric {
	mk := func(key, name string, val float64) model.Metric {
		return model.Metric{Key: key, Name: name, Value: val, StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:gauge"}
	}
	return []model.Metric{
		mk("load_1m", "Load 1m", v[0]),
		mk("load_5m", "Load 5m", v[1]),
		mk("load_15m", "Load 15m", v[2]),
	}
}

// Load publishes the 1/5/15-minute load averages.
type Load struct{}

func (Load) Name() string { return "load" }

func (Load) Available() bool {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return false
	}
	_, ok := parseLoadAvg(string(data))
	return ok
}

func (Load) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, nil
	}
	v, ok := parseLoadAvg(string(data))
	if !ok {
		return nil, nil
	}
	return loadMetrics(v), nil
}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/load.go && go test ./internal/collector/ -run Load'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/load.go internal/collector/load_test.go && git commit -F - <<'EOF'
feat: load average collector

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 3: Swap collector

**Files:**
- Create: `internal/collector/swap.go`, `internal/collector/swap_test.go`

**Interfaces:**
- Consumes: `collector.parseMeminfo` (from Plan 01's memory.go, same package), `collector.pct`
- Produces: `collector.swapMetric(mem map[string]uint64) (model.Metric, bool)`; `collector.Swap{}` implementing `Collector`, emitting `swap_used` (percent; absent/false when SwapTotal is 0)

- [ ] **Step 1: Write the failing test**

`internal/collector/swap_test.go`:
```go
package collector

import "testing"

func TestSwapMetric(t *testing.T) {
	mem := map[string]uint64{"SwapTotal": 1000, "SwapFree": 250}
	m, ok := swapMetric(mem)
	if !ok {
		t.Fatal("expected ok")
	}
	if m.Key != "swap_used" || m.Value != 75 { // used 750/1000 = 75%
		t.Fatalf("got %+v", m)
	}
}

func TestSwapMetricNoSwap(t *testing.T) {
	if _, ok := swapMetric(map[string]uint64{"SwapTotal": 0, "SwapFree": 0}); ok {
		t.Fatal("no swap configured should not emit")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Swap'`
Expected: FAIL, `undefined: swapMetric`.

- [ ] **Step 3: Implement**

`internal/collector/swap.go`:
```go
package collector

import (
	"context"
	"os"

	"github.com/giovi321/server-status/internal/model"
)

func swapMetric(mem map[string]uint64) (model.Metric, bool) {
	total := mem["SwapTotal"]
	if total == 0 {
		return model.Metric{}, false
	}
	used := total - mem["SwapFree"]
	return model.Metric{
		Key: "swap_used", Name: "Swap used", Value: pct(used, total), Unit: "%",
		StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:harddisk",
	}, true
}

// Swap publishes swap used percentage, only when swap is configured.
type Swap struct{}

func (Swap) Name() string { return "swap" }

func (Swap) Available() bool {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return false
	}
	mem, ok := parseMeminfo(string(data))
	if !ok {
		return false
	}
	_, has := swapMetric(mem)
	return has
}

func (Swap) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, nil
	}
	mem, ok := parseMeminfo(string(data))
	if !ok {
		return nil, nil
	}
	m, has := swapMetric(mem)
	if !has {
		return nil, nil
	}
	return []model.Metric{m}, nil
}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/swap.go && go test ./internal/collector/ -run Swap'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/swap.go internal/collector/swap_test.go && git commit -F - <<'EOF'
feat: swap collector

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 4: Filesystem collector

**Files:**
- Create: `internal/collector/filesystem.go`, `internal/collector/filesystem_test.go`

**Interfaces:**
- Produces: `collector.Mount{Source, Target, FSType string; ReadOnly bool}`; `collector.parseMountinfo(data string) []Mount` (filters pseudo/virtual filesystems); `collector.Filesystem{}` implementing `Collector`, emitting per real mount: `fs_usage` (%), `fs_used_bytes`, `fs_total_bytes` (data_size), `fs_inode_usage` (%), `fs_type` (diagnostic text), `fs_read_only` (binary_sensor, problem), all with `Instance` = the mount target and `Name` = "<target> <leaf>"

**Depends on:** Task 1 (Instance field)

- [ ] **Step 1: Add the unix dependency**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go get golang.org/x/sys/unix'`

- [ ] **Step 2: Write the failing test**

`internal/collector/filesystem_test.go`:
```go
package collector

import "testing"

// A trimmed /proc/self/mountinfo sample: real root + a data mount, plus pseudo fs to filter.
const mountinfoFixture = `22 28 0:21 / /proc rw,nosuid shared:14 - proc proc rw
23 28 0:22 / /sys rw,nosuid shared:15 - sysfs sysfs rw
24 28 0:5 / /dev rw,nosuid shared:2 - devtmpfs devtmpfs rw,size=1234k
28 1 254:0 / / rw,relatime shared:1 - ext4 /dev/mapper/vg-root rw
40 28 254:1 / /mnt/storage ro,relatime shared:29 - xfs /dev/mapper/vg-storage ro
55 28 0:44 / /run/snapd/ns rw shared:99 - tmpfs tmpfs rw
77 28 7:0 / /snap/core/1 ro,nodev - squashfs /dev/loop0 ro
`

func TestParseMountinfoFiltersPseudo(t *testing.T) {
	mounts := parseMountinfo(mountinfoFixture)
	byTarget := map[string]Mount{}
	for _, m := range mounts {
		byTarget[m.Target] = m
	}
	if _, ok := byTarget["/proc"]; ok {
		t.Error("/proc should be filtered")
	}
	if _, ok := byTarget["/run/snapd/ns"]; ok {
		t.Error("tmpfs should be filtered")
	}
	if _, ok := byTarget["/snap/core/1"]; ok {
		t.Error("squashfs should be filtered")
	}
	root, ok := byTarget["/"]
	if !ok || root.FSType != "ext4" || root.ReadOnly {
		t.Fatalf("root: %+v ok=%v", root, ok)
	}
	st, ok := byTarget["/mnt/storage"]
	if !ok || st.FSType != "xfs" || !st.ReadOnly {
		t.Fatalf("storage: %+v ok=%v", st, ok)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Mountinfo'`
Expected: FAIL, `undefined: parseMountinfo`.

- [ ] **Step 4: Implement**

`internal/collector/filesystem.go`:
```go
package collector

import (
	"context"
	"os"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/giovi321/server-status/internal/model"
)

// Mount is one real mounted filesystem.
type Mount struct {
	Source   string
	Target   string
	FSType   string
	ReadOnly bool
}

// pseudoFS are filesystem types that never represent real block-backed storage.
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "tmpfs": true, "devpts": true,
	"cgroup": true, "cgroup2": true, "pstore": true, "bpf": true, "tracefs": true,
	"debugfs": true, "mqueue": true, "hugetlbfs": true, "securityfs": true,
	"fusectl": true, "configfs": true, "squashfs": true, "overlay": true,
	"autofs": true, "binfmt_misc": true, "rpc_pipefs": true, "nsfs": true, "ramfs": true,
}

// parseMountinfo parses /proc/self/mountinfo and returns only real (block-backed) mounts.
// Format: "id parent maj:min root mountpoint opts... - fstype source superopts".
func parseMountinfo(data string) []Mount {
	var out []Mount
	seen := map[string]bool{}
	for _, line := range strings.Split(data, "\n") {
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		left := strings.Fields(line[:sep])
		right := strings.Fields(line[sep+3:])
		if len(left) < 6 || len(right) < 2 {
			continue
		}
		target := left[4]
		opts := left[5]
		fstype := right[0]
		source := right[1]
		if pseudoFS[fstype] {
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		ro := false
		for _, o := range strings.Split(opts, ",") {
			if o == "ro" {
				ro = true
			}
		}
		out = append(out, Mount{Source: source, Target: target, FSType: fstype, ReadOnly: ro})
	}
	return out
}

func mountMetrics(m Mount) []model.Metric {
	inst := m.Target
	var st unix.Statfs_t
	if err := unix.Statfs(m.Target, &st); err != nil || st.Blocks == 0 {
		return nil
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bfree * uint64(st.Bsize)
	used := total - free
	usagePct := int(float64(used)*100.0/float64(total) + 0.5)
	var inodePct int
	if st.Files > 0 {
		inodePct = int(float64(st.Files-st.Ffree)*100.0/float64(st.Files) + 0.5)
	}
	name := func(leaf string) string { return m.Target + " " + leaf }
	return []model.Metric{
		{Key: "fs_usage", Instance: inst, Name: name("usage"), Value: usagePct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:harddisk"},
		{Key: "fs_used_bytes", Instance: inst, Name: name("used"), Value: int64(used), Unit: "B", DeviceClass: "data_size", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "fs_total_bytes", Instance: inst, Name: name("size"), Value: int64(total), Unit: "B", DeviceClass: "data_size", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "fs_inode_usage", Instance: inst, Name: name("inode usage"), Value: inodePct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "fs_type", Instance: inst, Name: name("filesystem"), Value: m.FSType, Kind: model.KindText, Category: "diagnostic"},
		{Key: "fs_read_only", Instance: inst, Name: name("read only"), Value: m.ReadOnly, DeviceClass: "problem", Kind: model.KindBinarySensor, Category: "diagnostic"},
	}
}

func readMounts() []Mount {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	return parseMountinfo(string(data))
}

// Filesystem publishes usage and health for each real mounted filesystem.
type Filesystem struct{}

func (Filesystem) Name() string { return "filesystem" }

func (Filesystem) Available() bool { return len(readMounts()) > 0 }

func (Filesystem) Collect(ctx context.Context) ([]model.Metric, error) {
	var out []model.Metric
	for _, m := range readMounts() {
		out = append(out, mountMetrics(m)...)
	}
	return out, nil
}
```

- [ ] **Step 5: gofmt, run mountinfo test + build**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/filesystem.go && go build ./... && go test ./internal/collector/ -run Mountinfo'`
Expected: build clean, PASS. (The statvfs path is exercised live on WSL in Task 9.)

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/filesystem.go internal/collector/filesystem_test.go go.mod go.sum && git commit -F - <<'EOF'
feat: filesystem collector (auto-detected real mounts)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 5: Network collector

**Files:**
- Create: `internal/collector/network.go`, `internal/collector/network_test.go`

**Interfaces:**
- Produces: `collector.IfaceCounters{RxBytes, TxBytes uint64}`; `collector.parseNetDev(data string) map[string]IfaceCounters` (skips lo and virtual ifaces); `collector.rate(prev, cur uint64, seconds float64) float64`; `collector.Network{}` implementing `Collector` (stateful — holds the previous sample), emitting per real interface `net_rx_rate`/`net_tx_rate` (MB/s), `net_operstate` (binary_sensor connectivity)

**Depends on:** Task 1 (Instance field)

- [ ] **Step 1: Write the failing test**

`internal/collector/network_test.go`:
```go
package collector

import (
	"math"
	"testing"
)

const netdevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000       10    0    0    0     0          0         0     1000      10    0    0    0     0       0          0
  eth0: 2000       20    0    0    0     0          0         0     3000      30    0    0    0     0       0          0
docker0:  500        5    0    0    0     0          0         0      600       6    0    0    0     0       0          0
`

func TestParseNetDevSkipsVirtual(t *testing.T) {
	m := parseNetDev(netdevFixture)
	if _, ok := m["lo"]; ok {
		t.Error("lo should be skipped")
	}
	if _, ok := m["docker0"]; ok {
		t.Error("docker0 should be skipped")
	}
	e, ok := m["eth0"]
	if !ok || e.RxBytes != 2000 || e.TxBytes != 3000 {
		t.Fatalf("eth0: %+v ok=%v", e, ok)
	}
}

func TestRate(t *testing.T) {
	// 1,048,576 bytes over 1s = 1 MB/s
	if got := rate(0, 1048576, 1.0); math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("got %v", got)
	}
	// counter reset (cur < prev) => 0
	if got := rate(100, 10, 1.0); got != 0 {
		t.Fatalf("reset should be 0, got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run "NetDev|Rate"'`
Expected: FAIL, `undefined: parseNetDev`.

- [ ] **Step 3: Implement**

`internal/collector/network.go`:
```go
package collector

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/giovi321/server-status/internal/model"
)

// IfaceCounters holds cumulative byte counters for one interface.
type IfaceCounters struct {
	RxBytes uint64
	TxBytes uint64
}

func skipIface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range []string{"docker", "veth", "br-", "virbr", "tun", "tap", "kube", "cni", "flannel"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// parseNetDev parses /proc/net/dev, returning real interfaces only.
func parseNetDev(data string) map[string]IfaceCounters {
	out := map[string]IfaceCounters{}
	for _, line := range strings.Split(data, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || skipIface(name) {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 16 {
			continue
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[name] = IfaceCounters{RxBytes: rx, TxBytes: tx}
	}
	return out
}

func rate(prev, cur uint64, seconds float64) float64 {
	if cur < prev || seconds <= 0 {
		return 0
	}
	return float64(cur-prev) / seconds / (1024.0 * 1024.0)
}

func operstate(iface string) bool {
	b, err := os.ReadFile("/sys/class/net/" + iface + "/operstate")
	if err != nil {
		return true // assume up if unknown
	}
	return strings.TrimSpace(string(b)) == "up"
}

func readNetDev() map[string]IfaceCounters {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil
	}
	return parseNetDev(string(data))
}

// Network publishes per-interface throughput. It is stateful: rates need two samples.
type Network struct {
	prev   map[string]IfaceCounters
	prevAt time.Time
}

func (Network) Name() string { return "network" }

func (Network) Available() bool { return len(readNetDev()) > 0 }

func (n *Network) Collect(ctx context.Context) ([]model.Metric, error) {
	cur := readNetDev()
	now := time.Now()
	var out []model.Metric
	if n.prev != nil {
		secs := now.Sub(n.prevAt).Seconds()
		for iface, c := range cur {
			p, ok := n.prev[iface]
			if !ok {
				continue
			}
			name := func(leaf string) string { return iface + " " + leaf }
			out = append(out,
				model.Metric{Key: "net_rx_rate", Instance: iface, Name: name("rx rate"), Value: round2(rate(p.RxBytes, c.RxBytes, secs)), Unit: "MB/s", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:download-network"},
				model.Metric{Key: "net_tx_rate", Instance: iface, Name: name("tx rate"), Value: round2(rate(p.TxBytes, c.TxBytes, secs)), Unit: "MB/s", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:upload-network"},
				model.Metric{Key: "net_operstate", Instance: iface, Name: name("link"), Value: operstate(iface), DeviceClass: "connectivity", Kind: model.KindBinarySensor, Category: "diagnostic"},
			)
		}
	}
	n.prev = cur
	n.prevAt = now
	return out, nil
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
```
Note: `Network` uses a pointer receiver on `Collect`, so it must be registered as `&collector.Network{}` (Task 9). The first cycle returns no rate metrics (no previous sample); subsequent cycles do.

- [ ] **Step 4: gofmt, run, build**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/network.go && go build ./... && go test ./internal/collector/ -run "NetDev|Rate"'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/network.go internal/collector/network_test.go && git commit -F - <<'EOF'
feat: network collector (per-interface throughput)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 6: Temperature collector (hwmon)

**Files:**
- Create: `internal/collector/temperature.go`, `internal/collector/temperature_test.go`

**Interfaces:**
- Produces: `collector.TempReading{Label string; MilliC int}`; `collector.hwmonReadings(root string) []TempReading` (walks a hwmon-style tree rooted at `root`, pairing `tempN_input` with `tempN_label`/chip name); `collector.Temperature{}` implementing `Collector`, emitting `temperature` per sensor with `Instance` = label, `DeviceClass` = "temperature", `Unit` = "°C"

**Depends on:** Task 1 (Instance field)

- [ ] **Step 1: Write the failing test (fixture sysfs tree)**

`internal/collector/temperature_test.go`:
```go
package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHwmonReadings(t *testing.T) {
	root := t.TempDir()
	// hwmon0: coretemp with a labeled Package sensor
	writeFile(t, filepath.Join(root, "hwmon0", "name"), "coretemp\n")
	writeFile(t, filepath.Join(root, "hwmon0", "temp1_input"), "45000\n")
	writeFile(t, filepath.Join(root, "hwmon0", "temp1_label"), "Package id 0\n")
	// hwmon1: nvme, no label -> falls back to chip name + tempN
	writeFile(t, filepath.Join(root, "hwmon1", "name"), "nvme\n")
	writeFile(t, filepath.Join(root, "hwmon1", "temp1_input"), "38000\n")

	got := map[string]int{}
	for _, r := range hwmonReadings(root) {
		got[r.Label] = r.MilliC
	}
	if got["Package id 0"] != 45000 {
		t.Fatalf("labeled reading: %+v", got)
	}
	if got["nvme temp1"] != 38000 {
		t.Fatalf("unlabeled reading fallback: %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Hwmon'`
Expected: FAIL, `undefined: hwmonReadings`.

- [ ] **Step 3: Implement**

`internal/collector/temperature.go`:
```go
package collector

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// TempReading is one temperature sensor reading in milli-degrees Celsius.
type TempReading struct {
	Label  string
	MilliC int
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// hwmonReadings walks a /sys/class/hwmon-style tree and returns labeled temperature readings.
func hwmonReadings(root string) []TempReading {
	chips, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []TempReading
	for _, chip := range chips {
		dir := filepath.Join(root, chip.Name())
		chipName := readTrim(filepath.Join(dir, "name"))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var inputs []string
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, "temp") && strings.HasSuffix(n, "_input") {
				inputs = append(inputs, n)
			}
		}
		sort.Strings(inputs)
		for _, in := range inputs {
			raw := readTrim(filepath.Join(dir, in))
			v, err := strconv.Atoi(raw)
			if err != nil {
				continue
			}
			idx := strings.TrimSuffix(in, "_input") // "temp1"
			label := readTrim(filepath.Join(dir, idx+"_label"))
			if label == "" {
				base := chipName
				if base == "" {
					base = chip.Name()
				}
				label = base + " " + idx
			}
			out = append(out, TempReading{Label: label, MilliC: v})
		}
	}
	return out
}

func tempMetrics(readings []TempReading) []model.Metric {
	var out []model.Metric
	for _, r := range readings {
		out = append(out, model.Metric{
			Key: "temperature", Instance: r.Label, Name: r.Label + " temperature",
			Value: int(float64(r.MilliC)/1000.0 + 0.5), Unit: "°C", DeviceClass: "temperature",
			StateClass: "measurement", Kind: model.KindSensor, Category: "primary",
		})
	}
	return out
}

const hwmonRoot = "/sys/class/hwmon"

// Temperature publishes hwmon temperature sensors (CPU, NVMe, chipset, drives).
type Temperature struct{}

func (Temperature) Name() string { return "temperature" }

func (Temperature) Available() bool { return len(hwmonReadings(hwmonRoot)) > 0 }

func (Temperature) Collect(ctx context.Context) ([]model.Metric, error) {
	return tempMetrics(hwmonReadings(hwmonRoot)), nil
}
```

- [ ] **Step 4: gofmt, run, build**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/temperature.go && go build ./... && go test ./internal/collector/ -run Hwmon'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/temperature.go internal/collector/temperature_test.go && git commit -F - <<'EOF'
feat: hwmon temperature collector

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 7: apt updates and reboot-required collector

**Files:**
- Create: `internal/collector/apt.go`, `internal/collector/apt_test.go`

**Interfaces:**
- Produces: `collector.parseAptUpgradable(out string) (total, security int)` (parses `apt-get -s dist-upgrade` "Inst " lines; a line mentioning a `-security` archive counts as security); `collector.Apt{}` implementing `Collector`, emitting `apt_updates`, `apt_security_updates` (counts), and `reboot_required` (binary_sensor, device_class update) from `/var/run/reboot-required`

- [ ] **Step 1: Write the failing test**

`internal/collector/apt_test.go`:
```go
package collector

import "testing"

const aptFixture = `NOTE: This is only a simulation!
Inst libc6 [2.36-9] (2.36-9+deb12u4 Debian:12.4/stable [amd64])
Inst openssl [3.0.11-1] (3.0.14-1~deb12u2 Debian-Security:12/stable-security [amd64])
Inst tzdata [2024a-0] (2024b-0+deb12u1 Debian:12.6/stable [all])
Conf libc6 (2.36-9+deb12u4 Debian:12.4/stable [amd64])
`

func TestParseAptUpgradable(t *testing.T) {
	total, sec := parseAptUpgradable(aptFixture)
	if total != 3 {
		t.Fatalf("total=%d", total)
	}
	if sec != 1 { // only the openssl line references a -security archive
		t.Fatalf("security=%d", sec)
	}
}

func TestParseAptNone(t *testing.T) {
	total, sec := parseAptUpgradable("NOTE: only a simulation\n")
	if total != 0 || sec != 0 {
		t.Fatalf("expected 0/0, got %d/%d", total, sec)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Apt'`
Expected: FAIL, `undefined: parseAptUpgradable`.

- [ ] **Step 3: Implement**

`internal/collector/apt.go`:
```go
package collector

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseAptUpgradable counts "Inst " lines from `apt-get -s dist-upgrade`.
// A line whose new-version archive mentions "security" is also counted as a security update.
func parseAptUpgradable(out string) (total, security int) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		total++
		if strings.Contains(strings.ToLower(line), "security") {
			security++
		}
	}
	return total, security
}

func aptPath() string {
	for _, p := range []string{"/usr/bin/apt-get", "/bin/apt-get"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Apt publishes upgradable package counts and the reboot-required flag.
type Apt struct{}

func (Apt) Name() string { return "apt" }

func (Apt) Available() bool { return aptPath() != "" }

func (Apt) Collect(ctx context.Context) ([]model.Metric, error) {
	p := aptPath()
	if p == "" {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, p, "-s", "dist-upgrade")
	out, _ := cmd.Output() // non-zero exit still yields parseable stdout
	total, sec := parseAptUpgradable(string(out))
	_, rebootErr := os.Stat("/var/run/reboot-required")
	rebootRequired := rebootErr == nil
	return []model.Metric{
		{Key: "apt_updates", Name: "APT updates", Value: total, StateClass: "total", Kind: model.KindSensor, Category: "primary", Icon: "mdi:package-up"},
		{Key: "apt_security_updates", Name: "APT security updates", Value: sec, StateClass: "total", Kind: model.KindSensor, Category: "primary", Icon: "mdi:shield-alert"},
		{Key: "reboot_required", Name: "Reboot required", Value: rebootRequired, DeviceClass: "update", Kind: model.KindBinarySensor, Category: "primary"},
	}, nil
}
```
Note: for Phase 2 the apt simulation runs each cycle. It is read-only and fast enough at the default 60s loop; per-collector caching/cadence lands in Plan 08.

- [ ] **Step 4: gofmt, run, build**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/apt.go && go build ./... && go test ./internal/collector/ -run Apt'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/apt.go internal/collector/apt_test.go && git commit -F - <<'EOF'
feat: apt updates and reboot-required collector

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 8: systemd failed units collector

**Files:**
- Create: `internal/collector/systemd.go`, `internal/collector/systemd_test.go`

**Interfaces:**
- Produces: `collector.parseFailedUnits(out string) []string` (parses `systemctl --failed --plain --no-legend --no-pager` unit names); `collector.Systemd{}` implementing `Collector`, emitting `systemd_failed_units` (count) and `systemd_failed_list` (diagnostic text, comma-joined or "none")

- [ ] **Step 1: Write the failing test**

`internal/collector/systemd_test.go`:
```go
package collector

import (
	"reflect"
	"testing"
)

const failedFixture = `  nginx.service       loaded failed failed A high performance web server
  backup.service      loaded failed failed Nightly backup
`

func TestParseFailedUnits(t *testing.T) {
	got := parseFailedUnits(failedFixture)
	want := []string{"nginx.service", "backup.service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if len(parseFailedUnits("")) != 0 {
		t.Fatal("empty should yield no units")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Failed'`
Expected: FAIL, `undefined: parseFailedUnits`.

- [ ] **Step 3: Implement**

`internal/collector/systemd.go`:
```go
package collector

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseFailedUnits extracts unit names from `systemctl --failed --plain --no-legend`.
func parseFailedUnits(out string) []string {
	var units []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if strings.HasSuffix(f[0], ".service") || strings.HasSuffix(f[0], ".socket") ||
			strings.HasSuffix(f[0], ".timer") || strings.HasSuffix(f[0], ".mount") || strings.HasSuffix(f[0], ".target") {
			units = append(units, f[0])
		}
	}
	return units
}

func systemctlPath() string {
	for _, p := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Systemd publishes the count and list of failed systemd units.
type Systemd struct{}

func (Systemd) Name() string { return "systemd" }

func (Systemd) Available() bool { return systemctlPath() != "" }

func (Systemd) Collect(ctx context.Context) ([]model.Metric, error) {
	p := systemctlPath()
	if p == "" {
		return nil, nil
	}
	out, _ := exec.CommandContext(ctx, p, "--failed", "--plain", "--no-legend", "--no-pager").Output()
	units := parseFailedUnits(string(out))
	list := "none"
	if len(units) > 0 {
		list = strings.Join(units, ", ")
	}
	return []model.Metric{
		{Key: "systemd_failed_units", Name: "Failed units", Value: len(units), StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:alert-circle"},
		{Key: "systemd_failed_list", Name: "Failed units list", Value: list, Kind: model.KindText, Category: "diagnostic"},
	}, nil
}
```

- [ ] **Step 4: gofmt, run, build**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/systemd.go && go build ./... && go test ./internal/collector/ -run Failed'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/systemd.go internal/collector/systemd_test.go && git commit -F - <<'EOF'
feat: systemd failed-units collector

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 9: Register collectors, gate, and live re-validation

**Files:**
- Modify: `internal/detect/detect.go`

**Interfaces:**
- Consumes: all new collectors
- Produces: updated `detect.All()` returning cpu, memory, uptime, load, swap, filesystem, network, temperature, apt, systemd

- [ ] **Step 1: Register the new collectors**

In `internal/detect/detect.go`, update `All()`:
```go
func All() []collector.Collector {
	return []collector.Collector{
		collector.CPU{},
		collector.Memory{},
		collector.Uptime{},
		collector.Load{},
		collector.Swap{},
		collector.Filesystem{},
		&collector.Network{},
		collector.Temperature{},
		collector.Apt{},
		collector.Systemd{},
	}
}
```
Note: `Network` is registered as a pointer (`&collector.Network{}`) because its `Collect` uses a pointer receiver to hold the previous sample.

- [ ] **Step 2: Full gate**

Run:
```bash
wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -l . && go vet ./... && go test ./...'
```
Expected: gofmt empty, vet clean, all packages pass.

- [ ] **Step 3: Live dump-detected on WSL**

Run:
```bash
wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go build -o server-status ./cmd/server-status && printf "node: wsltest\nsinks:\n  - type: mqtt\n    host: 127.0.0.1\n" > /tmp/ss.yaml && ./server-status -c /tmp/ss.yaml --dump-detected'
```
Expected JSON now shows the new collectors. On WSL: load, swap (if configured), filesystem (root at minimum), network (present but rates only after a second cycle), temperature/apt/systemd may be available:false depending on the WSL image — that is correct autodetection. Confirm no collector errors and that filesystem shows the root mount metrics with instances.

- [ ] **Step 4: Commit**

```bash
cd "Z:/git/server-status" && git add internal/detect/detect.go && git commit -F - <<'EOF'
feat: register Plan 02 core collectors

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

- [ ] **Step 5: Live MQTT + HA re-validation (controller-run, needs broker)**

The controller runs `./server-status --once` against the real broker with a short node (e.g. `sstest2`), confirms via the Home Assistant integration that the host device now carries the expanded entity set (load, swap, per-mount filesystem usage with readable names like "Root usage", network, and any available temps/apt/systemd), then clears the retained discovery to remove the test device. This is done by the controller, not a subagent.

---

## Self-review against the spec

Spec Phase 2 (spec §19 item 2: "Autoconfiguration and the core collector set — load, swap, filesystems with fs_* keys, network, hwmon temperatures, apt updates, reboot-required, systemd failed units"):

- load → Task 2; swap → Task 3; filesystems (fs_* keys, auto-detected, read-only detection) → Task 4; network (per-interface rates, virtual skipped) → Task 5; hwmon temperatures (hwmon-first, no lm-sensors dep) → Task 6; apt updates + security + reboot-required → Task 7; systemd failed units → Task 8; wiring + gate + live re-validation → Task 9.
- Multi-instance modeling (spec §9 filesystems/network on host device, readable names) → Task 1 (Instance field) + used by Tasks 4/5/6.
- fs_* vs disk_* separation (spec self-review decision) → Task 4 uses `fs_*` exclusively.

Placeholder scan: every code and test step contains complete content.

Type consistency: `Metric.Instance` added in Task 1 is used by Tasks 4/5/6; `ha.StateTopic`/`ObjectID` new signatures are updated at every call site (discovery.go, sink/mqtt.go) in Task 1; `parseMeminfo`/`pct` reused by Task 3 exist in Plan 01's memory.go (same package); `Network` pointer-receiver registration in Task 9 matches its pointer-receiver `Collect` in Task 5.

## Roadmap: subsequent plans

- Plan 03: device hierarchy (component sub-devices, grouped/flat, disk aliasing, host-to-host parent end to end)
- Plan 04: rich storage (SMART curated/full, mdadm, ZFS, btrfs) and GPU
- Plan 05: docker (registry digest compare, container inventory, compose awareness)
- Plan 06: webhook sink + HTTP control surface, parity golden tests
- Plan 07: control commands + GitHub-Releases self-update, HA update entity, release pipeline
- Plan 08: reliability hardening (advanced reconnect/cached replay, watchdog, per-collector isolation + cadence, uninstall purge)
- Plan 09: HA validation against the live instance + migration cutover
