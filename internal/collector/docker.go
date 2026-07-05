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

func (*Docker) Name() string { return "docker" }

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
