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

func TestDiscoveryBinarySensor(t *testing.T) {
	dev := model.Device{Node: "n", Identifier: "server-status-n"}
	m := model.Metric{Key: "reboot_required", Name: "Reboot required", Value: true, Kind: model.KindBinarySensor, Category: "primary"}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/binary_sensor/n/n_reboot_required/config" {
		t.Fatalf("topic: %q", topic)
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["payload_on"] != "ON" || obj["payload_off"] != "OFF" {
		t.Fatalf("payload_on/off: %v/%v", obj["payload_on"], obj["payload_off"])
	}
	if _, ok := obj["unit_of_measurement"]; ok {
		t.Fatal("binary_sensor must not have unit_of_measurement")
	}
	if _, ok := obj["state_class"]; ok {
		t.Fatal("binary_sensor must not have state_class")
	}
}

func TestDiscoveryInstance(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Identifier: "server-status-gc01srvr", Name: "gc01srvr"}
	m := model.Metric{Key: "fs_usage", Instance: "root", Name: "Root usage", Value: 42, Unit: "%", StateClass: "measurement", Kind: model.KindSensor}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/sensor/gc01srvr/gc01srvr_fs_usage_root/config" {
		t.Fatalf("topic: %q", topic)
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["state_topic"] != "server-status/gc01srvr/fs_usage/root" {
		t.Fatalf("state_topic: %v", obj["state_topic"])
	}
	if obj["unique_id"] != "server-status-gc01srvr-fs_usage-root" {
		t.Fatalf("unique_id: %v", obj["unique_id"])
	}
	if obj["name"] != "Root usage" {
		t.Fatalf("name: %v", obj["name"])
	}
}

func TestDiscoveryViaDeviceWhenParentSet(t *testing.T) {
	dev := model.Device{Node: "vm-web", Identifier: "server-status-vm-web", Parent: "gc01srvr"}
	m := model.Metric{Key: "cpu_usage", Name: "CPU usage", Value: 5, Unit: "%", StateClass: "measurement", Kind: model.KindSensor}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	_, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatal(err)
	}
	device, ok := obj["device"].(map[string]any)
	if !ok {
		t.Fatal("device block missing")
	}
	if device["via_device"] != "server-status-gc01srvr" {
		t.Fatalf("via_device: %v", device["via_device"])
	}
}

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

func TestDiscoverySubDeviceWithParent(t *testing.T) {
	// A grouped sub-device on a host that itself has a Parent: the sub-device's
	// via_device must point at the HOST, not the grandparent. The host->parent
	// link lives on the host's own (Component=="") entities.
	dev := model.Device{Node: "vm1", Name: "vm1", Identifier: "server-status-vm1", Parent: "gc01srvr", Hierarchy: "grouped"}
	m := model.Metric{Key: "disk_temperature", Component: "disk-x", ComponentName: "Disk sda", Name: "Temperature", Value: 40, Unit: "°C", DeviceClass: "temperature", StateClass: "measurement", Kind: model.KindSensor}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	_, payload, err := Discovery(dev, m, sc)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatal(err)
	}
	device := obj["device"].(map[string]any)
	if device["via_device"] != "server-status-vm1" {
		t.Fatalf("sub-device via_device must be the host, not the parent: %v", device["via_device"])
	}
	ids := device["identifiers"].([]any)
	if ids[0] != "server-status-vm1-disk-x" {
		t.Fatalf("sub-device identifier: %v", ids[0])
	}
}
