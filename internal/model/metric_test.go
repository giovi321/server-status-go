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
