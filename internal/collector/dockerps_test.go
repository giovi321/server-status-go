package collector

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/giovi321/server-status/internal/ha"
	"github.com/giovi321/server-status/internal/model"
)

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

// docker_containers used to publish the whole JSON inventory as its state, which
// Home Assistant rejects past 255 characters. The state is now the count and the
// inventory moved to attributes; the key and sub-device must not move with it,
// or every existing sensor.<host>_docker_containers reference breaks.
func TestDockerContainersStateIsCountAndInventoryIsAttributes(t *testing.T) {
	cs := parseDockerPS(dockerPSOut)
	var m model.Metric
	for _, x := range dockerMetrics(cs, map[string]bool{"nginx:latest": true}) {
		if x.Key == "docker_containers" {
			m = x
		}
	}
	if m.Key == "" {
		t.Fatal("docker_containers metric missing")
	}
	if m.Component != "docker" || m.ComponentName != "Docker" || m.Name != "Containers" {
		t.Fatalf("identity moved: %+v", m)
	}
	if m.Value != len(cs) {
		t.Fatalf("state should be the container count, got %v", m.Value)
	}
	if got := ha.StateValue(m); len([]rune(got)) > ha.MaxStateLength {
		t.Fatalf("state is %d chars, over Home Assistant's limit", len([]rune(got)))
	}
	reports, ok := m.Attributes["containers"].([]containerReport)
	if !ok || len(reports) != len(cs) {
		t.Fatalf("attributes must carry every container: %#v", m.Attributes)
	}
	// The per-container health and update flags exist nowhere else.
	var db, nginx containerReport
	for _, r := range reports {
		switch r.Name {
		case "web-db-1":
			db = r
		case "web-nginx-1":
			nginx = r
		}
	}
	if db.Health != "unhealthy" {
		t.Fatalf("unhealthy flag lost: %+v", db)
	}
	if !nginx.Update {
		t.Fatalf("update flag lost: %+v", nginx)
	}
}

// A busy host is where the 255-character limit used to bite.
func TestDockerContainersStateFitsWithManyContainers(t *testing.T) {
	cs := make([]ContainerInfo, 300)
	for i := range cs {
		cs[i] = ContainerInfo{Name: fmt.Sprintf("service-with-a-long-name-%03d", i), Image: "ghcr.io/example/some-image:1.2.3", State: "running", Health: "healthy", Project: "stack"}
	}
	var m model.Metric
	for _, x := range dockerMetrics(cs, nil) {
		if x.Key == "docker_containers" {
			m = x
		}
	}
	if got := ha.StateValue(m); len([]rune(got)) > ha.MaxStateLength {
		t.Fatalf("state is %d chars, over Home Assistant's limit", len([]rune(got)))
	}
	b, err := json.Marshal(m.Attributes)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > ha.MaxAttributesBytes {
		t.Fatalf("attributes %d bytes exceed the %d-byte recorder cap", len(b), ha.MaxAttributesBytes)
	}
}
