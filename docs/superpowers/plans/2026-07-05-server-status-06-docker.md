# server-status docker (Plan 06) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a docker collector that reports running/stopped/unhealthy/restarting container counts, a per-container inventory (name, image, state, health, compose project), and a non-destructive "updates available" scan (compare each image's local digest to the registry manifest digest without pulling). Docker is published as a single Home Assistant sub-device.

**Architecture:** One collector in `internal/collector`. Container inventory comes from `docker ps -a --format '{{json .}}'` (one JSON object per line) — solid and fixture-testable. Update detection compares the local RepoDigest to the registry digest (via `skopeo inspect` preferred, `docker manifest inspect` fallback) and never pulls; its parsers are fixture-tested but the exec orchestration is best-effort and will need tuning against real docker/registries at deploy time (no docker exists on the WSL build host).

**Tech Stack:** Go 1.22+, existing deps (`os/exec`, `encoding/json`). Docker is queried via the `docker`/`skopeo` CLIs (no docker SDK dependency).

## Global Constraints

- Build/test only in WSL Debian: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && <go cmd>'`. Files edited on Windows; git commits on Windows with the two-line `giovi321` / `Claude-Session:` footer.
- `gofmt -w` new/changed Go files before committing; `gofmt -l .` stays empty.
- Canonical snake_case keys: `docker_running`, `docker_stopped`, `docker_restarting`, `docker_unhealthy`, `docker_updates_available` (counts), `docker_containers` (diagnostic text: JSON array of per-container name/image/state/health/project/update). Docker is ONE sub-device: `Component = "docker"`, `ComponentName = "Docker"`.
- Update detection NEVER pulls images. It compares local digest to registry digest; a fetch failure (auth/rate-limit/offline) for an image is treated as "unknown / no update", never as an error that drops the whole collector.
- The update scan is cached in memory on a min-interval (default 21600s / 6h), first cycle runs it, mirroring the SMART collector's cache discipline (mutex-guarded).
- Non-regression: existing collectors unchanged. `detect.All(cfg)` gains the docker collector.
- No webhook/HTTP/self-update. btrfs remains deferred.

## Prerequisites

- Plans 01-05 complete on `main` (repo giovi321/server-status-go). Sub-device discovery (Plan 03) and `detect.All(cfg)` exist; the SMART collector's mutex-cache pattern is the reference for the update-scan cache.
- `docker` (and optionally `skopeo`) present on the target server. NOT present in WSL — parsers are fixture-tested; live validation is deferred to a real docker host.

## File structure

```
internal/collector/dockerps.go       # CREATE: ContainerInfo + parseDockerPS + health/project extraction
internal/collector/dockerps_test.go   # CREATE: docker ps --format json fixtures
internal/collector/dockerscan.go      # CREATE: digest parsers + compare (update detection)
internal/collector/dockerscan_test.go # CREATE: RepoDigests/skopeo digest fixtures
internal/collector/docker.go          # CREATE: Docker collector (inventory + cached update scan)
internal/detect/detect.go             # MODIFY: register Docker in All(cfg)
```

---

### Task 1: Container inventory parser

**Files:**
- Create: `internal/collector/dockerps.go`, `internal/collector/dockerps_test.go`

**Interfaces:**
- Produces: `collector.ContainerInfo{Name, Image, State, Health, Project string}`; `collector.parseDockerPS(out string) []ContainerInfo`; `collector.containerHealth(status string) string`; `collector.composeProject(labels string) string`

- [ ] **Step 1: Write the failing test**

Create `internal/collector/dockerps_test.go`:
```go
package collector

import "testing"

// `docker ps -a --format '{{json .}}'` — one JSON object per line.
const dockerPSOut = `{"Image":"nginx:latest","Labels":"com.docker.compose.project=web,com.docker.compose.service=nginx","Names":"web-nginx-1","State":"running","Status":"Up 2 hours (healthy)"}
{"Image":"postgres:16","Labels":"com.docker.compose.project=web,com.docker.compose.service=db","Names":"web-db-1","State":"running","Status":"Up 2 hours (unhealthy)"}
{"Image":"redis:7","Labels":"","Names":"cache","State":"exited","Status":"Exited (0) 3 days ago"}
{"Image":"busybox","Labels":"","Names":"flaky","State":"restarting","Status":"Restarting (1) 5 seconds ago"}`

func TestParseDockerPS(t *testing.T) {
	cs := parseDockerPS(dockerPSOut)
	if len(cs) != 4 {
		t.Fatalf("expected 4 containers, got %d", len(cs))
	}
	by := map[string]ContainerInfo{}
	for _, c := range cs {
		by[c.Name] = c
	}
	if by["web-nginx-1"].State != "running" || by["web-nginx-1"].Health != "healthy" || by["web-nginx-1"].Project != "web" {
		t.Fatalf("nginx: %+v", by["web-nginx-1"])
	}
	if by["web-db-1"].Health != "unhealthy" {
		t.Fatalf("db health: %+v", by["web-db-1"])
	}
	if by["cache"].State != "exited" || by["cache"].Health != "none" || by["cache"].Project != "" {
		t.Fatalf("cache: %+v", by["cache"])
	}
	if by["flaky"].State != "restarting" {
		t.Fatalf("flaky: %+v", by["flaky"])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run DockerPS'`
Expected: FAIL, `undefined: parseDockerPS`.

- [ ] **Step 3: Implement**

Create `internal/collector/dockerps.go`:
```go
package collector

import (
	"encoding/json"
	"strings"
)

// ContainerInfo is one container from `docker ps -a --format '{{json .}}'`.
type ContainerInfo struct {
	Name, Image, State, Health, Project string
}

type dockerPSLine struct {
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Labels string `json:"Labels"`
}

// containerHealth extracts the health substring from a docker Status field.
func containerHealth(status string) string {
	l := strings.ToLower(status)
	switch {
	case strings.Contains(l, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(l, "(healthy)"):
		return "healthy"
	case strings.Contains(l, "health: starting"):
		return "starting"
	default:
		return "none"
	}
}

// composeProject extracts com.docker.compose.project from a comma-joined Labels string.
func composeProject(labels string) string {
	for _, kv := range strings.Split(labels, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if ok && k == "com.docker.compose.project" {
			return v
		}
	}
	return ""
}

// parseDockerPS parses one-JSON-object-per-line docker ps output.
func parseDockerPS(out string) []ContainerInfo {
	var cs []ContainerInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var d dockerPSLine
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			continue
		}
		cs = append(cs, ContainerInfo{
			Name:    strings.TrimPrefix(d.Names, "/"),
			Image:   d.Image,
			State:   d.State,
			Health:  containerHealth(d.Status),
			Project: composeProject(d.Labels),
		})
	}
	return cs
}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/dockerps.go && go build ./... && go test ./internal/collector/ -run DockerPS && go vet ./...'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/dockerps.go internal/collector/dockerps_test.go && git commit -F - <<'EOF'
feat: docker container inventory parser

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 2: Update-detection digest parsers and compare

**Files:**
- Create: `internal/collector/dockerscan.go`, `internal/collector/dockerscan_test.go`

**Interfaces:**
- Produces: `collector.parseRepoDigest(reposDigestsJSON string) string` (extracts the `sha256:...` from a `docker image inspect --format '{{json .RepoDigests}}'` array); `collector.updateAvailable(local, registry string) bool` (true only when both non-empty and differ)

- [ ] **Step 1: Write the failing test**

Create `internal/collector/dockerscan_test.go`:
```go
package collector

import "testing"

func TestParseRepoDigest(t *testing.T) {
	// docker image inspect --format '{{json .RepoDigests}}'
	in := `["nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]`
	if got := parseRepoDigest(in); got != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("digest: %q", got)
	}
	// multiple repo digests: take the first sha256
	in2 := `["repo1@sha256:bbbb","repo2@sha256:cccc"]`
	if got := parseRepoDigest(in2); got != "sha256:bbbb" {
		t.Fatalf("first digest: %q", got)
	}
	// no digest (image built locally, never pushed)
	if got := parseRepoDigest(`[]`); got != "" {
		t.Fatalf("empty should be '': %q", got)
	}
	if got := parseRepoDigest(`null`); got != "" {
		t.Fatalf("null should be '': %q", got)
	}
}

func TestUpdateAvailable(t *testing.T) {
	if !updateAvailable("sha256:aaa", "sha256:bbb") {
		t.Fatal("different digests -> update available")
	}
	if updateAvailable("sha256:aaa", "sha256:aaa") {
		t.Fatal("same digest -> no update")
	}
	// unknown (fetch failed) -> not an update, not an error
	if updateAvailable("sha256:aaa", "") {
		t.Fatal("empty registry digest -> unknown -> no update")
	}
	if updateAvailable("", "sha256:bbb") {
		t.Fatal("empty local digest -> unknown -> no update")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run "RepoDigest|UpdateAvailable"'`
Expected: FAIL, `undefined: parseRepoDigest`.

- [ ] **Step 3: Implement**

Create `internal/collector/dockerscan.go`:
```go
package collector

import (
	"encoding/json"
	"strings"
)

// parseRepoDigest extracts the first sha256 digest from a
// `docker image inspect --format '{{json .RepoDigests}}'` JSON array.
// Returns "" when the image has no repo digest (built locally, never pushed).
func parseRepoDigest(reposDigestsJSON string) string {
	var repos []string
	if err := json.Unmarshal([]byte(reposDigestsJSON), &repos); err != nil {
		return ""
	}
	for _, r := range repos {
		if _, digest, ok := strings.Cut(r, "@"); ok {
			return digest
		}
	}
	return ""
}

// updateAvailable reports whether the registry digest differs from the local one.
// Either digest being empty means "unknown" (fetch failed / local-only image) and
// is treated as no update, never as an error.
func updateAvailable(local, registry string) bool {
	if local == "" || registry == "" {
		return false
	}
	return local != registry
}
```

- [ ] **Step 4: gofmt, run, verify PASS**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/dockerscan.go && go build ./... && go test ./internal/collector/ -run "RepoDigest|UpdateAvailable"'`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/dockerscan.go internal/collector/dockerscan_test.go && git commit -F - <<'EOF'
feat: docker image update-detection digest parsing

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 3: Docker collector

**Files:**
- Create: `internal/collector/docker.go`

**Interfaces:**
- Consumes: `parseDockerPS`, `ContainerInfo`, `parseRepoDigest`, `updateAvailable`
- Produces: `collector.NewDocker() *collector.Docker` implementing `Collector` (pointer receiver `Collect` for the update-scan cache); `collector.dockerMetrics(cs []ContainerInfo, updates map[string]bool) []model.Metric`

- [ ] **Step 1: Write a metrics unit test**

Create the collector, then a test. First append to `internal/collector/dockerps_test.go`:
```go
import "github.com/giovi321/server-status/internal/model" // add to the import block if not present

func TestDockerMetrics(t *testing.T) {
	cs := parseDockerPS(dockerPSOut)
	updates := map[string]bool{"nginx:latest": true} // pretend nginx has an update
	ms := dockerMetrics(cs, updates)
	by := map[string]model.Metric{}
	for _, m := range ms {
		by[m.Key] = m
	}
	if by["docker_running"].Value != 2 {
		t.Fatalf("running: %v", by["docker_running"].Value)
	}
	if by["docker_unhealthy"].Value != 1 {
		t.Fatalf("unhealthy: %v", by["docker_unhealthy"].Value)
	}
	if by["docker_restarting"].Value != 1 {
		t.Fatalf("restarting: %v", by["docker_restarting"].Value)
	}
	if by["docker_stopped"].Value != 1 { // the exited "cache"
		t.Fatalf("stopped: %v", by["docker_stopped"].Value)
	}
	if by["docker_updates_available"].Value != 1 {
		t.Fatalf("updates: %v", by["docker_updates_available"].Value)
	}
	if by["docker_running"].Component != "docker" || by["docker_running"].ComponentName != "Docker" {
		t.Fatalf("sub-device: %+v", by["docker_running"])
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go test ./internal/collector/ -run DockerMetrics'`
Expected: FAIL, `undefined: dockerMetrics`.

- [ ] **Step 3: Implement the collector**

Create `internal/collector/docker.go`:
```go
package collector

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/giovi321/server-status/internal/model"
)

func dockerPath() string {
	for _, p := range []string{"/usr/bin/docker", "/bin/docker", "/usr/local/bin/docker"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func skopeoPath() string {
	for _, p := range []string{"/usr/bin/skopeo", "/bin/skopeo"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func dockerAvailable() bool {
	if dockerPath() == "" {
		return false
	}
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}
	// socket may be elsewhere; treat presence of the CLI as available and let Collect no-op on error
	return true
}

func dockerState(cs []ContainerInfo, want string) int {
	n := 0
	for _, c := range cs {
		if c.State == want {
			n++
		}
	}
	return n
}

type containerReport struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Health  string `json:"health"`
	Project string `json:"project,omitempty"`
	Update  bool   `json:"update,omitempty"`
}

func dockerMetrics(cs []ContainerInfo, updates map[string]bool) []model.Metric {
	const comp, name = "docker", "Docker"
	running := dockerState(cs, "running")
	restarting := dockerState(cs, "restarting")
	stopped := dockerState(cs, "exited") + dockerState(cs, "created") + dockerState(cs, "dead")
	unhealthy := 0
	updatesAvail := 0
	reports := make([]containerReport, 0, len(cs))
	seenUpdate := map[string]bool{}
	for _, c := range cs {
		if c.Health == "unhealthy" {
			unhealthy++
		}
		up := updates[c.Image]
		if up && !seenUpdate[c.Image] {
			updatesAvail++
			seenUpdate[c.Image] = true
		}
		reports = append(reports, containerReport{Name: c.Name, Image: c.Image, State: c.State, Health: c.Health, Project: c.Project, Update: up})
	}
	listJSON, _ := json.Marshal(reports)
	count := func(key, leaf string, v int, icon string) model.Metric {
		return model.Metric{Key: key, Component: comp, ComponentName: name, Name: leaf, Value: v, StateClass: "measurement", Kind: model.KindSensor, Category: "primary", Icon: icon}
	}
	return []model.Metric{
		count("docker_running", "Running", running, "mdi:docker"),
		count("docker_stopped", "Stopped", stopped, "mdi:docker"),
		count("docker_restarting", "Restarting", restarting, "mdi:restart-alert"),
		count("docker_unhealthy", "Unhealthy", unhealthy, "mdi:heart-broken"),
		count("docker_updates_available", "Updates available", updatesAvail, "mdi:package-up"),
		{Key: "docker_containers", Component: comp, ComponentName: name, Name: "Containers", Value: string(listJSON), Kind: model.KindText, Category: "diagnostic"},
	}
}

// Docker publishes container inventory and a cached image-update scan as a sub-device.
type Docker struct {
	scanInterval time.Duration
	mu           sync.Mutex
	updates      map[string]bool
	updatesAt    time.Time
}

func NewDocker() *Docker { return &Docker{scanInterval: 21600 * time.Second} }

func (Docker) Name() string { return "docker" }

func (*Docker) Available() bool { return dockerAvailable() }

func (d *Docker) Collect(ctx context.Context) ([]model.Metric, error) {
	bin := dockerPath()
	if bin == "" {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, bin, "ps", "-a", "--format", "{{json .}}").Output()
	if err != nil && len(out) == 0 {
		return nil, nil
	}
	cs := parseDockerPS(string(out))

	d.mu.Lock()
	fresh := d.updates != nil && time.Since(d.updatesAt) < d.scanInterval
	updates := d.updates
	d.mu.Unlock()
	if !fresh {
		updates = d.scanUpdates(ctx, cs)
		d.mu.Lock()
		d.updates = updates
		d.updatesAt = time.Now()
		d.mu.Unlock()
	}
	return dockerMetrics(cs, updates), nil
}

// scanUpdates compares each unique running image's local digest to the registry
// digest without pulling. Best-effort: any fetch failure yields "no update" for
// that image. Prefers skopeo; falls back to `docker manifest inspect`.
func (d *Docker) scanUpdates(ctx context.Context, cs []ContainerInfo) map[string]bool {
	res := map[string]bool{}
	seen := map[string]bool{}
	bin := dockerPath()
	for _, c := range cs {
		if c.State != "running" || seen[c.Image] {
			continue
		}
		seen[c.Image] = true
		local := localDigest(ctx, bin, c.Image)
		registry := registryDigest(ctx, c.Image)
		res[c.Image] = updateAvailable(local, registry)
	}
	return res
}

func localDigest(ctx context.Context, dockerBin, image string) string {
	if dockerBin == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, dockerBin, "image", "inspect", image, "--format", "{{json .RepoDigests}}").Output()
	if err != nil {
		return ""
	}
	return parseRepoDigest(strings.TrimSpace(string(out)))
}

func registryDigest(ctx context.Context, image string) string {
	if sk := skopeoPath(); sk != "" {
		out, err := exec.CommandContext(ctx, sk, "inspect", "--no-tags", "--format", "{{.Digest}}", "docker://"+image).Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	if db := dockerPath(); db != "" {
		out, err := exec.CommandContext(ctx, db, "manifest", "inspect", "--verbose", image).Output()
		if err == nil {
			return parseManifestDigest(strings.TrimSpace(string(out)))
		}
	}
	return ""
}
```
Add `parseManifestDigest` to `internal/collector/dockerscan.go` (with a fixture test appended to dockerscan_test.go):
```go
// parseManifestDigest extracts a digest from `docker manifest inspect --verbose`
// output, which is either an object with .Descriptor.digest or an array of such.
func parseManifestDigest(out string) string {
	type entry struct {
		Descriptor struct {
			Digest string `json:"digest"`
		} `json:"Descriptor"`
	}
	var one entry
	if err := json.Unmarshal([]byte(out), &one); err == nil && one.Descriptor.Digest != "" {
		return one.Descriptor.Digest
	}
	var many []entry
	if err := json.Unmarshal([]byte(out), &many); err == nil {
		for _, e := range many {
			if e.Descriptor.Digest != "" {
				return e.Descriptor.Digest
			}
		}
	}
	return ""
}
```
Append to `internal/collector/dockerscan_test.go`:
```go
func TestParseManifestDigest(t *testing.T) {
	single := `{"Ref":"docker.io/library/nginx:latest","Descriptor":{"digest":"sha256:dddd"}}`
	if got := parseManifestDigest(single); got != "sha256:dddd" {
		t.Fatalf("single: %q", got)
	}
	list := `[{"Descriptor":{"digest":"sha256:eeee"}},{"Descriptor":{"digest":"sha256:ffff"}}]`
	if got := parseManifestDigest(list); got != "sha256:eeee" {
		t.Fatalf("list: %q", got)
	}
	if got := parseManifestDigest("garbage"); got != "" {
		t.Fatalf("garbage: %q", got)
	}
}
```

- [ ] **Step 4: gofmt, build, run all docker tests**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/collector/ && go build ./... && go test ./internal/collector/ -run "Docker|RepoDigest|UpdateAvailable|ManifestDigest" && go vet ./...'`
Expected: build clean, all docker tests pass. If `go vet` flags copylock on `Name`/`Available` (Docker has a sync.Mutex), make them pointer receivers `(*Docker)` — `NewDocker` returns `*Docker` and detect registers `NewDocker()`.

- [ ] **Step 5: Commit**

```bash
cd "Z:/git/server-status" && git add internal/collector/docker.go internal/collector/dockerscan.go internal/collector/dockerscan_test.go internal/collector/dockerps_test.go && git commit -F - <<'EOF'
feat: docker collector (inventory + cached registry-digest update scan)

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

### Task 4: Register docker and gate

**Files:**
- Modify: `internal/detect/detect.go`

- [ ] **Step 1: Register**

In `internal/detect/detect.go`, add `collector.NewDocker()` to the `All(cfg)` slice after `collector.Zfs{}` (NewDocker returns `*Docker`, which satisfies Collector).

- [ ] **Step 2: Full gate**

Run: `wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && gofmt -w internal/ && go build ./... && go vet ./... && go test ./... && gofmt -l .'`
Expected: build/vet clean, all tests pass, gofmt empty.

- [ ] **Step 3: Live dump-detected on WSL (docker available:false — no docker on WSL)**

Run:
```bash
wsl.exe -d Debian -- bash -lc 'cd /mnt/z/git/server-status && go build -o server-status ./cmd/server-status && printf "node: wsltest\nsinks:\n  - type: mqtt\n    host: 127.0.0.1\n" > /tmp/ss.yaml && ./server-status -c /tmp/ss.yaml --dump-detected | grep -E "\"name\": \"docker\"" -A1; rm -f /tmp/ss.yaml'
```
Expected: `docker` appears with `available: false` on WSL (no docker) — correct autodetection.

- [ ] **Step 4: Commit**

```bash
cd "Z:/git/server-status" && git add internal/detect/detect.go && git commit -F - <<'EOF'
feat: register docker collector

giovi321
Claude-Session: https://claude.ai/code/session_01SwDX7yf8o7nQd4iURr7EUX
EOF
```

---

## Self-review against the spec

Spec Phase 5 (docker): container inventory (running/stopped/unhealthy/restarting + compose project) → Tasks 1, 3; non-destructive registry-digest update scan → Tasks 2, 3; docker as a sub-device → Task 3; registration → Task 4.

Never-pull guarantee: `scanUpdates` only calls `docker image inspect` (local metadata), `skopeo inspect` / `docker manifest inspect` (registry metadata) — none pull. Any fetch failure → `updateAvailable` returns false (unknown, not error), so the collector degrades gracefully.

Cache: the update scan is mutex-guarded and cached on a 6h interval (SMART pattern), so it does not hammer registries every cycle.

Live-validation caveat: no docker on the WSL build host, so the exec orchestration (`docker ps` format, `docker image inspect` RepoDigests, skopeo/manifest digest) is fixture-tested at the parsing layer but the end-to-end scan MUST be validated against real docker + your registries at deploy time — private-registry auth, Docker Hub rate limits, and multi-arch manifest-list digests are the likely tuning points.

Placeholder scan: every code and test step contains complete content.

## Roadmap: subsequent plans

- Plan 07: webhook sink + HTTP control surface, parity golden tests (fully WSL-validatable — delivers n8n parity)
- Plan 08: control commands + GitHub-Releases self-update, HA update entity, release pipeline
- Plan 09: reliability hardening + migration cutover
- Optional: btrfs collector
