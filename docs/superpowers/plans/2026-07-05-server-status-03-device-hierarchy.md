# server-status device hierarchy (Plan 03) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the Home Assistant sub-device mechanism so a metric with a `Component` becomes its own HA device (disk, GPU, RAID array, docker) linked to the host via `via_device`, gated by a `hierarchy: grouped|flat` setting, plus a disk-alias config map. Host-to-host parent nesting already works from Plan 01.

**Architecture:** Pure discovery-layer work. `model.Metric` gains a `ComponentName` (sub-device friendly name); `model.Device` gains `Hierarchy`. The `ha.Discovery` builder, when a metric has a `Component` and hierarchy is `grouped` (default), emits a device block representing the SUB-DEVICE (its own identifiers + `via_device` = host) instead of the host device block. No new collectors here (Plans 04-05 emit the first real sub-device metrics); the mechanism is unit/golden tested and live-validated with a synthetic sub-device.

**Tech Stack:** Go 1.22+, existing deps. Tests use fixtures and golden JSON under `internal/ha/testdata/`.

## Global Constraints

- Build/test only run in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows; commit messages end with the two-line `giovi321` / `Claude-Session:` footer.
- Run `gofmt -w` on new/changed Go files before committing; `gofmt -l .` must stay empty.
- CRITICAL non-regression: when a metric has NO `Component` (all current cpu/memory/uptime/load/swap/filesystem/network/temperature/apt/systemd metrics are host-level), the discovery payload MUST be byte-identical to today's output. Only `Component != "" && Hierarchy != "flat"` changes the device block. Any change to host-level entities would orphan every existing HA entity.
- Sub-device identifier scheme: a sub-device's identifier is `<host-identifier>-<component>` (e.g. `server-status-gc01srvr-disk-wd1234`). Its `via_device` is the host identifier `server-status-<node>`. Its display name is `<host display name> <ComponentName>` (e.g. `gc01srvr Disk sda`); entity names stay the short leaf (`Temperature`).
- `hierarchy: flat` makes a Component'd metric attach to the host device (Component still used for entity uniqueness in unique_id/object_id/state_topic, but the device block is the host).
- Host-to-host parent (`dev.Parent` → host device `via_device = server-status-<parent>`) is unchanged from Plan 01 and applies only to HOST-level device blocks, producing a sub-device → host → parent-host chain.
- No new collectors, no webhook/HTTP/self-update. Disk aliasing adds config + a resolver only (consumed by Plan 04's disk collector).

## Prerequisites

- Plans 01-02 complete on `main` (repo giovi321/server-status-go). Packages `model`, `config`, `ident`, `ha`, `sink`, `collector`, `detect` exist and are green.
- For live validation: the MQTT broker + Home Assistant reachable (controller-run).

## File structure

```
internal/model/metric.go       # MODIFY: add ComponentName; add Device.Hierarchy
internal/config/config.go      # MODIFY: add Disks map + DiskName resolver
internal/config/config_test.go # MODIFY: disks-map test
internal/ident/ident.go        # MODIFY: set Device.Hierarchy from cfg.Hierarchy
internal/ident/ident_test.go   # MODIFY: hierarchy test
internal/ha/discovery.go       # MODIFY: sub-device device block when grouped
internal/ha/discovery_test.go  # MODIFY: grouped + flat sub-device golden tests
internal/ha/testdata/subdevice_discovery.json  # CREATE: golden
```

---

### Task 1: Sub-device fields — model, config, ident

**Files:**
- Modify: `internal/model/metric.go`, `internal/config/config.go`, `internal/config/config_test.go`, `internal/ident/ident.go`, `internal/ident/ident_test.go`

**Interfaces:**
- Produces: `model.Metric.ComponentName string` (placed after `Component`); `model.Device.Hierarchy string`; `config.Config.Disks map[string]string` (yaml `disks`); `config.Config.DiskName(serial, fallback string) string`; `ident.Identify` sets `dev.Hierarchy = cfg.Hierarchy`

- [ ] **Step 1: Add the model fields**

In `internal/model/metric.go`, add `ComponentName` to `Metric` right after `Component`:
```go
	Component     string
	ComponentName string
```
and add `Hierarchy` to `Device` (after `Parent`):
```go
	Parent       string
	Hierarchy    string
```

- [ ] **Step 2: Write failing config + ident tests**

In `internal/config/config_test.go`, append:
```go
func TestLoadDisksAliasMap(t *testing.T) {
	t.Setenv("TEST_MQTT_PASSWORD", "x")
	cfg, err := Load("testdata/disks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Disks["WD-WMC4N1234567"] != "Parity" {
		t.Fatalf("alias: %q", cfg.Disks["WD-WMC4N1234567"])
	}
	if got := cfg.DiskName("WD-WMC4N1234567", "sda"); got != "Parity" {
		t.Fatalf("DiskName alias: %q", got)
	}
	if got := cfg.DiskName("UNKNOWN", "sdb"); got != "sdb" {
		t.Fatalf("DiskName fallback: %q", got)
	}
}
```
Create `internal/config/testdata/disks.yaml`:
```yaml
node: gc01srvr
disks:
  "WD-WMC4N1234567": "Parity"
sinks:
  - type: mqtt
    host: 192.168.1.65
    password: ${TEST_MQTT_PASSWORD}
```
In `internal/ident/ident_test.go`, append:
```go
func TestIdentifySetsHierarchy(t *testing.T) {
	dev := Identify(config.Config{Hierarchy: "flat"}, "h")
	if dev.Hierarchy != "flat" {
		t.Fatalf("hierarchy: %q", dev.Hierarchy)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/config/ ./internal/ident/ -run "Disks|Hierarchy"'`
Expected: FAIL (undefined `Disks`/`DiskName`, `Hierarchy` field, etc.).

- [ ] **Step 4: Implement**

In `internal/config/config.go`, add the field to `Config`:
```go
	Disks map[string]string `yaml:"disks"`
```
and add the resolver method:
```go
// DiskName returns the configured friendly alias for a disk serial/WWN, or fallback.
func (c Config) DiskName(serial, fallback string) string {
	if alias, ok := c.Disks[serial]; ok && alias != "" {
		return alias
	}
	return fallback
}
```
In `internal/ident/ident.go`, in `Identify`, set the hierarchy on the returned device (add the field to the `model.Device{...}` literal):
```go
		Hierarchy:    cfg.Hierarchy,
```

- [ ] **Step 5: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ && go build ./... && go test ./internal/config/ ./internal/ident/ ./internal/model/ && go vet ./...'`
Expected: PASS, build/vet clean.

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/model/metric.go internal/config/ internal/ident/ && git commit -F - <<'EOF'
feat: sub-device fields (ComponentName, Device.Hierarchy, disk aliases)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: Discovery sub-device device block

**Files:**
- Modify: `internal/ha/discovery.go`, `internal/ha/discovery_test.go`
- Create: `internal/ha/testdata/subdevice_discovery.json`

**Interfaces:**
- Consumes: `model.Device.Hierarchy`, `model.Metric.Component`, `model.Metric.ComponentName`
- Produces: `ha.Discovery` emitting a sub-device device block (`identifiers=[<host>-<component>]`, `name="<host name> <ComponentName>"`, `via_device=<host>`) when `m.Component != "" && dev.Hierarchy != "flat"`; host device block otherwise (unchanged)

**Depends on:** Task 1

- [ ] **Step 1: Write the golden sub-device test**

Create `internal/ha/testdata/subdevice_discovery.json`:
```json
{
  "name": "Temperature",
  "state_topic": "server-status/gc01srvr/disk-wd1234/disk_temperature",
  "unique_id": "server-status-gc01srvr-disk-wd1234-disk_temperature",
  "object_id": "gc01srvr_disk-wd1234_disk_temperature",
  "has_entity_name": true,
  "unit_of_measurement": "°C",
  "device_class": "temperature",
  "state_class": "measurement",
  "availability_topic": "server-status/gc01srvr/availability",
  "payload_available": "online",
  "payload_not_available": "offline",
  "qos": 0,
  "device": {
    "identifiers": [
      "server-status-gc01srvr-disk-wd1234"
    ],
    "name": "gc01srvr Disk sda",
    "via_device": "server-status-gc01srvr"
  }
}
```
Append to `internal/ha/discovery_test.go`:
```go
func TestDiscoverySubDeviceGrouped(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr", Hierarchy: "grouped"}
	m := model.Metric{
		Key: "disk_temperature", Component: "disk-wd1234", ComponentName: "Disk sda",
		Name: "Temperature", Value: 38, Unit: "°C", DeviceClass: "temperature",
		StateClass: "measurement", Kind: model.KindSensor, Category: "primary",
	}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/sensor/gc01srvr/gc01srvr_disk-wd1234_disk_temperature/config" {
		t.Fatalf("topic: %q", topic)
	}
	want, _ := os.ReadFile("testdata/subdevice_discovery.json")
	var got, w any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("payload invalid: %v", err)
	}
	_ = json.Unmarshal(want, &w)
	gn, _ := json.Marshal(got)
	wn, _ := json.Marshal(w)
	if !bytes.Equal(gn, wn) {
		t.Fatalf("mismatch\n got: %s\nwant: %s", gn, wn)
	}
}

func TestDiscoverySubDeviceFlat(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr", Hierarchy: "flat"}
	m := model.Metric{Key: "disk_temperature", Component: "disk-wd1234", ComponentName: "Disk sda", Name: "Temperature", Value: 38, Unit: "°C", DeviceClass: "temperature", StateClass: "measurement", Kind: model.KindSensor}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	_, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	_ = json.Unmarshal(payload, &obj)
	device := obj["device"].(map[string]any)
	ids := device["identifiers"].([]any)
	if ids[0] != "server-status-gc01srvr" {
		t.Fatalf("flat must use host identifier, got %v", ids[0])
	}
	if device["via_device"] != nil {
		t.Fatalf("flat host device must not have via_device here, got %v", device["via_device"])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/ha/ -run SubDevice'`
Expected: FAIL (grouped test: device block still shows host identifier, no via_device).

- [ ] **Step 3: Implement the sub-device block**

In `internal/ha/discovery.go`, in `Discovery`, replace the device-block construction. Currently it builds `p.Device` with host identifiers and sets `via_device` from `dev.Parent`. Change so that a grouped sub-device metric gets a sub-device block. Locate the block that creates `p.Device = deviceBlock{...}` and the following `if dev.Parent != "" { p.Device.ViaDevice = ... }`, and replace both with:
```go
	if m.Component != "" && dev.Hierarchy != "flat" {
		// Sub-device: its own identifier, linked to the host via via_device.
		compName := m.ComponentName
		if compName == "" {
			compName = m.Component
		}
		p.Device = deviceBlock{
			Identifiers: []string{dev.Identifier + "-" + m.Component},
			Name:        dev.Name + " " + compName,
			ViaDevice:   dev.Identifier,
		}
	} else {
		p.Device = deviceBlock{
			Identifiers:  []string{dev.Identifier},
			Name:         dev.Name,
			Manufacturer: dev.Manufacturer,
			Model:        dev.Model,
			SWVersion:    dev.SWVersion,
		}
		if dev.Parent != "" {
			p.Device.ViaDevice = "server-status-" + dev.Parent
		}
	}
```
(Keep the rest of `Discovery` — objectID, unique_id, state topic, availability, entity_category, binary/sensor fields — exactly as is.)

- [ ] **Step 4: gofmt, run all ha tests (incl. the non-regression ones)**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ha/ && go build ./... && go test ./internal/ha/ && go vet ./...'`
Expected: PASS. Critically, the existing `TestDiscoveryCPUGolden`, `TestDiscoveryInstance`, `TestDiscoveryViaDeviceWhenParentSet`, `TestDiscoveryBinarySensor` must still pass unchanged (host-level output is byte-identical because those metrics have `Component == ""`).

- [ ] **Step 5: Full suite**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./... && gofmt -l .'`
Expected: all green, gofmt empty.

- [ ] **Step 6: Commit**

```bash
cd "Z:/git/server-status" && git add internal/ha/ && git commit -F - <<'EOF'
feat: Home Assistant sub-device discovery for grouped components

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 3: Live validation of the sub-device hierarchy (controller-run)

**Files:** none (verification)

No collector emits sub-device metrics yet (Plans 04-05 do), so the controller live-validates the mechanism with a SYNTHETIC sub-device rather than a subagent implementing this task.

- [ ] **Step 1: Publish a synthetic host + sub-device**

The controller writes a throwaway program (inside the module, so it can import internal packages) that connects to the broker as node `sshier`, publishes: (a) one host-level metric (e.g. cpu_usage) so the host device exists, and (b) a sub-device metric with `Component: "disk-demo"`, `ComponentName: "Disk demo"`, `Key: "disk_temperature"`, `Kind: sensor`, plus availability `online`. Use `ha.Discovery` + retained publish, mirroring the MQTT sink.

- [ ] **Step 2: Confirm the nesting in Home Assistant**

Via the connected Home Assistant integration, confirm two devices exist: host `sshier` and a sub-device `sshier Disk demo` whose `via_device` points at `server-status-sshier` (the disk entity `sensor.sshier_...` groups under the sub-device, and the sub-device shows under the host). Record the result.

- [ ] **Step 3: Clean up**

Clear the retained discovery config topics and availability for node `sshier` (empty retained payloads via the broker), then confirm both devices are removed from Home Assistant.

---

## Self-review against the spec

Spec Phase 3 (spec §19 item 3: "device hierarchy — component sub-devices, hierarchy: grouped|flat, disk aliasing, host-to-host parent end to end"):

- Component sub-devices via `via_device` → Task 2 (grouped device block with sub-device identifier + via_device=host)
- `hierarchy: grouped|flat` → Task 1 (Device.Hierarchy from config) + Task 2 (flat falls back to host device block)
- Disk aliasing → Task 1 (config.Disks map + DiskName resolver; consumed by Plan 04's disk collector)
- Host-to-host parent → already complete in Plan 01 (host device via_device from dev.Parent); unchanged here and confirmed still applies to host-level blocks in Task 2's else-branch
- Live end-to-end → Task 3 (synthetic sub-device on real HA)

Non-regression: Task 2's else-branch reproduces the Plan 01 host device block verbatim, so `Component == ""` metrics (all current collectors) are byte-identical — asserted by keeping the existing golden tests green.

Placeholder scan: every code and test step contains complete content.

Type consistency: `ComponentName`/`Hierarchy` added in Task 1 are consumed in Task 2; `config.Disks`/`DiskName` are prep for Plan 04; the sub-device identifier format `<host>-<component>` matches `UniqueID`'s existing component handling.

## Roadmap: subsequent plans

- Plan 04: rich storage (SMART curated/full with disk serials + aliases via DiskName, mdadm, ZFS, btrfs) and GPU — the first real sub-device collectors
- Plan 05: docker (registry digest compare, container inventory, compose awareness), docker as a sub-device
- Plan 06: webhook sink + HTTP control surface, parity golden tests
- Plan 07: control commands + GitHub-Releases self-update, HA update entity, release pipeline
- Plan 08: reliability hardening
- Plan 09: migration cutover
