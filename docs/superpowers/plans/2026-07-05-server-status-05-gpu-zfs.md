# server-status GPU and ZFS (Plan 05) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two more sub-device collectors: nvidia GPU per card (temperature, utilization, memory, power, fan) and ZFS per pool (health, capacity, fragmentation, degraded), each published as its own Home Assistant sub-device under the host.

**Architecture:** Two collectors in `internal/collector`, both emitting `Component`/`ComponentName` sub-device metrics (Plan 03 mechanism). GPU parses `nvidia-smi --query-gpu=... --format=csv`; ZFS parses `zpool list -H`. Parsers are fixture-tested; live hardware validation happens on a real server (WSL has no GPU/zpool). btrfs is deferred to an optional later plan.

**Tech Stack:** Go 1.22+, existing deps (`os/exec`, string parsing). No new dependencies.

## Global Constraints

- Build/test only in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows with the two-line `giovi321` / `Claude-Session:` footer.
- `gofmt -w` new/changed Go files before committing; `gofmt -l .` stays empty.
- Canonical snake_case keys. GPU: `gpu_temperature`, `gpu_utilization`, `gpu_memory_used` (%), `gpu_power` (W), `gpu_power_limit` (W, diagnostic), `gpu_fan` (%), + diagnostic text `gpu_name`, `gpu_driver`. ZFS: `pool_health` (text), `pool_degraded` (binary_sensor problem, ON when health != ONLINE), `pool_capacity` (%), `pool_fragmentation` (%).
- Each GPU is a sub-device: `Component = "gpu-" + index`, `ComponentName = "GPU " + index`. Each pool is a sub-device: `Component = "pool-" + name`, `ComponentName = "Pool " + name`. Entity `Name` is the short leaf.
- Fields nvidia-smi reports as `[N/A]` or `[Not Supported]` (e.g. fan/power on datacenter cards) MUST be omitted, not published as 0. ZFS `frag` reported as `-` MUST be omitted.
- Non-regression: existing collectors unchanged. `detect.All(cfg)` gains the two new collectors.
- No btrfs, no docker, no webhook/HTTP/self-update.

## Prerequisites

- Plans 01-04 complete on `main` (repo giovi321/server-status-go). Sub-device discovery (Plan 03) and `detect.All(cfg)` (Plan 04) exist.
- `nvidia-smi` / `zpool` present on the target server. Not required in WSL (parsers fixture-tested).

## File structure

```
internal/collector/gpu.go        # CREATE: parseNvidiaSMI + Gpu collector
internal/collector/gpu_test.go   # CREATE: nvidia-smi CSV fixtures (incl. [N/A])
internal/collector/zfs.go        # CREATE: parseZpoolList + Zfs collector
internal/collector/zfs_test.go   # CREATE: zpool list fixtures (incl. DEGRADED, '-')
internal/detect/detect.go        # MODIFY: register Gpu + Zfs in All(cfg)
```

---

### Task 1: GPU collector (nvidia)

**Files:**
- Create: `internal/collector/gpu.go`, `internal/collector/gpu_test.go`

**Interfaces:**
- Produces: `collector.GpuInfo{Index, Name string; Temp, Util, MemTotalMB, MemUsedMB int; Power, PowerLimit *float64; Fan *int; Driver string}`; `collector.parseNvidiaSMI(csv string) []GpuInfo`; `collector.gpuMetrics(g GpuInfo) []model.Metric`; `collector.Gpu{}` implementing `Collector`

- [ ] **Step 1: Write the failing test**

Create `internal/collector/gpu_test.go`:
```go
package collector

import "testing"

// nvidia-smi --query-gpu=index,name,temperature.gpu,utilization.gpu,memory.total,memory.used,power.draw,power.limit,fan.speed,driver_version --format=csv,noheader,nounits
const nvidiaSMICSV = `0, NVIDIA GeForce RTX 3060, 45, 12, 12288, 2048, 80.50, 170.00, 30, 535.104.05
1, NVIDIA Tesla T4, 38, 0, 15360, 512, 26.30, 70.00, [N/A], 535.104.05`

func TestParseNvidiaSMI(t *testing.T) {
	gpus := parseNvidiaSMI(nvidiaSMICSV)
	if len(gpus) != 2 {
		t.Fatalf("expected 2 gpus, got %d", len(gpus))
	}
	g0 := gpus[0]
	if g0.Index != "0" || g0.Name != "NVIDIA GeForce RTX 3060" {
		t.Fatalf("g0 id: %+v", g0)
	}
	if g0.Temp != 45 || g0.Util != 12 || g0.MemTotalMB != 12288 || g0.MemUsedMB != 2048 {
		t.Fatalf("g0 core: %+v", g0)
	}
	if g0.Power == nil || *g0.Power != 80.50 || g0.Fan == nil || *g0.Fan != 30 {
		t.Fatalf("g0 power/fan: %v %v", g0.Power, g0.Fan)
	}
	if g0.Driver != "535.104.05" {
		t.Fatalf("g0 driver: %q", g0.Driver)
	}
	// Tesla T4 has no fan (datacenter card): [N/A] -> nil
	if gpus[1].Fan != nil {
		t.Fatalf("g1 fan should be nil, got %v", *gpus[1].Fan)
	}
}

func TestGpuMetricsMemoryPercent(t *testing.T) {
	gpus := parseNvidiaSMI(nvidiaSMICSV)
	ms := gpuMetrics(gpus[0])
	by := map[string]model.Metric{}
	for _, m := range ms {
		by[m.Key] = m
	}
	// 2048/12288 = 16.67% -> 17
	if by["gpu_memory_used"].Value != 17 {
		t.Fatalf("mem used pct: %v", by["gpu_memory_used"].Value)
	}
	if by["gpu_temperature"].Component != "gpu-0" || by["gpu_temperature"].ComponentName != "GPU 0" {
		t.Fatalf("sub-device: %+v", by["gpu_temperature"])
	}
	// no fan metric for a card whose fan is N/A would be absent; here g0 has a fan
	if _, ok := by["gpu_fan"]; !ok {
		t.Fatal("g0 should have gpu_fan")
	}
}
```
(Add `"github.com/giovi321/server-status/internal/model"` to the test imports.)

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run "NvidiaSMI|GpuMetrics"'`
Expected: FAIL, `undefined: parseNvidiaSMI`.

- [ ] **Step 3: Implement**

Create `internal/collector/gpu.go`:
```go
package collector

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// GpuInfo is one GPU parsed from nvidia-smi CSV. Optional fields are pointers.
type GpuInfo struct {
	Index, Name            string
	Temp, Util             int
	MemTotalMB, MemUsedMB  int
	Power, PowerLimit      *float64
	Fan                    *int
	Driver                 string
}

func naField(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s == "[N/A]" || s == "[Not Supported]" || s == "[Unknown Error]"
}

// parseNvidiaSMI parses `nvidia-smi --query-gpu=index,name,temperature.gpu,
// utilization.gpu,memory.total,memory.used,power.draw,power.limit,fan.speed,
// driver_version --format=csv,noheader,nounits` output (one GPU per line).
func parseNvidiaSMI(csv string) []GpuInfo {
	var out []GpuInfo
	for _, line := range strings.Split(csv, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, ",")
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		if len(f) < 10 {
			continue
		}
		g := GpuInfo{Index: f[0], Name: f[1], Driver: f[9]}
		g.Temp, _ = strconv.Atoi(f[2])
		g.Util, _ = strconv.Atoi(f[3])
		g.MemTotalMB, _ = strconv.Atoi(f[4])
		g.MemUsedMB, _ = strconv.Atoi(f[5])
		if !naField(f[6]) {
			if v, err := strconv.ParseFloat(f[6], 64); err == nil {
				g.Power = &v
			}
		}
		if !naField(f[7]) {
			if v, err := strconv.ParseFloat(f[7], 64); err == nil {
				g.PowerLimit = &v
			}
		}
		if !naField(f[8]) {
			if v, err := strconv.Atoi(f[8]); err == nil {
				g.Fan = &v
			}
		}
		out = append(out, g)
	}
	return out
}

func gpuMetrics(g GpuInfo) []model.Metric {
	comp := "gpu-" + g.Index
	name := "GPU " + g.Index
	sensor := func(key, leaf string, val any, unit, dc string) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Unit: unit, DeviceClass: dc, StateClass: "measurement", Kind: model.KindSensor, Category: "primary"}
	}
	out := []model.Metric{
		sensor("gpu_temperature", "Temperature", g.Temp, "°C", "temperature"),
		sensor("gpu_utilization", "Utilization", g.Util, "%", ""),
	}
	if g.MemTotalMB > 0 {
		pctUsed := int(float64(g.MemUsedMB)*100.0/float64(g.MemTotalMB) + 0.5)
		out = append(out, sensor("gpu_memory_used", "Memory used", pctUsed, "%", ""))
	}
	if g.Power != nil {
		out = append(out, sensor("gpu_power", "Power", *g.Power, "W", "power"))
	}
	if g.PowerLimit != nil {
		out = append(out, model.Metric{Key: "gpu_power_limit", Component: comp, ComponentName: name, Name: "Power limit", Value: *g.PowerLimit, Unit: "W", DeviceClass: "power", Kind: model.KindSensor, Category: "diagnostic"})
	}
	if g.Fan != nil {
		out = append(out, sensor("gpu_fan", "Fan", *g.Fan, "%", ""))
	}
	if g.Name != "" {
		out = append(out, model.Metric{Key: "gpu_name", Component: comp, ComponentName: name, Name: "Name", Value: g.Name, Kind: model.KindText, Category: "diagnostic"})
	}
	if g.Driver != "" {
		out = append(out, model.Metric{Key: "gpu_driver", Component: comp, ComponentName: name, Name: "Driver", Value: g.Driver, Kind: model.KindText, Category: "diagnostic"})
	}
	return out
}

func nvidiaSMIPath() string {
	for _, p := range []string{"/usr/bin/nvidia-smi", "/bin/nvidia-smi"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

const nvidiaQuery = "index,name,temperature.gpu,utilization.gpu,memory.total,memory.used,power.draw,power.limit,fan.speed,driver_version"

// Gpu publishes per-GPU metrics as sub-devices via nvidia-smi.
type Gpu struct{}

func (Gpu) Name() string { return "gpu" }

func (Gpu) Available() bool { return nvidiaSMIPath() != "" }

func (Gpu) Collect(ctx context.Context) ([]model.Metric, error) {
	bin := nvidiaSMIPath()
	if bin == "" {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, bin, "--query-gpu="+nvidiaQuery, "--format=csv,noheader,nounits").Output()
	if err != nil && len(out) == 0 {
		return nil, nil
	}
	var metrics []model.Metric
	for _, g := range parseNvidiaSMI(string(out)) {
		metrics = append(metrics, gpuMetrics(g)...)
	}
	return metrics, nil
}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/gpu.go && go build ./... && go test ./internal/collector/ -run "NvidiaSMI|GpuMetrics" && go vet ./...'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/gpu.go internal/collector/gpu_test.go && git commit -F - <<'EOF'
feat: nvidia GPU collector (per-card sub-devices)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: ZFS collector

**Files:**
- Create: `internal/collector/zfs.go`, `internal/collector/zfs_test.go`

**Interfaces:**
- Produces: `collector.ZpoolInfo{Name, Health string; CapacityPct int; FragPct int; HasFrag bool}`; `collector.parseZpoolList(out string) []ZpoolInfo`; `collector.zfsMetrics(z ZpoolInfo) []model.Metric`; `collector.Zfs{}` implementing `Collector`

- [ ] **Step 1: Write the failing test**

Create `internal/collector/zfs_test.go`:
```go
package collector

import "testing"

// `zpool list -H -o name,health,cap,frag` (tab-separated, no header).
const zpoolListOut = "tank\tONLINE\t52%\t3%\nbackup\tDEGRADED\t78%\t-\n"

func TestParseZpoolList(t *testing.T) {
	pools := parseZpoolList(zpoolListOut)
	by := map[string]ZpoolInfo{}
	for _, p := range pools {
		by[p.Name] = p
	}
	tank, ok := by["tank"]
	if !ok || tank.Health != "ONLINE" || tank.CapacityPct != 52 || !tank.HasFrag || tank.FragPct != 3 {
		t.Fatalf("tank: %+v ok=%v", tank, ok)
	}
	backup, ok := by["backup"]
	if !ok || backup.Health != "DEGRADED" || backup.CapacityPct != 78 || backup.HasFrag {
		t.Fatalf("backup: %+v (frag '-' should mean HasFrag=false)", backup)
	}
}

func TestZfsMetricsDegraded(t *testing.T) {
	pools := parseZpoolList(zpoolListOut)
	var backup ZpoolInfo
	for _, p := range pools {
		if p.Name == "backup" {
			backup = p
		}
	}
	ms := zfsMetrics(backup)
	by := map[string]model.Metric{}
	for _, m := range ms {
		by[m.Key] = m
	}
	if by["pool_degraded"].Value != true {
		t.Fatal("DEGRADED pool must report pool_degraded=true")
	}
	if by["pool_health"].Component != "pool-backup" || by["pool_health"].ComponentName != "Pool backup" {
		t.Fatalf("sub-device: %+v", by["pool_health"])
	}
	// frag '-' -> no fragmentation metric
	if _, ok := by["pool_fragmentation"]; ok {
		t.Fatal("backup has no fragmentation ('-') so no metric")
	}
}
```
(Add the `model` import to the test.)

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run "ZpoolList|ZfsMetrics"'`
Expected: FAIL, `undefined: parseZpoolList`.

- [ ] **Step 3: Implement**

Create `internal/collector/zfs.go`:
```go
package collector

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// ZpoolInfo is one ZFS pool from `zpool list -H -o name,health,cap,frag`.
type ZpoolInfo struct {
	Name, Health string
	CapacityPct  int
	FragPct      int
	HasFrag      bool
}

func pctField(s string) (int, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" || s == "-" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseZpoolList parses `zpool list -H -o name,health,cap,frag` (tab-separated).
func parseZpoolList(out string) []ZpoolInfo {
	var pools []ZpoolInfo
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		z := ZpoolInfo{Name: f[0], Health: f[1]}
		if cap, ok := pctField(f[2]); ok {
			z.CapacityPct = cap
		}
		if frag, ok := pctField(f[3]); ok {
			z.FragPct = frag
			z.HasFrag = true
		}
		pools = append(pools, z)
	}
	return pools
}

func zfsMetrics(z ZpoolInfo) []model.Metric {
	comp := "pool-" + z.Name
	name := "Pool " + z.Name
	out := []model.Metric{
		{Key: "pool_health", Component: comp, ComponentName: name, Name: "Health", Value: z.Health, Kind: model.KindText, Category: "primary"},
		{Key: "pool_degraded", Component: comp, ComponentName: name, Name: "Degraded", Value: z.Health != "ONLINE", DeviceClass: "problem", Kind: model.KindBinarySensor, Category: "primary"},
		{Key: "pool_capacity", Component: comp, ComponentName: name, Name: "Capacity", Value: z.CapacityPct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary"},
	}
	if z.HasFrag {
		out = append(out, model.Metric{Key: "pool_fragmentation", Component: comp, ComponentName: name, Name: "Fragmentation", Value: z.FragPct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"})
	}
	return out
}

func zpoolPath() string {
	for _, p := range []string{"/usr/sbin/zpool", "/sbin/zpool", "/usr/bin/zpool"} {
		if fi, err := statFile(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func readZpools(ctx context.Context) []ZpoolInfo {
	bin := zpoolPath()
	if bin == "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, bin, "list", "-H", "-o", "name,health,cap,frag").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return parseZpoolList(string(out))
}

// Zfs publishes per-pool ZFS health as sub-devices.
type Zfs struct{}

func (Zfs) Name() string { return "zfs" }

func (Zfs) Available() bool { return zpoolPath() != "" }

func (Zfs) Collect(ctx context.Context) ([]model.Metric, error) {
	var out []model.Metric
	for _, z := range readZpools(ctx) {
		out = append(out, zfsMetrics(z)...)
	}
	return out, nil
}
```
Note: `statFile` is a small `os.Stat` wrapper — if it does not already exist in the package, replace `statFile(p)` with `os.Stat(p)` and add the `os` import (mirror how `apt.go`/`systemd.go`/`smart.go` do their path lookups with `os.Stat`). Prefer the simplest form that compiles.

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/zfs.go && go build ./... && go test ./internal/collector/ -run "ZpoolList|ZfsMetrics" && go vet ./...'`
Expected: build clean (fix the `statFile` note if it does not compile), PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/zfs.go internal/collector/zfs_test.go && git commit -F - <<'EOF'
feat: ZFS pool collector (per-pool sub-devices)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 3: Register GPU + ZFS and gate

**Files:**
- Modify: `internal/detect/detect.go`

**Interfaces:**
- Produces: `detect.All(cfg)` returning the existing collectors plus `collector.Gpu{}` and `collector.Zfs{}`

- [ ] **Step 1: Register the collectors**

In `internal/detect/detect.go`, add `collector.Gpu{}` and `collector.Zfs{}` to the `All(cfg)` slice after `collector.Mdadm{}` (keep all existing collectors and order).

- [ ] **Step 2: Full gate**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ && go build ./... && go vet ./... && go test ./... && gofmt -l .'`
Expected: build/vet clean, all tests pass, gofmt empty.

- [ ] **Step 3: Live dump-detected on WSL (gpu/zfs available:false expected)**

Run:
```bash
wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go build -o server-status ./cmd/server-status && printf "node: wsltest\nsinks:\n  - type: mqtt\n    host: 127.0.0.1\n" > /tmp/ss.yaml && ./server-status -c /tmp/ss.yaml --dump-detected | grep -E "\"name\": \"(gpu|zfs)\"" -A1; rm -f /tmp/ss.yaml'
```
Expected: `gpu` and `zfs` appear with `available: false` on WSL (no nvidia-smi, no zpool) — correct autodetection. Confirm no collector error.

- [ ] **Step 4: Commit**

```bash
cd "Z:/git/server-status" && git add internal/detect/detect.go && git commit -F - <<'EOF'
feat: register GPU and ZFS collectors

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

## Self-review against the spec

Spec Phase 4/5 remainder: GPU (nvidia) → Task 1; ZFS pools → Task 2; registration → Task 3. btrfs is explicitly deferred to an optional later plan (its CLI output is messy and it is the least common on the target hosts); note it in the roadmap.

Sub-device usage: GPUs and pools both emit `Component`/`ComponentName` so they render as HA sub-devices under the host (Plan 03 mechanism), consistent with SMART/mdadm from Plan 04.

Absent-field correctness: nvidia `[N/A]`/`[Not Supported]` fan/power and zpool `-` fragmentation are omitted (pointer/HasFrag), not published as 0 — asserted by the tests.

Live validation: deferred to real hardware; Task 3 confirms both collectors autodetect to `available: false` on WSL.

Placeholder scan: every code and test step contains complete content. The nvidia-smi CSV and zpool list layouts are the documented `--format=csv,noheader,nounits` and `-H -o` forms; a field-order tweak may be needed against a specific driver/zfs version on the real server, which is expected for a deferred-live-validation collector.

## Roadmap: subsequent plans

- Plan 06: docker (registry digest compare, container inventory, compose awareness), docker as a sub-device
- Plan 07: webhook sink + HTTP control surface, parity golden tests
- Plan 08: control commands + GitHub-Releases self-update, HA update entity, release pipeline
- Plan 09: reliability hardening + migration cutover
- Optional: btrfs pool/device-stats collector
