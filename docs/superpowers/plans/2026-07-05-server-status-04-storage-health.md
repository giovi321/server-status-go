# server-status storage health (Plan 04) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the first real sub-device collectors: SMART per physical disk (health, temperature, curated attributes, serial/model, with config aliases and a curated/full toggle) and mdadm per RAID array, each published as its own Home Assistant sub-device under the host. Thread config into the collector registry so collectors that need it (SMART) can read it.

**Architecture:** Two new collectors in `internal/collector`, both emitting `Component`/`ComponentName` sub-device metrics (Plan 03 mechanism). SMART parses `smartctl --json -a` (handling both ATA and NVMe layouts) and caches results in memory on a min-interval so it does not wake drives every cycle. `detect.All` becomes `detect.All(cfg)` so `NewSmart(cfg)` can read disk aliases and the `smart_attributes` setting. GPU, ZFS, and btrfs are deferred to the next plan.

**Tech Stack:** Go 1.22+, existing deps (uses `os/exec` for smartctl/mdadm, `encoding/json` for smartctl JSON). Parsers are fixture-tested; live hardware validation happens on a real Debian server at deploy time (WSL has no physical disks).

## Global Constraints

- Build/test only in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows with the two-line `giovi321` / `Claude-Session:` footer.
- `gofmt -w` new/changed Go files before committing; `gofmt -l .` stays empty.
- All metrics stay canonical snake_case. SMART keys: `disk_health` (binary_sensor, problem — ON when NOT passed), `disk_temperature`, `disk_power_on_hours`, `disk_power_cycles`, `disk_reallocated_sectors`, `disk_pending_sectors`, `disk_crc_errors`, `disk_percentage_used`, `disk_available_spare`, `disk_media_errors`, `disk_unsafe_shutdowns`, `disk_data_written`, and diagnostic text `disk_serial`, `disk_model`, `disk_firmware`, `disk_capacity`, `disk_rotation` (+ `disk_smart_raw` when `smart_attributes: full`). mdadm keys: `raid_state` (text), `raid_degraded` (binary_sensor, problem), `raid_active_devices`, `raid_total_devices`, `raid_failed_devices`, `raid_resync_progress`.
- Each disk is a sub-device: `Component = "disk-" + slug(serial-or-wwn-or-name)`, `ComponentName = cfg.DiskName(serial, "Disk "+name)`. Each array is a sub-device: `Component = "raid-" + name`, `ComponentName = "RAID " + name`. Entity `Name` is the short leaf ("Temperature", "Health", ...). Diagnostic entities carry `Category: "diagnostic"`.
- SMART attributes that are absent for a given device (ATA vs NVMe differ) MUST be omitted, not published as 0. Use presence to decide.
- SMART must not run smartctl every cycle: cache in memory with a min-interval (default 1800s), first cycle runs it.
- Non-regression: existing collectors and host-level output unchanged. `detect.All(cfg)` must keep returning the existing collectors plus the two new ones.
- No GPU/ZFS/btrfs, no docker, no webhook/HTTP/self-update here.

## Prerequisites

- Plans 01-03 complete on `main` (repo giovi321/server-status-go). `model.Metric` has `Component`/`ComponentName`/`Instance`; `ha.Discovery` supports sub-devices; `config.DiskName` and `config.Config.Disks` exist.
- `smartctl` (smartmontools) and `mdadm` present on the target server. Not required in WSL (parsers are fixture-tested).

## File structure

```
internal/collector/smartparse.go        # CREATE: SmartInfo + parseSmartctl + physicalDisks
internal/collector/smartparse_test.go    # CREATE: ATA + NVMe fixtures
internal/collector/smart.go              # CREATE: Smart collector (NewSmart(cfg), min-interval cache)
internal/collector/mdadm.go              # CREATE: parseMdstat + Mdadm collector
internal/collector/mdadm_test.go         # CREATE: mdstat fixtures
internal/config/config.go                # MODIFY: add SmartAttributes, SmartInterval
internal/config/config_test.go           # MODIFY: smart config test
internal/detect/detect.go                # MODIFY: All(cfg) threads config; register Smart+Mdadm
internal/detect/detect_test.go           # MODIFY: All(cfg) call
cmd/server-status/main.go                # MODIFY: pass cfg to detect.All
```

---

### Task 1: Disk enumeration and smartctl parser

**Files:**
- Create: `internal/collector/smartparse.go`, `internal/collector/smartparse_test.go`

**Interfaces:**
- Produces: `collector.physicalDisks(sysBlockRoot string) []string` (whole disks under /sys/block, excluding loop/ram/dm/md/sr/zram and partitions); `collector.SmartInfo` (fields below, optional numeric attrs as `*int64`/`*int`); `collector.parseSmartctl(data []byte) (SmartInfo, error)` handling ATA and NVMe

- [ ] **Step 1: Write the failing tests with realistic fixtures**

Create `internal/collector/smartparse_test.go`:
```go
package collector

import "testing"

// Trimmed but realistic `smartctl --json -a` output for an ATA HDD.
const smartctlATA = `{
  "model_name": "WDC WD40EFRX-68N32N0",
  "serial_number": "WD-WCC7K1234567",
  "firmware_version": "82.00A82",
  "user_capacity": {"bytes": 4000787030016},
  "rotation_rate": 5400,
  "smart_status": {"passed": true},
  "temperature": {"current": 34},
  "ata_smart_attributes": {"table": [
    {"id": 5,   "name": "Reallocated_Sector_Ct",   "raw": {"value": 0}},
    {"id": 9,   "name": "Power_On_Hours",           "raw": {"value": 21987}},
    {"id": 12,  "name": "Power_Cycle_Count",        "raw": {"value": 142}},
    {"id": 197, "name": "Current_Pending_Sector",   "raw": {"value": 0}},
    {"id": 198, "name": "Offline_Uncorrectable",    "raw": {"value": 0}},
    {"id": 199, "name": "UDMA_CRC_Error_Count",     "raw": {"value": 3}}
  ]}
}`

// Trimmed `smartctl --json -a` output for an NVMe SSD.
const smartctlNVMe = `{
  "model_name": "Samsung SSD 980 PRO 1TB",
  "serial_number": "S5GXNX0R123456",
  "firmware_version": "5B2QGXA7",
  "user_capacity": {"bytes": 1000204886016},
  "rotation_rate": 0,
  "smart_status": {"passed": true},
  "temperature": {"current": 41},
  "nvme_smart_health_information_log": {
    "percentage_used": 4,
    "available_spare": 100,
    "media_errors": 0,
    "unsafe_shutdowns": 12,
    "power_on_hours": 5123,
    "power_cycles": 88,
    "data_units_written": 41231234
  }
}`

func TestParseSmartctlATA(t *testing.T) {
	si, err := parseSmartctl([]byte(smartctlATA))
	if err != nil {
		t.Fatal(err)
	}
	if si.Model != "WDC WD40EFRX-68N32N0" || si.Serial != "WD-WCC7K1234567" {
		t.Fatalf("id: %+v", si)
	}
	if !si.Passed || si.CapacityBytes != 4000787030016 || si.RotationRate != 5400 {
		t.Fatalf("common: %+v", si)
	}
	if si.Temperature == nil || *si.Temperature != 34 {
		t.Fatalf("temp: %v", si.Temperature)
	}
	if si.PowerOnHours == nil || *si.PowerOnHours != 21987 {
		t.Fatalf("poh: %v", si.PowerOnHours)
	}
	if si.CRCErrors == nil || *si.CRCErrors != 3 {
		t.Fatalf("crc: %v", si.CRCErrors)
	}
	// NVMe-only attributes must be absent for an ATA disk
	if si.PercentageUsed != nil || si.AvailableSpare != nil {
		t.Fatalf("nvme attrs should be nil for ATA: %+v", si)
	}
}

func TestParseSmartctlNVMe(t *testing.T) {
	si, err := parseSmartctl([]byte(smartctlNVMe))
	if err != nil {
		t.Fatal(err)
	}
	if si.RotationRate != 0 {
		t.Fatalf("nvme rotation should be 0: %d", si.RotationRate)
	}
	if si.PercentageUsed == nil || *si.PercentageUsed != 4 {
		t.Fatalf("pct used: %v", si.PercentageUsed)
	}
	if si.AvailableSpare == nil || *si.AvailableSpare != 100 {
		t.Fatalf("avail spare: %v", si.AvailableSpare)
	}
	if si.DataWrittenBytes == nil || *si.DataWrittenBytes != 41231234*512*1000 {
		t.Fatalf("data written: %v", si.DataWrittenBytes)
	}
	// ATA-only attributes absent for NVMe
	if si.CRCErrors != nil || si.Reallocated != nil {
		t.Fatalf("ata attrs should be nil for NVMe: %+v", si)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Smartctl'`
Expected: FAIL, `undefined: parseSmartctl`.

- [ ] **Step 3: Implement the parser and enumeration**

Create `internal/collector/smartparse.go`:
```go
package collector

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// SmartInfo is the parsed subset of `smartctl --json -a` we publish.
// Optional attributes are pointers so "absent" is distinct from "zero".
type SmartInfo struct {
	Model, Serial, WWN, Firmware string
	CapacityBytes                int64
	RotationRate                 int
	Passed                       bool
	HasHealth                    bool

	Temperature          *int
	PowerOnHours         *int64
	PowerCycles          *int64
	Reallocated          *int64
	Pending              *int64
	OfflineUncorrectable *int64
	CRCErrors            *int64
	PercentageUsed       *int
	AvailableSpare       *int
	MediaErrors          *int64
	UnsafeShutdowns      *int64
	DataWrittenBytes     *int64
}

type smartRaw struct {
	ModelName    string `json:"model_name"`
	SerialNumber string `json:"serial_number"`
	Firmware     string `json:"firmware_version"`
	WWN          *struct {
		NAA int64 `json:"naa"`
		OUI int64 `json:"oui"`
		ID  int64 `json:"id"`
	} `json:"wwn"`
	UserCapacity struct {
		Bytes int64 `json:"bytes"`
	} `json:"user_capacity"`
	RotationRate int `json:"rotation_rate"`
	SmartStatus  *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature *struct {
		Current int `json:"current"`
	} `json:"temperature"`
	ATA *struct {
		Table []struct {
			ID  int    `json:"id"`
			Raw struct {
				Value int64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	NVMe *struct {
		PercentageUsed  *int   `json:"percentage_used"`
		AvailableSpare  *int   `json:"available_spare"`
		MediaErrors     *int64 `json:"media_errors"`
		UnsafeShutdowns *int64 `json:"unsafe_shutdowns"`
		PowerOnHours    *int64 `json:"power_on_hours"`
		PowerCycles     *int64 `json:"power_cycles"`
		DataUnitsWritten *int64 `json:"data_units_written"`
	} `json:"nvme_smart_health_information_log"`
}

func i64(v int64) *int64 { return &v }
func ip(v int) *int      { return &v }

// parseSmartctl parses `smartctl --json -a` output for ATA or NVMe devices.
func parseSmartctl(data []byte) (SmartInfo, error) {
	var r smartRaw
	if err := json.Unmarshal(data, &r); err != nil {
		return SmartInfo{}, err
	}
	si := SmartInfo{
		Model: r.ModelName, Serial: r.SerialNumber, Firmware: r.Firmware,
		CapacityBytes: r.UserCapacity.Bytes, RotationRate: r.RotationRate,
	}
	if r.SmartStatus != nil {
		si.HasHealth = true
		si.Passed = r.SmartStatus.Passed
	}
	if r.Temperature != nil {
		si.Temperature = ip(r.Temperature.Current)
	}
	if r.ATA != nil {
		for _, a := range r.ATA.Table {
			switch a.ID {
			case 5:
				si.Reallocated = i64(a.Raw.Value)
			case 9:
				si.PowerOnHours = i64(a.Raw.Value)
			case 12:
				si.PowerCycles = i64(a.Raw.Value)
			case 197:
				si.Pending = i64(a.Raw.Value)
			case 198:
				si.OfflineUncorrectable = i64(a.Raw.Value)
			case 199:
				si.CRCErrors = i64(a.Raw.Value)
			}
		}
	}
	if r.NVMe != nil {
		si.PercentageUsed = r.NVMe.PercentageUsed
		si.AvailableSpare = r.NVMe.AvailableSpare
		si.MediaErrors = r.NVMe.MediaErrors
		si.UnsafeShutdowns = r.NVMe.UnsafeShutdowns
		if r.NVMe.PowerOnHours != nil {
			si.PowerOnHours = r.NVMe.PowerOnHours
		}
		if r.NVMe.PowerCycles != nil {
			si.PowerCycles = r.NVMe.PowerCycles
		}
		if r.NVMe.DataUnitsWritten != nil {
			// NVMe data units are 512000-byte units (512 * 1000).
			si.DataWrittenBytes = i64(*r.NVMe.DataUnitsWritten * 512 * 1000)
		}
	}
	if r.WWN != nil {
		si.WWN = formatWWN(r.WWN.NAA, r.WWN.OUI, r.WWN.ID)
	}
	return si, nil
}

func formatWWN(naa, oui, id int64) string {
	// smartctl reports NAA/OUI/ID as separate integers; join as a stable hex string.
	b := strings.Builder{}
	b.WriteString(hex(naa))
	b.WriteString(hex6(oui))
	b.WriteString(hex9(id))
	return b.String()
}

func hex(v int64) string  { return trimHex(v, 1) }
func hex6(v int64) string { return trimHex(v, 6) }
func hex9(v int64) string { return trimHex(v, 9) }

var hexpad = map[int]string{1: "%x", 6: "%06x", 9: "%09x"}

func trimHex(v int64, width int) string {
	// simple fixed-width lowercase hex
	const digits = "0123456789abcdef"
	if v == 0 {
		return strings.Repeat("0", width)
	}
	var out []byte
	for v > 0 {
		out = append([]byte{digits[v&0xf]}, out...)
		v >>= 4
	}
	for len(out) < width {
		out = append([]byte{'0'}, out...)
	}
	return string(out)
}

var partSuffix = regexp.MustCompile(`(p?\d+)$`)

// physicalDisks lists whole physical disks under a /sys/block-style root,
// excluding loop/ram/dm/md/sr/zram devices and partitions.
func physicalDisks(sysBlockRoot string) []string {
	entries, err := os.ReadDir(sysBlockRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") ||
			strings.HasPrefix(n, "dm-") || strings.HasPrefix(n, "md") ||
			strings.HasPrefix(n, "sr") || strings.HasPrefix(n, "zram") {
			continue
		}
		out = append(out, n)
	}
	return out
}
```
Note: the `hexpad` var and `partSuffix` are declared for clarity/future use; if `go vet` or the compiler flags either as unused, delete the unused declaration (`hexpad` is unused by the current `trimHex`; remove it) and re-run.

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/smartparse.go && go build ./... && go test ./internal/collector/ -run Smartctl'`
Expected: build clean (remove any unused decl the compiler flags), tests PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/smartparse.go internal/collector/smartparse_test.go && git commit -F - <<'EOF'
feat: smartctl JSON parser and physical-disk enumeration

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: SMART collector

**Files:**
- Create: `internal/collector/smart.go`
- Modify: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `parseSmartctl`, `physicalDisks`, `SmartInfo`, `config.Config` (DiskName, SmartAttributes)
- Produces: `config.Config.SmartAttributes string` (yaml `smart_attributes`, default "curated"); `collector.NewSmart(cfg config.Config) *collector.Smart` implementing `Collector` (pointer receiver `Collect` for the min-interval cache); `collector.smartMetrics(cfg config.Config, disk string, si SmartInfo) []model.Metric`

- [ ] **Step 1: Add config field + failing config test**

In `internal/config/config.go`, add to `Config`:
```go
	SmartAttributes string `yaml:"smart_attributes"`
```
and in `applyDefaults`, default it:
```go
	if c.SmartAttributes == "" {
		c.SmartAttributes = "curated"
	}
```
Append to `internal/config/config_test.go`:
```go
func TestSmartAttributesDefault(t *testing.T) {
	t.Setenv("TEST_MQTT_PASSWORD", "x")
	cfg, err := Load("testdata/disks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SmartAttributes != "curated" {
		t.Fatalf("default smart_attributes: %q", cfg.SmartAttributes)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/config/ -run SmartAttributes'`
Expected: FAIL (SmartAttributes empty).

- [ ] **Step 3: Implement config default + the SMART collector**

Apply the config change above. Then create `internal/collector/smart.go`:
```go
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/ha"
	"github.com/giovi321/server-status/internal/model"
)

const smartctlBin = "/usr/sbin/smartctl"

// Smart publishes SMART health per physical disk as a sub-device. It caches
// results in memory on a min-interval so it does not wake drives every cycle.
type Smart struct {
	cfg      config.Config
	interval time.Duration
	mu       muLike
	cached   []model.Metric
	cachedAt time.Time
}

// muLike lets Collect run serially without importing sync just for docs; use sync.Mutex.
type muLike = smartMu

func NewSmart(cfg config.Config) *Smart {
	iv := 1800 * time.Second
	return &Smart{cfg: cfg, interval: iv}
}

func smartctlPath() string {
	for _, p := range []string{"/usr/sbin/smartctl", "/sbin/smartctl", "/usr/bin/smartctl"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (Smart) Name() string { return "smart" }

func (Smart) Available() bool {
	return smartctlPath() != "" && len(physicalDisks("/sys/block")) > 0
}

func (s *Smart) Collect(ctx context.Context) ([]model.Metric, error) {
	s.mu.Lock()
	fresh := s.cached != nil && time.Since(s.cachedAt) < s.interval
	cached := s.cached
	s.mu.Unlock()
	if fresh {
		return cached, nil
	}
	bin := smartctlPath()
	if bin == "" {
		return nil, nil
	}
	var out []model.Metric
	for _, disk := range physicalDisks("/sys/block") {
		data, err := exec.CommandContext(ctx, bin, "--json", "-a", "/dev/"+disk).Output()
		if err != nil && len(data) == 0 {
			continue // smartctl exits non-zero with warnings but still prints JSON; skip only if truly empty
		}
		si, perr := parseSmartctl(data)
		if perr != nil {
			continue
		}
		out = append(out, smartMetrics(s.cfg, disk, si)...)
	}
	s.mu.Lock()
	s.cached = out
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return out, nil
}

func smartComponent(disk string, si SmartInfo) string {
	id := si.Serial
	if id == "" {
		id = si.WWN
	}
	if id == "" {
		id = disk
	}
	return "disk-" + ha.InstanceSlug(id)
}

func smartMetrics(cfg config.Config, disk string, si SmartInfo) []model.Metric {
	comp := smartComponent(disk, si)
	name := cfg.DiskName(si.Serial, "Disk "+disk)
	sensor := func(key, leaf string, val any, unit, dc, sc string) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Unit: unit, DeviceClass: dc, StateClass: sc, Kind: model.KindSensor, Category: "primary"}
	}
	diag := func(key, leaf string, val any) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Kind: model.KindText, Category: "diagnostic"}
	}
	var out []model.Metric
	if si.HasHealth {
		out = append(out, model.Metric{Key: "disk_health", Component: comp, ComponentName: name, Name: "Health", Value: !si.Passed, DeviceClass: "problem", Kind: model.KindBinarySensor, Category: "primary"})
	}
	if si.Temperature != nil {
		out = append(out, sensor("disk_temperature", "Temperature", *si.Temperature, "°C", "temperature", "measurement"))
	}
	if si.PowerOnHours != nil {
		out = append(out, sensor("disk_power_on_hours", "Power on hours", *si.PowerOnHours, "h", "", "total_increasing"))
	}
	if si.PowerCycles != nil {
		out = append(out, sensor("disk_power_cycles", "Power cycles", *si.PowerCycles, "", "", "total_increasing"))
	}
	if si.Reallocated != nil {
		out = append(out, sensor("disk_reallocated_sectors", "Reallocated sectors", *si.Reallocated, "", "", "measurement"))
	}
	if si.Pending != nil {
		out = append(out, sensor("disk_pending_sectors", "Pending sectors", *si.Pending, "", "", "measurement"))
	}
	if si.CRCErrors != nil {
		out = append(out, sensor("disk_crc_errors", "CRC errors", *si.CRCErrors, "", "", "measurement"))
	}
	if si.PercentageUsed != nil {
		out = append(out, sensor("disk_percentage_used", "Percentage used", *si.PercentageUsed, "%", "", "measurement"))
	}
	if si.AvailableSpare != nil {
		out = append(out, sensor("disk_available_spare", "Available spare", *si.AvailableSpare, "%", "", "measurement"))
	}
	if si.MediaErrors != nil {
		out = append(out, sensor("disk_media_errors", "Media errors", *si.MediaErrors, "", "", "measurement"))
	}
	if si.UnsafeShutdowns != nil {
		out = append(out, sensor("disk_unsafe_shutdowns", "Unsafe shutdowns", *si.UnsafeShutdowns, "", "", "total_increasing"))
	}
	if si.DataWrittenBytes != nil {
		out = append(out, sensor("disk_data_written", "Data written", *si.DataWrittenBytes, "B", "data_size", "total_increasing"))
	}
	if si.Model != "" {
		out = append(out, diag("disk_model", "Model", si.Model))
	}
	if si.Serial != "" {
		out = append(out, diag("disk_serial", "Serial", si.Serial))
	}
	if si.Firmware != "" {
		out = append(out, diag("disk_firmware", "Firmware", si.Firmware))
	}
	if si.CapacityBytes > 0 {
		out = append(out, model.Metric{Key: "disk_capacity", Component: comp, ComponentName: name, Name: "Capacity", Value: si.CapacityBytes, Unit: "B", DeviceClass: "data_size", Kind: model.KindSensor, Category: "diagnostic"})
	}
	rot := "SSD"
	if si.RotationRate > 0 {
		rot = fmt.Sprintf("%d rpm", si.RotationRate)
	}
	out = append(out, diag("disk_rotation", "Rotation", rot))
	if strings.EqualFold(cfg.SmartAttributes, "full") {
		if raw, err := json.Marshal(si); err == nil {
			out = append(out, diag("disk_smart_raw", "SMART raw", string(raw)))
		}
	}
	return out
}
```
Add `internal/collector/smart_mu.go` (a tiny file so the collector file stays focused):
```go
package collector

import "sync"

// smartMu is an alias so smart.go reads clearly; it is a standard mutex.
type smartMu = sync.Mutex
```
Note: if the `muLike`/`smartMu` alias indirection trips gofmt/vet or feels over-engineered to the implementer, replace `mu muLike` with a plain `mu sync.Mutex` field in `Smart` and `import "sync"` directly in smart.go, and delete smart_mu.go. Prefer whichever is simpler; the requirement is only that `s.mu.Lock()/Unlock()` guards `cached`/`cachedAt`.

- [ ] **Step 4: gofmt, build, run**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/ internal/config/ && go build ./... && go test ./internal/collector/ ./internal/config/ && go vet ./...'`
Expected: build clean, tests pass. (The collector's live smartctl path is exercised on a real server; here the parser is unit-tested and the collector compiles + `smartMetrics` can be unit-tested — see step 5.)

- [ ] **Step 5: Add a smartMetrics unit test**

Append to `internal/collector/smartparse_test.go`:
```go
import "github.com/giovi321/server-status/internal/config" // add to the import block if not present

func TestSmartMetricsCuratedNVMe(t *testing.T) {
	si, _ := parseSmartctl([]byte(smartctlNVMe))
	cfg := config.Config{SmartAttributes: "curated", Disks: map[string]string{"S5GXNX0R123456": "OS drive"}}
	ms := smartMetrics(cfg, "nvme0n1", si)
	keys := map[string]model.Metric{}
	for _, m := range ms {
		keys[m.Key] = m
	}
	if _, ok := keys["disk_percentage_used"]; !ok {
		t.Fatal("nvme should emit disk_percentage_used")
	}
	if _, ok := keys["disk_crc_errors"]; ok {
		t.Fatal("nvme should NOT emit ata-only disk_crc_errors")
	}
	// alias applied and component derived from serial
	if keys["disk_temperature"].ComponentName != "OS drive" {
		t.Fatalf("alias: %q", keys["disk_temperature"].ComponentName)
	}
	if keys["disk_temperature"].Component != "disk-s5gxnx0r123456" {
		t.Fatalf("component: %q", keys["disk_temperature"].Component)
	}
	// no raw dump in curated mode
	if _, ok := keys["disk_smart_raw"]; ok {
		t.Fatal("curated mode must not emit disk_smart_raw")
	}
}
```
Add `"github.com/giovi321/server-status/internal/model"` to the test imports if needed. Run:
`wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/ && go test ./internal/collector/ -run "Smartctl|SmartMetrics"'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/smart.go internal/collector/smart_mu.go internal/collector/smartparse_test.go internal/config/ && git commit -F - <<'EOF'
feat: SMART collector (per-disk sub-devices, curated/full, aliases, cached)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```
(If you dropped smart_mu.go per the note, adjust the `git add` accordingly.)

---

### Task 3: mdadm RAID collector

**Files:**
- Create: `internal/collector/mdadm.go`, `internal/collector/mdadm_test.go`

**Interfaces:**
- Produces: `collector.RaidArray{Name, Level, State string; Active, Total, Failed int; ResyncPct int; ResyncActive bool}`; `collector.parseMdstat(data string) []RaidArray`; `collector.Mdadm{}` implementing `Collector`, emitting per-array sub-device metrics

- [ ] **Step 1: Write the failing test**

Create `internal/collector/mdadm_test.go`:
```go
package collector

import "testing"

// /proc/mdstat: a healthy md0 (raid1) and a degraded+resyncing md1 (raid5).
const mdstatFixture = `Personalities : [raid1] [raid6] [raid5] [raid4]
md0 : active raid1 sdb1[1] sda1[0]
      3906887168 blocks super 1.2 [2/2] [UU]

md1 : active raid5 sdd1[3] sdc1[1] sde1[2]
      7813772800 blocks super 1.2 level 5, 512k chunk, algorithm 2 [4/3] [UU_U]
      [==========>..........]  recovery = 52.3% (1234567/2345678) finish=100.0min speed=100000K/sec

unused devices: <none>
`

func TestParseMdstat(t *testing.T) {
	arrays := parseMdstat(mdstatFixture)
	by := map[string]RaidArray{}
	for _, a := range arrays {
		by[a.Name] = a
	}
	md0, ok := by["md0"]
	if !ok || md0.Level != "raid1" || md0.Total != 2 || md0.Active != 2 || md0.Failed != 0 {
		t.Fatalf("md0: %+v ok=%v", md0, ok)
	}
	md1, ok := by["md1"]
	if !ok || md1.Total != 4 || md1.Active != 3 || md1.Failed != 1 {
		t.Fatalf("md1: %+v", md1)
	}
	if !md1.ResyncActive || md1.ResyncPct != 52 {
		t.Fatalf("md1 resync: active=%v pct=%d", md1.ResyncActive, md1.ResyncPct)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run Mdstat'`
Expected: FAIL, `undefined: parseMdstat`.

- [ ] **Step 3: Implement**

Create `internal/collector/mdadm.go`:
```go
package collector

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// RaidArray is one mdadm array parsed from /proc/mdstat.
type RaidArray struct {
	Name, Level, State string
	Active, Total      int
	Failed             int
	ResyncPct          int
	ResyncActive       bool
}

var (
	mdHeader   = regexp.MustCompile(`^(md\d+)\s*:\s*(\S+)\s+(\S+)\s`)
	mdCounts   = regexp.MustCompile(`\[(\d+)/(\d+)\]`)
	mdRecovery = regexp.MustCompile(`(recovery|resync|reshape|check)\s*=\s*(\d+)(?:\.\d+)?%`)
)

// parseMdstat parses /proc/mdstat into a slice of arrays.
func parseMdstat(data string) []RaidArray {
	var arrays []RaidArray
	var cur *RaidArray
	flush := func() {
		if cur != nil {
			arrays = append(arrays, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(data, "\n") {
		if m := mdHeader.FindStringSubmatch(line); m != nil {
			flush()
			cur = &RaidArray{Name: m[1], State: m[2], Level: m[3]}
			continue
		}
		if cur == nil {
			continue
		}
		if m := mdCounts.FindStringSubmatch(line); m != nil {
			cur.Total, _ = strconv.Atoi(m[1])
			cur.Active, _ = strconv.Atoi(m[2])
			if cur.Total >= cur.Active {
				cur.Failed = cur.Total - cur.Active
			}
		}
		if m := mdRecovery.FindStringSubmatch(line); m != nil {
			cur.ResyncActive = true
			cur.ResyncPct, _ = strconv.Atoi(m[2])
		}
	}
	flush()
	return arrays
}

func raidMetrics(a RaidArray) []model.Metric {
	comp := "raid-" + a.Name
	name := "RAID " + a.Name
	m := func(key, leaf string, val any, kind model.Kind, dc, cat string) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: val, Kind: kind, DeviceClass: dc, Category: cat}
	}
	out := []model.Metric{
		m("raid_state", "State", a.State, model.KindText, "", "primary"),
		m("raid_degraded", "Degraded", a.Failed > 0, model.KindBinarySensor, "problem", "primary"),
		{Key: "raid_active_devices", Component: comp, ComponentName: name, Name: "Active devices", Value: a.Active, StateClass: "measurement", Kind: model.KindSensor, Category: "primary"},
		{Key: "raid_total_devices", Component: comp, ComponentName: name, Name: "Total devices", Value: a.Total, StateClass: "measurement", Kind: model.KindSensor, Category: "diagnostic"},
		{Key: "raid_failed_devices", Component: comp, ComponentName: name, Name: "Failed devices", Value: a.Failed, StateClass: "measurement", Kind: model.KindSensor, Category: "primary"},
	}
	if a.ResyncActive {
		out = append(out, model.Metric{Key: "raid_resync_progress", Component: comp, ComponentName: name, Name: "Resync progress", Value: a.ResyncPct, Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary"})
	}
	return out
}

func readMdstat() []RaidArray {
	data, err := os.ReadFile("/proc/mdstat")
	if err != nil {
		return nil
	}
	return parseMdstat(string(data))
}

// Mdadm publishes mdadm RAID array health as sub-devices.
type Mdadm struct{}

func (Mdadm) Name() string { return "mdadm" }

func (Mdadm) Available() bool { return len(readMdstat()) > 0 }

func (Mdadm) Collect(ctx context.Context) ([]model.Metric, error) {
	var out []model.Metric
	for _, a := range readMdstat() {
		out = append(out, raidMetrics(a)...)
	}
	return out, nil
}
```

- [ ] **Step 4: gofmt, run**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/mdadm.go && go build ./... && go test ./internal/collector/ -run Mdstat'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/mdadm.go internal/collector/mdadm_test.go && git commit -F - <<'EOF'
feat: mdadm RAID collector (per-array sub-devices)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 4: Thread config into detect.All and register

**Files:**
- Modify: `internal/detect/detect.go`, `internal/detect/detect_test.go`, `cmd/server-status/main.go`

**Interfaces:**
- Produces: `detect.All(cfg config.Config) []collector.Collector` returning the existing collectors plus `NewSmart(cfg)` and `collector.Mdadm{}`

- [ ] **Step 1: Change All to take config and register the new collectors**

In `internal/detect/detect.go`, add the import for `config` and change `All`:
```go
func All(cfg config.Config) []collector.Collector {
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
		collector.NewSmart(cfg),
		collector.Mdadm{},
	}
}
```

- [ ] **Step 2: Update callers**

In `cmd/server-status/main.go`, change `detect.Available(detect.All())` to `detect.Available(detect.All(cfg))` (there are two call sites: the dump-detected branch and the run-loop `cols`; the dump path uses `detect.All(cfg)` directly per Plan 01's fix, so update both `detect.All()` occurrences to `detect.All(cfg)`).

- [ ] **Step 3: Fix the detect test if needed**

`internal/detect/detect_test.go` uses fake collectors via `availableFrom`/`snapshotFrom`, not `All()`, so it should be unaffected. If any test references `All()` with no args, update it to `All(config.Config{})` and add the config import.

- [ ] **Step 4: gofmt, full gate**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ cmd/ && go build ./... && go vet ./... && go test ./... && gofmt -l .'`
Expected: build/vet clean, all tests pass, gofmt empty.

- [ ] **Step 5: Live dump-detected on WSL (no disks expected)**

Run:
```bash
wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go build -o server-status ./cmd/server-status && printf "node: wsltest\nsinks:\n  - type: mqtt\n    host: 127.0.0.1\n" > /tmp/ss.yaml && ./server-status -c /tmp/ss.yaml --dump-detected | grep -E "\"name\": \"(smart|mdadm)\"" -A1; rm -f /tmp/ss.yaml'
```
Expected: `smart` and `mdadm` appear with `available: false` on WSL (no smartctl-capable disks, no /proc/mdstat arrays) — correct autodetection. Confirm no collector error and the binary still builds/runs.

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/detect/ cmd/server-status/main.go && git commit -F - <<'EOF'
feat: thread config into detect.All and register SMART + mdadm collectors

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

## Self-review against the spec

Spec Phase 4 (storage): SMART curated/full with serials + aliases → Tasks 1-2; mdadm → Task 3; registration + config threading → Task 4. ZFS, btrfs, and GPU are explicitly deferred to the next plan (Plan 05) to keep this plan focused and shippable.

Sub-device usage: SMART disks and mdadm arrays both emit `Component`/`ComponentName` so they render as HA sub-devices under the host (Plan 03 mechanism). Disk aliases flow through `cfg.DiskName`.

Absent-attribute correctness: `SmartInfo` uses pointers so ATA-only / NVMe-only attributes are omitted rather than published as 0 — asserted by `TestParseSmartctlATA`/`NVMe` and `TestSmartMetricsCuratedNVMe`.

Live validation: deferred to real hardware at deploy time (WSL has no physical disks or arrays); Task 4 confirms both collectors autodetect to `available: false` on WSL, which is correct.

Placeholder scan: every code and test step contains complete content. The smartctl JSON fixtures are realistic subsets of the documented schema; the parser may need a minor field tweak against a specific smartctl version on the real server, which is expected for a deferred-live-validation collector.

## Roadmap: subsequent plans

- Plan 05: GPU (nvidia), ZFS pools, btrfs — the remaining storage/accelerator collectors (sub-devices)
- Plan 06: docker (registry digest compare, container inventory, compose awareness), docker as a sub-device
- Plan 07: webhook sink + HTTP control surface, parity golden tests
- Plan 08: control commands + GitHub-Releases self-update, HA update entity, release pipeline
- Plan 09: reliability hardening + migration cutover
