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
