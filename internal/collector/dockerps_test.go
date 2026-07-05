package collector

import (
	"testing"

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
