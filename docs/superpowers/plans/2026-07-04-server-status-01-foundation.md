# server-status foundation (Plan 01) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first working vertical slice of the Go rewrite: from a minimal config, autodetect and publish CPU usage, memory, and uptime to MQTT with Home Assistant discovery as a per-host device, runnable under systemd and installable with a script.

**Architecture:** One Go module. Collectors produce a normalized in-memory `Snapshot` of `Metric`s; a `Sink` renders that snapshot to a transport. Phase 1 ships the model, config, device identity, three `/proc`-based collectors, a `--dump-detected` dry run, the Home Assistant discovery payload builder, and one MQTT sink. Each collector splits a pure parser (fixture-tested, cross-platform) from a thin Linux reader.

**Tech Stack:** Go 1.22+, `gopkg.in/yaml.v3`, `github.com/eclipse/paho.mqtt.golang`. Tests use the standard `testing` package with fixture files under `testdata/`.

## Global Constraints

- Language and packaging: single Go static binary, target `linux/amd64` and `linux/arm64`; module path `github.com/giovi321/server-status`, Go 1.22+
- Target OS at runtime: Debian/Ubuntu Linux only; parser tests must not depend on `/proc` so they run on the dev machine (Windows) too
- Agent is a pure state publisher: no thresholds, no notifications in the agent
- Naming: display names are short human leaves (`has_entity_name: true`); serials and UUIDs never appear in device or entity display names, only in hidden unique_ids or diagnostic values
- Canonical metric keys are snake_case and fixed: this plan introduces `cpu_usage`, `memory_used`, `memory_available`, `uptime`
- Device identifier scheme: host device identifier is `server-status-<node>`; entity unique_id is `<host-identifier>[-<component>]-<key>`
- Topic scheme: base default `server-status`; host metric state topic `<base>/<node>/<key>`; availability `<base>/<node>/availability`; discovery `<discovery_prefix>/<ha_component>/<node>/<object_id>/config`
- DRY, YAGNI, TDD, frequent commits. Do not add `Interval()`, webhook, HTTP control, sub-devices, or slow collectors in this plan; later plans add them

## Prerequisites

- Go 1.22+ installed on the machine that runs `go test` and `go build`. Verify with `go version`
- For live MQTT and systemd verification: a Debian/Ubuntu host with an MQTT broker reachable, and Home Assistant with MQTT discovery enabled
- Work on branch `server-status-revamp`

## File structure created by this plan

```
go.mod
go.sum
cmd/server-status/main.go              # flags, wiring, run loop
internal/version/version.go            # Version var (ldflags-stamped)
internal/model/metric.go               # Kind, Metric, Device, Snapshot
internal/model/metric_test.go
internal/config/config.go              # Config structs, Load, ${ENV} interpolation
internal/config/config_test.go
internal/config/testdata/minimal.yaml
internal/ident/ident.go                # Identify(cfg) -> model.Device, sanitize
internal/ident/ident_test.go
internal/collector/collector.go        # Collector interface, Registry
internal/collector/cpu.go              # parseCPUSample + CPU collector
internal/collector/cpu_test.go
internal/collector/cpu_test data       # inline fixtures (strings)
internal/collector/memory.go           # parseMeminfo + Memory collector
internal/collector/memory_test.go
internal/collector/uptime.go           # parseUptime + Uptime collector
internal/collector/uptime_test.go
internal/detect/detect.go              # Collect available metrics, DumpJSON
internal/detect/detect_test.go
internal/ha/topics.go                  # topic + id helpers
internal/ha/topics_test.go
internal/ha/discovery.go               # Discovery payload builder, StateValue
internal/ha/discovery_test.go
internal/ha/testdata/cpu_discovery.json
internal/sink/sink.go                  # Sink interface
internal/sink/mqtt.go                  # MQTT sink
packaging/server-status.service
scripts/install.sh
```

---

### Task 1: Module scaffolding and core model

**Files:**
- Create: `go.mod`
- Create: `internal/version/version.go`
- Create: `internal/model/metric.go`
- Test: `internal/model/metric_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `model.Kind` (string enum: `KindSensor`, `KindBinarySensor`, `KindText`, `KindUpdate`), `model.Metric{Key, Component, Name string; Value any; Unit, DeviceClass, StateClass string; Kind Kind; Category, Icon string}`, `model.Device{Node, Name, Identifier, Parent, Model, Manufacturer, SWVersion string}`, `model.Snapshot{Device Device; TS time.Time; Metrics []Metric}`, and `model.Metric.IsHost() bool` (true when `Component == ""`); `version.Version string`

- [ ] **Step 1: Initialize the module**

Run:
```bash
cd Z:/git/server-status
go mod init github.com/giovi321/server-status
```
Expected: creates `go.mod` containing `module github.com/giovi321/server-status` and a `go 1.22` line (or your installed minor).

- [ ] **Step 2: Write the version package**

Create `internal/version/version.go`:
```go
// Package version exposes the build version, stamped at build time via ldflags.
package version

// Version is overridden at build time with:
//   -ldflags "-X github.com/giovi321/server-status/internal/version.Version=v1.2.3"
var Version = "dev"
```

- [ ] **Step 3: Write the failing model test**

Create `internal/model/metric_test.go`:
```go
package model

import "testing"

func TestMetricIsHost(t *testing.T) {
	host := Metric{Key: "cpu_usage"}
	if !host.IsHost() {
		t.Fatalf("empty Component should be a host metric")
	}
	comp := Metric{Key: "disk_temperature", Component: "disk-abc"}
	if comp.IsHost() {
		t.Fatalf("non-empty Component should not be a host metric")
	}
}
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `go test ./internal/model/`
Expected: FAIL, `undefined: Metric` (package does not compile yet).

- [ ] **Step 5: Write the model**

Create `internal/model/metric.go`:
```go
// Package model holds the transport-agnostic data types shared by collectors and sinks.
package model

import "time"

// Kind maps a metric to a Home Assistant entity component.
type Kind string

const (
	KindSensor       Kind = "sensor"
	KindBinarySensor Kind = "binary_sensor"
	KindText         Kind = "text"
	KindUpdate       Kind = "update"
)

// Metric is one published value. Component is the sub-device id; empty means the host device.
type Metric struct {
	Key         string
	Component   string
	Name        string
	Value       any
	Unit        string
	DeviceClass string
	StateClass  string
	Kind        Kind
	Category    string // "primary" or "diagnostic"
	Icon        string
}

// IsHost reports whether the metric attaches to the host device rather than a sub-device.
func (m Metric) IsHost() bool { return m.Component == "" }

// Device identifies the host (and, via Parent, its parent host).
type Device struct {
	Node         string
	Name         string
	Identifier   string
	Parent       string
	Model        string
	Manufacturer string
	SWVersion    string
}

// Snapshot is the full set of metrics for one host at one instant.
type Snapshot struct {
	Device  Device
	TS      time.Time
	Metrics []Metric
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/model/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod internal/version/version.go internal/model/
git commit -m "feat: module scaffolding and core model types"
```

---

### Task 2: Config loading with environment interpolation

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/testdata/minimal.yaml`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `config.SinkConfig{Type, Host string; Port int; Username, Password, BaseTopic, DiscoveryPrefix string; Retain bool; QoS int}`, `config.Config{Node, FriendlyName, Parent, Hierarchy string; Sinks []SinkConfig}`, `config.Load(path string) (Config, error)` (reads YAML, interpolates `${VAR}` from the environment, applies defaults: `Hierarchy="grouped"`, per-sink `BaseTopic="server-status"`, `DiscoveryPrefix="homeassistant"`, `Port=1883`), and `config.ExpandEnv(raw []byte) []byte`

- [ ] **Step 1: Add the yaml dependency**

Run:
```bash
go get gopkg.in/yaml.v3@latest
```
Expected: adds `gopkg.in/yaml.v3` to `go.mod`.

- [ ] **Step 2: Create the fixture config**

Create `internal/config/testdata/minimal.yaml`:
```yaml
node: gc01srvr
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${TEST_MQTT_PASSWORD}
```

- [ ] **Step 3: Write the failing test**

Create `internal/config/config_test.go`:
```go
package config

import (
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_MQTT_PASSWORD", "s3cret")
	got := string(ExpandEnv([]byte("password: ${TEST_MQTT_PASSWORD}")))
	if got != "password: s3cret" {
		t.Fatalf("got %q", got)
	}
	// Unset variables expand to empty, and a literal $ that is not ${...} is left alone.
	if string(ExpandEnv([]byte("a: ${NOPE} b: $5"))) != "a:  b: $5" {
		t.Fatalf("unexpected expansion: %q", ExpandEnv([]byte("a: ${NOPE} b: $5")))
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("TEST_MQTT_PASSWORD", "s3cret")
	cfg, err := Load("testdata/minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node != "gc01srvr" {
		t.Fatalf("node: %q", cfg.Node)
	}
	if cfg.Hierarchy != "grouped" {
		t.Fatalf("default hierarchy: %q", cfg.Hierarchy)
	}
	if len(cfg.Sinks) != 1 {
		t.Fatalf("sinks: %d", len(cfg.Sinks))
	}
	s := cfg.Sinks[0]
	if s.Host != "192.168.1.65" || s.Port != 1883 {
		t.Fatalf("host/port: %q/%d", s.Host, s.Port)
	}
	if s.Password != "s3cret" {
		t.Fatalf("password not interpolated: %q", s.Password)
	}
	if s.BaseTopic != "server-status" || s.DiscoveryPrefix != "homeassistant" {
		t.Fatalf("topic defaults: %q/%q", s.BaseTopic, s.DiscoveryPrefix)
	}
}
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `go test ./internal/config/`
Expected: FAIL, `undefined: ExpandEnv` / `undefined: Load`.

- [ ] **Step 5: Write the config package**

Create `internal/config/config.go`:
```go
// Package config loads and defaults the agent configuration.
package config

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// SinkConfig configures one output transport. Phase 1 uses only type "mqtt".
type SinkConfig struct {
	Type            string `yaml:"type"`
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	BaseTopic       string `yaml:"base_topic"`
	DiscoveryPrefix string `yaml:"discovery_prefix"`
	Retain          bool   `yaml:"retain"`
	QoS             int    `yaml:"qos"`
}

// Config is the whole agent configuration.
type Config struct {
	Node         string       `yaml:"node"`
	FriendlyName string       `yaml:"friendly_name"`
	Parent       string       `yaml:"parent"`
	Hierarchy    string       `yaml:"hierarchy"`
	Sinks        []SinkConfig `yaml:"sinks"`
}

var envRefs = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces ${VAR} references with their environment values.
// A bare $ that is not part of ${...} is left untouched.
func ExpandEnv(raw []byte) []byte {
	return envRefs.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := envRefs.FindSubmatch(m)[1]
		return []byte(os.Getenv(string(name)))
	})
}

// Load reads a YAML config file, interpolates ${ENV}, and applies defaults.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(ExpandEnv(raw), &cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Hierarchy == "" {
		c.Hierarchy = "grouped"
	}
	for i := range c.Sinks {
		s := &c.Sinks[i]
		if s.Port == 0 {
			s.Port = 1883
		}
		if s.BaseTopic == "" {
			s.BaseTopic = "server-status"
		}
		if s.DiscoveryPrefix == "" {
			s.DiscoveryPrefix = "homeassistant"
		}
	}
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/
git commit -m "feat: config loading with env interpolation and defaults"
```

---

### Task 3: Device identity

**Files:**
- Create: `internal/ident/ident.go`
- Test: `internal/ident/ident_test.go`

**Interfaces:**
- Consumes: `config.Config`, `model.Device`, `version.Version`
- Produces: `ident.Sanitize(s string) string` (lowercases, replaces any run of characters outside `[a-z0-9-]` with a single `-`, trims leading/trailing `-`), `ident.Identify(cfg config.Config, hostname string) model.Device` (node = `cfg.Node` or `Sanitize(hostname)`; `Identifier="server-status-"+node`; `Name=cfg.FriendlyName` or node; `Parent=Sanitize(cfg.Parent)`; `SWVersion=version.Version`; `Manufacturer="server-status"`)

- [ ] **Step 1: Write the failing test**

Create `internal/ident/ident_test.go`:
```go
package ident

import (
	"testing"

	"github.com/giovi321/server-status/internal/config"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"GC01srvr":       "gc01srvr",
		"host.lan":       "host-lan",
		"weird__Name!!":  "weird-name",
		"--edges--":      "edges",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIdentifyDefaultsToHostname(t *testing.T) {
	dev := Identify(config.Config{}, "GC01srvr")
	if dev.Node != "gc01srvr" {
		t.Fatalf("node %q", dev.Node)
	}
	if dev.Identifier != "server-status-gc01srvr" {
		t.Fatalf("identifier %q", dev.Identifier)
	}
	if dev.Name != "gc01srvr" {
		t.Fatalf("name %q", dev.Name)
	}
}

func TestIdentifyOverrides(t *testing.T) {
	dev := Identify(config.Config{Node: "vm-web", FriendlyName: "Web VM", Parent: "GC01srvr"}, "ignored")
	if dev.Node != "vm-web" || dev.Name != "Web VM" {
		t.Fatalf("got %+v", dev)
	}
	if dev.Parent != "gc01srvr" {
		t.Fatalf("parent %q", dev.Parent)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ident/`
Expected: FAIL, `undefined: Sanitize` / `undefined: Identify`.

- [ ] **Step 3: Write the ident package**

Create `internal/ident/ident.go`:
```go
// Package ident derives the stable, human-readable host identity.
package ident

import (
	"regexp"
	"strings"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
	"github.com/giovi321/server-status/internal/version"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Sanitize turns an arbitrary label into a short slug of [a-z0-9-].
func Sanitize(s string) string {
	s = strings.ToLower(s)
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// Identify builds the host Device from config, falling back to the hostname for the node.
func Identify(cfg config.Config, hostname string) model.Device {
	node := cfg.Node
	if node == "" {
		node = Sanitize(hostname)
	} else {
		node = Sanitize(node)
	}
	name := cfg.FriendlyName
	if name == "" {
		name = node
	}
	return model.Device{
		Node:         node,
		Name:         name,
		Identifier:   "server-status-" + node,
		Parent:       Sanitize(cfg.Parent),
		Manufacturer: "server-status",
		SWVersion:    version.Version,
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ident/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ident/
git commit -m "feat: human-readable device identity"
```

---

### Task 4: Collector interface and CPU collector

**Files:**
- Create: `internal/collector/collector.go`
- Create: `internal/collector/cpu.go`
- Test: `internal/collector/cpu_test.go`

**Interfaces:**
- Consumes: `model.Metric`
- Produces: `collector.Collector` interface (`Name() string`, `Available() bool`, `Collect(ctx context.Context) ([]model.Metric, error)`); `collector.CPUSample{Idle, Total uint64}`, `collector.parseCPUSample(line string) (CPUSample, bool)`, `collector.usagePercent(a, b CPUSample) float64`, and `collector.CPU{}` implementing `Collector` for key `cpu_usage`

- [ ] **Step 1: Write the failing parser test**

Create `internal/collector/cpu_test.go`:
```go
package collector

import (
	"math"
	"testing"
)

func TestParseCPUSample(t *testing.T) {
	// user nice system idle iowait irq softirq steal
	s, ok := parseCPUSample("cpu  100 0 50 800 20 0 30 0")
	if !ok {
		t.Fatal("expected parse ok")
	}
	// idle_all = idle+iowait = 820; non_idle = user+nice+system+irq+softirq(+steal) = 100+0+50+0+30+0 = 180
	if s.Idle != 820 || s.Total != 1000 {
		t.Fatalf("got idle=%d total=%d", s.Idle, s.Total)
	}
}

func TestParseCPUSampleRejectsOtherLines(t *testing.T) {
	if _, ok := parseCPUSample("cpu0 1 2 3 4 5 6 7"); ok {
		t.Fatal("only the aggregate cpu line should parse")
	}
	if _, ok := parseCPUSample("intr 1234"); ok {
		t.Fatal("non-cpu line should not parse")
	}
}

func TestUsagePercent(t *testing.T) {
	a := CPUSample{Idle: 100, Total: 200}
	b := CPUSample{Idle: 150, Total: 400} // delta idle 50, delta total 200 => 75% busy
	got := usagePercent(a, b)
	if math.Abs(got-75.0) > 0.001 {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/collector/`
Expected: FAIL, `undefined: parseCPUSample`.

- [ ] **Step 3: Write the collector interface**

Create `internal/collector/collector.go`:
```go
// Package collector defines the Collector interface and the built-in collectors.
package collector

import (
	"context"

	"github.com/giovi321/server-status/internal/model"
)

// Collector produces zero or more metrics for one metric family.
type Collector interface {
	// Name is a stable family identifier, e.g. "cpu".
	Name() string
	// Available reports whether this host can produce these metrics.
	Available() bool
	// Collect gathers the current metrics.
	Collect(ctx context.Context) ([]model.Metric, error)
}
```

- [ ] **Step 4: Write the CPU collector**

Create `internal/collector/cpu.go`:
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

// CPUSample is a point-in-time reading of aggregate CPU jiffies.
type CPUSample struct {
	Idle  uint64
	Total uint64
}

// parseCPUSample parses the aggregate "cpu " line of /proc/stat.
func parseCPUSample(line string) (CPUSample, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return CPUSample{}, false
	}
	var vals []uint64
	for _, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return CPUSample{}, false
		}
		vals = append(vals, v)
	}
	// vals: user nice system idle iowait irq softirq [steal ...]
	idle := vals[3]
	iowait := uint64(0)
	if len(vals) > 4 {
		iowait = vals[4]
	}
	var total uint64
	for _, v := range vals {
		total += v
	}
	return CPUSample{Idle: idle + iowait, Total: total}, true
}

func usagePercent(a, b CPUSample) float64 {
	dTotal := float64(b.Total - a.Total)
	if dTotal <= 0 {
		return 0
	}
	dIdle := float64(b.Idle - a.Idle)
	busy := (dTotal - dIdle) * 100.0 / dTotal
	if busy < 0 {
		return 0
	}
	if busy > 100 {
		return 100
	}
	return busy
}

func readCPUSample() (CPUSample, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPUSample{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			return parseCPUSample(line)
		}
	}
	return CPUSample{}, false
}

// CPU publishes overall CPU usage as a percentage.
type CPU struct{}

func (CPU) Name() string { return "cpu" }

func (CPU) Available() bool {
	_, ok := readCPUSample()
	return ok
}

func (CPU) Collect(ctx context.Context) ([]model.Metric, error) {
	a, ok := readCPUSample()
	if !ok {
		return nil, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Second):
	}
	b, ok := readCPUSample()
	if !ok {
		return nil, nil
	}
	usage := usagePercent(a, b)
	return []model.Metric{{
		Key:         "cpu_usage",
		Name:        "CPU usage",
		Value:       int(usage + 0.5),
		Unit:        "%",
		StateClass:  "measurement",
		Kind:        model.KindSensor,
		Category:    "primary",
		Icon:        "mdi:cpu-64-bit",
	}}, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/collector/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/collector/collector.go internal/collector/cpu.go internal/collector/cpu_test.go
git commit -m "feat: collector interface and CPU usage collector"
```

---

### Task 5: Memory collector

**Files:**
- Create: `internal/collector/memory.go`
- Test: `internal/collector/memory_test.go`

**Interfaces:**
- Consumes: `collector.Collector`, `model.Metric`
- Produces: `collector.parseMeminfo(data string) (map[string]uint64, bool)`, `collector.memoryMetrics(mem map[string]uint64) []model.Metric` (emits `memory_used` and `memory_available` as integer percents), and `collector.Memory{}` implementing `Collector`

- [ ] **Step 1: Write the failing test**

Create `internal/collector/memory_test.go`:
```go
package collector

import "testing"

const meminfoFixture = `MemTotal:       16384000 kB
MemFree:         1000000 kB
MemAvailable:    8192000 kB
Buffers:          500000 kB
Cached:          4000000 kB
`

func TestParseMeminfo(t *testing.T) {
	m, ok := parseMeminfo(meminfoFixture)
	if !ok {
		t.Fatal("expected ok")
	}
	if m["MemTotal"] != 16384000 || m["MemAvailable"] != 8192000 {
		t.Fatalf("got %+v", m)
	}
}

func TestMemoryMetrics(t *testing.T) {
	m, _ := parseMeminfo(meminfoFixture)
	metrics := memoryMetrics(m)
	got := map[string]any{}
	for _, mt := range metrics {
		got[mt.Key] = mt.Value
	}
	// available = 8192000/16384000 = 50%, used = 50%
	if got["memory_available"] != 50 {
		t.Fatalf("memory_available=%v", got["memory_available"])
	}
	if got["memory_used"] != 50 {
		t.Fatalf("memory_used=%v", got["memory_used"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/collector/ -run Memory`
Expected: FAIL, `undefined: parseMeminfo`.

- [ ] **Step 3: Write the memory collector**

Create `internal/collector/memory.go`:
```go
package collector

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseMeminfo parses /proc/meminfo into a map of key -> kB value.
func parseMeminfo(data string) (map[string]uint64, bool) {
	out := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if v, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			out[key] = v
		}
	}
	_, ok := out["MemTotal"]
	return out, ok
}

func pct(part, total uint64) int {
	if total == 0 {
		return 0
	}
	return int(float64(part)*100.0/float64(total) + 0.5)
}

func memoryMetrics(mem map[string]uint64) []model.Metric {
	total := mem["MemTotal"]
	avail := mem["MemAvailable"]
	used := total - avail
	return []model.Metric{
		{Key: "memory_used", Name: "Memory used", Value: pct(used, total), Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:memory"},
		{Key: "memory_available", Name: "Memory available", Value: pct(avail, total), Unit: "%", StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:memory"},
	}
}

// Memory publishes memory used and available percentages.
type Memory struct{}

func (Memory) Name() string { return "memory" }

func (Memory) Available() bool {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return false
	}
	_, ok := parseMeminfo(string(data))
	return ok
}

func (Memory) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, nil
	}
	mem, ok := parseMeminfo(string(data))
	if !ok {
		return nil, nil
	}
	return memoryMetrics(mem), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/collector/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collector/memory.go internal/collector/memory_test.go
git commit -m "feat: memory collector"
```

---

### Task 6: Uptime collector

**Files:**
- Create: `internal/collector/uptime.go`
- Test: `internal/collector/uptime_test.go`

**Interfaces:**
- Consumes: `collector.Collector`, `model.Metric`
- Produces: `collector.parseUptime(data string) (float64, bool)` (returns days), `collector.uptimeMetric(days float64) model.Metric`, and `collector.Uptime{}` implementing `Collector`

- [ ] **Step 1: Write the failing test**

Create `internal/collector/uptime_test.go`:
```go
package collector

import (
	"math"
	"testing"
)

func TestParseUptime(t *testing.T) {
	days, ok := parseUptime("172800.00 100000.00")
	if !ok {
		t.Fatal("expected ok")
	}
	if math.Abs(days-2.0) > 0.001 { // 172800s = 2 days
		t.Fatalf("got %v", days)
	}
	if _, ok := parseUptime("garbage"); ok {
		t.Fatal("garbage should not parse")
	}
}

func TestUptimeMetricPrecision(t *testing.T) {
	// Under 10 days keeps two decimals; 10+ days rounds to an integer.
	if v := uptimeMetric(2.5).Value; v != 2.5 {
		t.Fatalf("under 10 days: %v", v)
	}
	if v := uptimeMetric(42.7).Value; v != 43 {
		t.Fatalf("over 10 days: %v", v)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/collector/ -run Uptime`
Expected: FAIL, `undefined: parseUptime`.

- [ ] **Step 3: Write the uptime collector**

Create `internal/collector/uptime.go`:
```go
package collector

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

// parseUptime reads the first field of /proc/uptime (seconds) and returns days.
func parseUptime(data string) (float64, bool) {
	fields := strings.Fields(data)
	if len(fields) == 0 {
		return 0, false
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return secs / 86400.0, true
}

func uptimeMetric(days float64) model.Metric {
	var value any
	if days < 10 {
		value = math.Round(days*100) / 100
	} else {
		value = int(days + 0.5)
	}
	return model.Metric{
		Key:        "uptime",
		Name:       "Uptime",
		Value:      value,
		Unit:       "d",
		StateClass: "measurement",
		Kind:       model.KindSensor,
		Category:   "primary",
		Icon:       "mdi:clock-outline",
	}
}

// Uptime publishes system uptime in days.
type Uptime struct{}

func (Uptime) Name() string { return "uptime" }

func (Uptime) Available() bool {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return false
	}
	_, ok := parseUptime(string(data))
	return ok
}

func (Uptime) Collect(ctx context.Context) ([]model.Metric, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil, nil
	}
	days, ok := parseUptime(string(data))
	if !ok {
		return nil, nil
	}
	return []model.Metric{uptimeMetric(days)}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/collector/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collector/uptime.go internal/collector/uptime_test.go
git commit -m "feat: uptime collector"
```

---

### Task 7: Detection registry and --dump-detected

**Files:**
- Create: `internal/detect/detect.go`
- Test: `internal/detect/detect_test.go`

**Interfaces:**
- Consumes: `collector.Collector`, `collector.CPU`, `collector.Memory`, `collector.Uptime`, `model.Metric`, `model.Device`, `model.Snapshot`
- Produces: `detect.All() []collector.Collector` (returns the built-in collectors), `detect.Available(cols []collector.Collector) []collector.Collector` (filters by `Available()`), `detect.Snapshot(ctx, dev, cols) model.Snapshot` (runs each collector, aggregates metrics, sets `TS`), and `detect.DumpJSON(w io.Writer, dev model.Device, cols []collector.Collector, ctx context.Context) error` (writes indented JSON listing each collector name, availability, and its metrics)

- [ ] **Step 1: Write the failing test**

Create `internal/detect/detect_test.go`:
```go
package detect

import (
	"context"
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

// fake is a deterministic collector for testing aggregation without touching /proc.
type fake struct {
	name    string
	avail   bool
	metrics []model.Metric
}

func (f fake) Name() string                                          { return f.name }
func (f fake) Available() bool                                       { return f.avail }
func (f fake) Collect(context.Context) ([]model.Metric, error)       { return f.metrics, nil }

func TestAvailableFilters(t *testing.T) {
	cols := []collectorIface{
		fake{name: "a", avail: true},
		fake{name: "b", avail: false},
	}
	got := availableFrom(cols)
	if len(got) != 1 || got[0].Name() != "a" {
		t.Fatalf("got %d collectors", len(got))
	}
}

func TestSnapshotAggregates(t *testing.T) {
	cols := []collectorIface{
		fake{name: "a", avail: true, metrics: []model.Metric{{Key: "x"}}},
		fake{name: "b", avail: true, metrics: []model.Metric{{Key: "y"}, {Key: "z"}}},
	}
	snap := snapshotFrom(context.Background(), model.Device{Node: "n"}, cols)
	if len(snap.Metrics) != 3 {
		t.Fatalf("got %d metrics", len(snap.Metrics))
	}
	if snap.TS.IsZero() {
		t.Fatal("timestamp not set")
	}
}
```

Note: the test uses local aliases `collectorIface`, `availableFrom`, and `snapshotFrom` so it does not depend on the real `/proc` collectors. Define these as thin internal helpers in `detect.go` and have the exported `All`/`Available`/`Snapshot` wrap them.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/detect/`
Expected: FAIL, `undefined: collectorIface`.

- [ ] **Step 3: Write the detect package**

Create `internal/detect/detect.go`:
```go
// Package detect wires the built-in collectors together and powers --dump-detected.
package detect

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/giovi321/server-status/internal/collector"
	"github.com/giovi321/server-status/internal/model"
)

type collectorIface = collector.Collector

// All returns every built-in collector, regardless of availability.
func All() []collector.Collector {
	return []collector.Collector{
		collector.CPU{},
		collector.Memory{},
		collector.Uptime{},
	}
}

func availableFrom(cols []collectorIface) []collectorIface {
	var out []collectorIface
	for _, c := range cols {
		if c.Available() {
			out = append(out, c)
		}
	}
	return out
}

// Available returns only the collectors that report data on this host.
func Available(cols []collector.Collector) []collector.Collector {
	return availableFrom(cols)
}

func snapshotFrom(ctx context.Context, dev model.Device, cols []collectorIface) model.Snapshot {
	snap := model.Snapshot{Device: dev, TS: time.Now()}
	for _, c := range cols {
		metrics, err := c.Collect(ctx)
		if err != nil {
			continue
		}
		snap.Metrics = append(snap.Metrics, metrics...)
	}
	return snap
}

// Snapshot runs the given collectors and aggregates their metrics.
func Snapshot(ctx context.Context, dev model.Device, cols []collector.Collector) model.Snapshot {
	return snapshotFrom(ctx, dev, cols)
}

type dumpCollector struct {
	Name      string         `json:"name"`
	Available bool           `json:"available"`
	Metrics   []model.Metric `json:"metrics,omitempty"`
}

// DumpJSON writes an indented JSON report of each collector and the metrics it would publish.
func DumpJSON(w io.Writer, dev model.Device, cols []collector.Collector, ctx context.Context) error {
	report := struct {
		Device     model.Device    `json:"device"`
		Collectors []dumpCollector `json:"collectors"`
	}{Device: dev}
	for _, c := range cols {
		dc := dumpCollector{Name: c.Name(), Available: c.Available()}
		if dc.Available {
			if metrics, err := c.Collect(ctx); err == nil {
				dc.Metrics = metrics
			}
		}
		report.Collectors = append(report.Collectors, dc)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/detect/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/detect/
git commit -m "feat: detection registry and snapshot aggregation"
```

---

### Task 8: Home Assistant topic and discovery builder

**Files:**
- Create: `internal/ha/topics.go`
- Create: `internal/ha/discovery.go`
- Create: `internal/ha/testdata/cpu_discovery.json`
- Test: `internal/ha/topics_test.go`
- Test: `internal/ha/discovery_test.go`

**Interfaces:**
- Consumes: `model.Device`, `model.Metric`, `config.SinkConfig`
- Produces: `ha.StateTopic(base, node, component, key string) string`, `ha.AvailabilityTopic(base, node string) string`, `ha.ObjectID(node, component, key string) string`, `ha.UniqueID(dev model.Device, m model.Metric) string`, `ha.Component(k model.Kind) string` (sensor or binary_sensor), `ha.DiscoveryTopic(prefix string, k model.Kind, node, objectID string) string`, `ha.StateValue(m model.Metric) string`, and `ha.Discovery(dev model.Device, m model.Metric, sc config.SinkConfig) (topic string, payload []byte, err error)`

- [ ] **Step 1: Write the failing topics test**

Create `internal/ha/topics_test.go`:
```go
package ha

import (
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

func TestTopics(t *testing.T) {
	if got := StateTopic("server-status", "gc01srvr", "", "cpu_usage"); got != "server-status/gc01srvr/cpu_usage" {
		t.Fatalf("host state topic: %q", got)
	}
	if got := StateTopic("server-status", "gc01srvr", "disk-abc", "disk_temperature"); got != "server-status/gc01srvr/disk-abc/disk_temperature" {
		t.Fatalf("component state topic: %q", got)
	}
	if got := AvailabilityTopic("server-status", "gc01srvr"); got != "server-status/gc01srvr/availability" {
		t.Fatalf("availability: %q", got)
	}
	if got := ObjectID("gc01srvr", "", "cpu_usage"); got != "gc01srvr_cpu_usage" {
		t.Fatalf("object id host: %q", got)
	}
	if got := ObjectID("gc01srvr", "disk-abc", "disk_temperature"); got != "gc01srvr_disk-abc_disk_temperature" {
		t.Fatalf("object id component: %q", got)
	}
	if got := DiscoveryTopic("homeassistant", model.KindSensor, "gc01srvr", "gc01srvr_cpu_usage"); got != "homeassistant/sensor/gc01srvr/gc01srvr_cpu_usage/config" {
		t.Fatalf("discovery topic: %q", got)
	}
}

func TestStateValue(t *testing.T) {
	if got := StateValue(model.Metric{Value: 42}); got != "42" {
		t.Fatalf("int: %q", got)
	}
	if got := StateValue(model.Metric{Value: 2.5}); got != "2.5" {
		t.Fatalf("float: %q", got)
	}
	if got := StateValue(model.Metric{Kind: model.KindBinarySensor, Value: true}); got != "ON" {
		t.Fatalf("bool true: %q", got)
	}
	if got := StateValue(model.Metric{Kind: model.KindBinarySensor, Value: false}); got != "OFF" {
		t.Fatalf("bool false: %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ha/`
Expected: FAIL, `undefined: StateTopic`.

- [ ] **Step 3: Write the topics helpers**

Create `internal/ha/topics.go`:
```go
// Package ha builds Home Assistant MQTT discovery payloads and topic names.
package ha

import (
	"fmt"
	"strconv"

	"github.com/giovi321/server-status/internal/model"
)

// StateTopic is where a metric's value is published.
func StateTopic(base, node, component, key string) string {
	if component == "" {
		return fmt.Sprintf("%s/%s/%s", base, node, key)
	}
	return fmt.Sprintf("%s/%s/%s/%s", base, node, component, key)
}

// AvailabilityTopic is the per-host LWT topic.
func AvailabilityTopic(base, node string) string {
	return fmt.Sprintf("%s/%s/availability", base, node)
}

// ObjectID is the human-readable slug used to build the entity_id.
func ObjectID(node, component, key string) string {
	if component == "" {
		return node + "_" + key
	}
	return node + "_" + component + "_" + key
}

// UniqueID is the hidden, stable id. It may contain serials via the component.
func UniqueID(dev model.Device, m model.Metric) string {
	if m.Component == "" {
		return dev.Identifier + "-" + m.Key
	}
	return dev.Identifier + "-" + m.Component + "-" + m.Key
}

// Component maps a metric kind to its Home Assistant discovery component.
func Component(k model.Kind) string {
	if k == model.KindBinarySensor {
		return "binary_sensor"
	}
	return "sensor"
}

// DiscoveryTopic is where the retained discovery config is published.
func DiscoveryTopic(prefix string, k model.Kind, node, objectID string) string {
	return fmt.Sprintf("%s/%s/%s/%s/config", prefix, Component(k), node, objectID)
}

// StateValue renders a metric value into its MQTT string payload.
func StateValue(m model.Metric) string {
	if m.Kind == model.KindBinarySensor {
		if b, ok := m.Value.(bool); ok {
			if b {
				return "ON"
			}
			return "OFF"
		}
	}
	switch v := m.Value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
```

- [ ] **Step 4: Run the topics test to verify it passes**

Run: `go test ./internal/ha/ -run TestTopics`
Expected: PASS. (`TestStateValue` also passes.)

- [ ] **Step 5: Write the failing discovery test and golden file**

Create `internal/ha/testdata/cpu_discovery.json`:
```json
{
  "name": "CPU usage",
  "state_topic": "server-status/gc01srvr/cpu_usage",
  "unique_id": "server-status-gc01srvr-cpu_usage",
  "object_id": "gc01srvr_cpu_usage",
  "has_entity_name": true,
  "unit_of_measurement": "%",
  "state_class": "measurement",
  "icon": "mdi:cpu-64-bit",
  "availability_topic": "server-status/gc01srvr/availability",
  "payload_available": "online",
  "payload_not_available": "offline",
  "qos": 0,
  "device": {
    "identifiers": [
      "server-status-gc01srvr"
    ],
    "name": "gc01srvr",
    "manufacturer": "server-status",
    "sw_version": "dev"
  }
}
```

Create `internal/ha/discovery_test.go`:
```go
package ha

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

func TestDiscoveryCPUGolden(t *testing.T) {
	dev := model.Device{
		Node:         "gc01srvr",
		Name:         "gc01srvr",
		Identifier:   "server-status-gc01srvr",
		Manufacturer: "server-status",
		SWVersion:    "dev",
	}
	m := model.Metric{
		Key: "cpu_usage", Name: "CPU usage", Value: 5, Unit: "%",
		StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: "mdi:cpu-64-bit",
	}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}

	topic, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/sensor/gc01srvr/gc01srvr_cpu_usage/config" {
		t.Fatalf("topic: %q", topic)
	}

	want, err := os.ReadFile("testdata/cpu_discovery.json")
	if err != nil {
		t.Fatal(err)
	}
	var gotObj, wantObj any
	if err := json.Unmarshal(payload, &gotObj); err != nil {
		t.Fatalf("payload not valid json: %v", err)
	}
	if err := json.Unmarshal(want, &wantObj); err != nil {
		t.Fatal(err)
	}
	gotN, _ := json.Marshal(gotObj)
	wantN, _ := json.Marshal(wantObj)
	if !bytes.Equal(gotN, wantN) {
		t.Fatalf("discovery mismatch\n got: %s\nwant: %s", gotN, wantN)
	}
}

func TestDiscoveryDiagnosticCategory(t *testing.T) {
	dev := model.Device{Node: "n", Identifier: "server-status-n"}
	m := model.Metric{Key: "disk_serial", Name: "Serial", Value: "X", Kind: model.KindText, Category: "diagnostic", Component: "disk-abc"}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	_, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	_ = json.Unmarshal(payload, &obj)
	if obj["entity_category"] != "diagnostic" {
		t.Fatalf("expected diagnostic entity_category, got %v", obj["entity_category"])
	}
	if obj["via_device"] != nil {
		t.Fatal("via_device belongs inside device, not at top level")
	}
}
```

- [ ] **Step 6: Run the discovery test to verify it fails**

Run: `go test ./internal/ha/ -run Discovery`
Expected: FAIL, `undefined: Discovery`.

- [ ] **Step 7: Write the discovery builder**

Create `internal/ha/discovery.go`:
```go
package ha

import (
	"encoding/json"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

type deviceBlock struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	SWVersion    string   `json:"sw_version,omitempty"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

type discoveryPayload struct {
	Name              string      `json:"name"`
	StateTopic        string      `json:"state_topic"`
	UniqueID          string      `json:"unique_id"`
	ObjectID          string      `json:"object_id"`
	HasEntityName     bool        `json:"has_entity_name"`
	Unit              string      `json:"unit_of_measurement,omitempty"`
	DeviceClass       string      `json:"device_class,omitempty"`
	StateClass        string      `json:"state_class,omitempty"`
	EntityCategory    string      `json:"entity_category,omitempty"`
	Icon              string      `json:"icon,omitempty"`
	PayloadOn         string      `json:"payload_on,omitempty"`
	PayloadOff        string      `json:"payload_off,omitempty"`
	AvailabilityTopic string      `json:"availability_topic"`
	PayloadAvailable  string      `json:"payload_available"`
	PayloadNotAvail   string      `json:"payload_not_available"`
	QoS               int         `json:"qos"`
	Device            deviceBlock `json:"device"`
}

// Discovery builds the retained discovery config topic and JSON payload for a metric.
func Discovery(dev model.Device, m model.Metric, sc config.SinkConfig) (string, []byte, error) {
	objectID := ObjectID(dev.Node, m.Component, m.Key)
	p := discoveryPayload{
		Name:              m.Name,
		StateTopic:        StateTopic(sc.BaseTopic, dev.Node, m.Component, m.Key),
		UniqueID:          UniqueID(dev, m),
		ObjectID:          objectID,
		HasEntityName:     true,
		AvailabilityTopic: AvailabilityTopic(sc.BaseTopic, dev.Node),
		PayloadAvailable:  "online",
		PayloadNotAvail:   "offline",
		QoS:               sc.QoS,
		Device: deviceBlock{
			Identifiers:  []string{dev.Identifier},
			Name:         dev.Name,
			Manufacturer: dev.Manufacturer,
			Model:        dev.Model,
			SWVersion:    dev.SWVersion,
		},
	}
	if dev.Parent != "" {
		p.Device.ViaDevice = "server-status-" + dev.Parent
	}
	if m.Category == "diagnostic" {
		p.EntityCategory = "diagnostic"
	}
	if m.Kind == model.KindBinarySensor {
		p.PayloadOn = "ON"
		p.PayloadOff = "OFF"
	} else {
		p.Unit = m.Unit
		p.StateClass = m.StateClass
	}
	p.DeviceClass = m.DeviceClass
	p.Icon = m.Icon

	payload, err := json.Marshal(p)
	if err != nil {
		return "", nil, err
	}
	topic := DiscoveryTopic(sc.DiscoveryPrefix, m.Kind, dev.Node, objectID)
	return topic, payload, nil
}
```

- [ ] **Step 8: Run all ha tests to verify they pass**

Run: `go test ./internal/ha/`
Expected: PASS. If the golden test fails on field values, reconcile `testdata/cpu_discovery.json` with the builder output (the test compares normalized JSON, so field order does not matter).

- [ ] **Step 9: Commit**

```bash
git add internal/ha/
git commit -m "feat: Home Assistant topics and discovery payload builder"
```

---

### Task 9: MQTT sink

**Files:**
- Create: `internal/sink/sink.go`
- Create: `internal/sink/mqtt.go`

**Interfaces:**
- Consumes: `model.Snapshot`, `model.Device`, `config.SinkConfig`, `ha.Discovery`, `ha.StateTopic`, `ha.StateValue`, `ha.AvailabilityTopic`
- Produces: `sink.Sink` interface (`Connect() error`, `Publish(model.Snapshot) error`, `Close() error`); `sink.NewMQTT(sc config.SinkConfig, dev model.Device) *sink.MQTT`; `sink.MQTT` implementing `Sink`, publishing retained discovery on (re)connect and metric states on each `Publish`, with an availability LWT of `offline` and an `online` publish after connect

- [ ] **Step 1: Add the MQTT client dependency**

Run:
```bash
go get github.com/eclipse/paho.mqtt.golang@latest
```
Expected: adds the module to `go.mod`.

- [ ] **Step 2: Write the Sink interface**

Create `internal/sink/sink.go`:
```go
// Package sink renders snapshots to output transports.
package sink

import "github.com/giovi321/server-status/internal/model"

// Sink is one output transport. Phase 1 provides MQTT.
type Sink interface {
	Connect() error
	Publish(snap model.Snapshot) error
	Close() error
}
```

- [ ] **Step 3: Write the MQTT sink**

Create `internal/sink/mqtt.go`:
```go
package sink

import (
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/ha"
	"github.com/giovi321/server-status/internal/model"
)

// MQTT publishes snapshots to an MQTT broker with Home Assistant discovery.
type MQTT struct {
	sc        config.SinkConfig
	dev       model.Device
	client    mqtt.Client
	availTopic string
	// discovered tracks which metric keys have had discovery published this connection.
	discovered map[string]bool
}

// NewMQTT builds an unconnected MQTT sink.
func NewMQTT(sc config.SinkConfig, dev model.Device) *MQTT {
	return &MQTT{
		sc:         sc,
		dev:        dev,
		availTopic: ha.AvailabilityTopic(sc.BaseTopic, dev.Node),
		discovered: map[string]bool{},
	}
}

// Connect establishes the broker connection, sets the LWT, and publishes availability online.
func (m *MQTT) Connect() error {
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", m.sc.Host, m.sc.Port)).
		SetClientID("server-status-" + m.dev.Node).
		SetKeepAlive(30 * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetMaxReconnectInterval(60 * time.Second).
		SetWill(m.availTopic, "offline", byte(m.sc.QoS), true)
	if m.sc.Username != "" {
		opts.SetUsername(m.sc.Username).SetPassword(m.sc.Password)
	}
	// On every (re)connect, republish availability and force discovery to be re-sent.
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		m.discovered = map[string]bool{}
		c.Publish(m.availTopic, byte(m.sc.QoS), true, "online")
	})

	m.client = mqtt.NewClient(opts)
	tok := m.client.Connect()
	if !tok.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("mqtt connect timeout to %s:%d", m.sc.Host, m.sc.Port)
	}
	return tok.Error()
}

// Publish sends discovery (once per connection per metric) then the current state for each metric.
func (m *MQTT) Publish(snap model.Snapshot) error {
	for _, metric := range snap.Metrics {
		if !m.discovered[metric.Key+"|"+metric.Component] {
			topic, payload, err := ha.Discovery(snap.Device, metric, m.sc)
			if err != nil {
				return err
			}
			m.client.Publish(topic, byte(m.sc.QoS), true, payload)
			m.discovered[metric.Key+"|"+metric.Component] = true
		}
		stateTopic := ha.StateTopic(m.sc.BaseTopic, snap.Device.Node, metric.Component, metric.Key)
		m.client.Publish(stateTopic, byte(m.sc.QoS), m.sc.Retain, ha.StateValue(metric))
	}
	return nil
}

// Close publishes offline and disconnects.
func (m *MQTT) Close() error {
	if m.client != nil && m.client.IsConnected() {
		tok := m.client.Publish(m.availTopic, byte(m.sc.QoS), true, "offline")
		tok.WaitTimeout(2 * time.Second)
		m.client.Disconnect(250)
	}
	return nil
}
```

- [ ] **Step 4: Verify the package builds**

Run: `go build ./internal/sink/`
Expected: no output, exit 0. (Network behavior is verified live in Task 10.)

- [ ] **Step 5: Commit**

```bash
git add internal/sink/ go.mod go.sum
git commit -m "feat: MQTT sink with Home Assistant discovery"
```

---

### Task 10: Wire main, run loop, and live verification

**Files:**
- Create: `cmd/server-status/main.go`

**Interfaces:**
- Consumes: `config.Load`, `ident.Identify`, `detect.All`, `detect.Available`, `detect.Snapshot`, `detect.DumpJSON`, `sink.NewMQTT`, `sink.Sink`, `version.Version`
- Produces: the `server-status` binary with flags `-c/--config <path>`, `--once`, `--dump-detected`, `--version`

- [ ] **Step 1: Write main**

Create `cmd/server-status/main.go`:
```go
// Command server-status publishes host metrics to MQTT with Home Assistant discovery.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/detect"
	"github.com/giovi321/server-status/internal/ident"
	"github.com/giovi321/server-status/internal/sink"
	"github.com/giovi321/server-status/internal/version"
)

func main() {
	var (
		cfgPath    = flag.String("c", "", "path to YAML config file")
		once       = flag.Bool("once", false, "run one cycle then exit")
		dump       = flag.Bool("dump-detected", false, "print detected collectors and metrics as JSON, then exit")
		showVer    = flag.Bool("version", false, "print version and exit")
		loopSecs   = flag.Int("interval", 60, "seconds between cycles")
	)
	flag.StringVar(cfgPath, "config", "", "path to YAML config file")
	flag.Parse()

	if *showVer {
		fmt.Println(version.Version)
		return
	}
	if *cfgPath == "" {
		log.Fatal("missing -c/--config")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	hostname, _ := os.Hostname()
	dev := ident.Identify(cfg, hostname)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cols := detect.Available(detect.All())

	if *dump {
		if err := detect.DumpJSON(os.Stdout, dev, cols, ctx); err != nil {
			log.Fatalf("dump: %v", err)
		}
		return
	}

	// Phase 1: one MQTT sink. Later plans generalize to a sink list.
	var sk sink.Sink
	for _, sc := range cfg.Sinks {
		if sc.Type == "mqtt" {
			sk = sink.NewMQTT(sc, dev)
			break
		}
	}
	if sk == nil {
		log.Fatal("no mqtt sink configured")
	}
	if err := sk.Connect(); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer sk.Close()

	cycle := func() {
		snap := detect.Snapshot(ctx, dev, cols)
		if err := sk.Publish(snap); err != nil {
			log.Printf("publish: %v", err)
		}
	}

	cycle()
	if *once {
		return
	}
	ticker := time.NewTicker(time.Duration(*loopSecs) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Print("shutting down")
			return
		case <-ticker.C:
			cycle()
		}
	}
}
```

- [ ] **Step 2: Build the binary**

Run: `go build -o server-status ./cmd/server-status`
Expected: builds `server-status` (or `server-status.exe` on Windows), exit 0.

- [ ] **Step 3: Verify dump mode with no network**

Run (Linux host):
```bash
cat > /tmp/ss.yaml <<'YAML'
node: testhost
sinks:
  - type: mqtt
    host: 127.0.0.1
YAML
./server-status -c /tmp/ss.yaml --dump-detected
```
Expected: JSON with a `device` block (node `testhost`, identifier `server-status-testhost`) and `collectors` array showing cpu, memory, uptime available with metric values. On a non-Linux dev machine the collectors show `available: false`, which is expected.

- [ ] **Step 4: Verify live publish against a broker (Linux host with MQTT)**

Run:
```bash
./server-status -c /tmp/ss.yaml --once
```
Then confirm with an MQTT client (adjust host/creds):
```bash
mosquitto_sub -h 127.0.0.1 -t 'homeassistant/#' -t 'server-status/#' -v -W 3
```
Expected: retained discovery configs under `homeassistant/sensor/testhost/...` and state values under `server-status/testhost/...` (cpu_usage, memory_used, memory_available, uptime), plus `server-status/testhost/availability` = `online`.

- [ ] **Step 5: Validate the device in Home Assistant**

In Home Assistant, confirm a single device named `testhost` appears with four entities (CPU usage, Memory used, Memory available, Uptime) and that entity names are short (no serials). This confirms the per-host device requirement. (During implementation this is cross-checked through the connected Home Assistant integration.)

- [ ] **Step 6: Commit**

```bash
git add cmd/server-status/main.go
git commit -m "feat: wire main run loop with dump, once, and loop modes"
```

---

### Task 11: systemd unit and install script

**Files:**
- Create: `packaging/server-status.service`
- Create: `scripts/install.sh`

**Interfaces:**
- Consumes: the built `server-status` binary
- Produces: a systemd unit installed at `/etc/systemd/system/server-status.service` and an idempotent installer

- [ ] **Step 1: Write the systemd unit**

Create `packaging/server-status.service`:
```ini
[Unit]
Description=server-status metrics publisher
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=/opt/server-status/server-status -c /etc/server-status/config.yaml
Restart=always
RestartSec=15s
TimeoutStopSec=10s
# Sandboxing that does not block hardware access; expanded in a later reliability plan.
ProtectHome=true
NoNewPrivileges=false

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Write the install script**

Create `scripts/install.sh`:
```bash
#!/usr/bin/env bash
# Install or upgrade server-status from a locally built binary.
# Usage: sudo ./scripts/install.sh [--uninstall]
set -euo pipefail

BIN_DIR=/opt/server-status
CFG_DIR=/etc/server-status
UNIT=/etc/systemd/system/server-status.service
SRC_BIN=${SRC_BIN:-./server-status}

if [[ "${1:-}" == "--uninstall" ]]; then
  systemctl disable --now server-status.service 2>/dev/null || true
  rm -f "$UNIT"
  systemctl daemon-reload
  echo "Uninstalled service. Left $CFG_DIR and $BIN_DIR in place."
  exit 0
fi

if [[ ! -f "$SRC_BIN" ]]; then
  echo "Binary not found at $SRC_BIN. Build it first: go build -o server-status ./cmd/server-status" >&2
  exit 1
fi

install -d "$BIN_DIR" "$CFG_DIR"
install -m 0755 "$SRC_BIN" "$BIN_DIR/server-status"

if [[ ! -f "$CFG_DIR/config.yaml" ]]; then
  cat > "$CFG_DIR/config.yaml" <<'YAML'
# Minimal config. See docs for all options.
node:            # defaults to hostname
sinks:
  - type: mqtt
    host: 192.168.1.65
    username: mqtt
    password: ${MQTT_PASSWORD}
YAML
  echo "Wrote default config to $CFG_DIR/config.yaml (edit it, then restart the service)."
fi

install -m 0644 packaging/server-status.service "$UNIT"
systemctl daemon-reload
systemctl enable --now server-status.service
echo "Installed and started server-status. Logs: journalctl -u server-status -f"
```

- [ ] **Step 3: Make the script executable and syntax-check it**

Run:
```bash
chmod +x scripts/install.sh
bash -n scripts/install.sh
```
Expected: no output, exit 0.

- [ ] **Step 4: Live install verification (Debian host)**

Run:
```bash
go build -o server-status ./cmd/server-status
sudo ./scripts/install.sh
sudo systemctl status server-status --no-pager
```
Expected: service active (running) after editing `/etc/server-status/config.yaml` with real broker details and running `sudo systemctl restart server-status`.

- [ ] **Step 5: Commit**

```bash
git add packaging/ scripts/
git commit -m "feat: systemd unit and install script"
```

---

### Task 12: Full build, vet, and test gate

**Files:**
- Modify: none (verification and housekeeping)
- Create: `.gitignore` entry for the built binary

**Interfaces:**
- Consumes: everything above
- Produces: a clean `go vet` and `go test ./...` pass and an ignored build artifact

- [ ] **Step 1: Ignore the built binary**

Append to `.gitignore`:
```
/server-status
/server-status.exe
```

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: all packages PASS (collector/detect show real metrics only on Linux; parser tests pass everywhere).

- [ ] **Step 3: Run vet**

Run: `go vet ./...`
Expected: no findings.

- [ ] **Step 4: Cross-compile for the target platforms**

Run:
```bash
GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/server-status
GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/server-status
```
Expected: both succeed, confirming the code is portable to the deploy targets.

- [ ] **Step 5: Commit**

```bash
git add .gitignore
git commit -m "chore: ignore built binary; foundation build/test gate green"
```

---

## Self-review against the spec

Spec coverage for Phase 1 (spec section 19, item 1: "Skeleton: module layout, config, model, scheduler, --dump-detected, one core collector, MQTT sink with per-host device and discovery, systemd unit, install script"):

- Module layout: Task 1
- Config with env interpolation and defaults: Task 2
- Model: Task 1
- Device identity per host, human-readable, fixes the collision bug (spec section 3): Task 3
- Core collectors (cpu, memory, uptime; the spec's "always on" set start): Tasks 4-6
- Autoconfiguration via `Available()` and `--dump-detected` (spec section 8): Task 7
- Home Assistant discovery with `has_entity_name`, short names, per-host device, availability (spec sections 5, 7, 10): Tasks 8-9
- MQTT sink with reconnect and retained discovery (spec section 10, basic form): Task 9
- Run loop, `--once`, dump mode: Task 10
- systemd unit and install script (spec section 13, basic form): Task 11
- Build/test/vet gate and cross-compile to the two targets (spec section 4): Task 12

Deferred by design to later plans (explicitly out of scope here per Global Constraints): webhook sink and HTTP control (Plan 06), control commands and self-update (Plan 07), sub-devices and parent nesting beyond the `via_device` wiring already present (Plan 03), SMART and storage (Plan 04), docker (Plan 05), the advanced reconnect/cached-replay and watchdog (Plan 08), and the remaining core collectors load/swap/filesystems/network/temps/apt/systemd (Plan 02).

Placeholder scan: no TBD/TODO; every code and test step contains complete content.

Type consistency: `Collector` interface (`Name`/`Available`/`Collect`) is used identically in Tasks 4-10; `model.Metric` field names are stable across collectors, `ha`, and `sink`; `config.SinkConfig` fields used in `ha.Discovery` and `sink.MQTT` match Task 2; `model.Device` fields used in `ha` and `ident` match Task 1. The `via_device` value format `server-status-<parent>` in `ha.Discovery` matches the identifier scheme from Task 3.

## Roadmap: subsequent plans

Written just-in-time after each is executed and reviewed, each a working testable slice.

- Plan 02: remaining core collectors (load, swap, filesystems with `fs_*` keys, network, hwmon temperatures, apt updates, reboot-required, systemd failed units)
- Plan 03: device hierarchy (component sub-devices, `hierarchy: grouped|flat`, disk aliasing, host-to-host `parent` end to end)
- Plan 04: rich storage (SMART curated and full, mdadm, ZFS, btrfs) and GPU
- Plan 05: docker collector (registry digest compare, container inventory, compose awareness)
- Plan 06: webhook sink and HTTP control surface, parity golden tests
- Plan 07: control commands and GitHub-Releases self-update, HA update entity, release pipeline
- Plan 08: reliability hardening (advanced reconnect with cached replay, systemd watchdog, per-collector isolation and cadence, uninstall purge)
- Plan 09: Home Assistant validation against the live instance and migration cutover
