package model

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestSnapshotJSONSnakeCase(t *testing.T) {
	snap := Snapshot{
		Device:  Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr", SWVersion: "dev"},
		Metrics: []Metric{{Key: "cpu_usage", Name: "CPU usage", Value: 5, Unit: "%", DeviceClass: "", StateClass: "measurement", Kind: KindSensor}},
	}
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"device"`, `"node":"gc01srvr"`, `"sw_version":"dev"`, `"metrics"`, `"key":"cpu_usage"`, `"state_class":"measurement"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	// omitempty: empty device_class must be absent
	if strings.Contains(s, `"device_class"`) {
		t.Fatalf("empty device_class should be omitted: %s", s)
	}
}
