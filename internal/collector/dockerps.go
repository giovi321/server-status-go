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
