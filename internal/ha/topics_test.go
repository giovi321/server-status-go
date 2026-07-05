package ha

import (
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

func TestTopics(t *testing.T) {
	if got := StateTopic("server-status", "gc01srvr", "", "cpu_usage", ""); got != "server-status/gc01srvr/cpu_usage" {
		t.Fatalf("host state topic: %q", got)
	}
	if got := StateTopic("server-status", "gc01srvr", "disk-abc", "disk_temperature", ""); got != "server-status/gc01srvr/disk-abc/disk_temperature" {
		t.Fatalf("component state topic: %q", got)
	}
	if got := AvailabilityTopic("server-status", "gc01srvr"); got != "server-status/gc01srvr/availability" {
		t.Fatalf("availability: %q", got)
	}
	if got := ObjectID("gc01srvr", "", "cpu_usage", ""); got != "gc01srvr_cpu_usage" {
		t.Fatalf("object id host: %q", got)
	}
	if got := ObjectID("gc01srvr", "disk-abc", "disk_temperature", ""); got != "gc01srvr_disk-abc_disk_temperature" {
		t.Fatalf("object id component: %q", got)
	}
	if got := DiscoveryTopic("homeassistant", model.KindSensor, "gc01srvr", "gc01srvr_cpu_usage"); got != "homeassistant/sensor/gc01srvr/gc01srvr_cpu_usage/config" {
		t.Fatalf("discovery topic: %q", got)
	}
}

func TestInstanceSlug(t *testing.T) {
	cases := map[string]string{
		"root":         "root",
		"/dev/sda1":    "dev-sda1",
		"Package id 0": "package-id-0",
		"eth0":         "eth0",
		"/":            "root",
		"---":          "root",
	}
	for in, want := range cases {
		if got := InstanceSlug(in); got != want {
			t.Errorf("InstanceSlug(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTopicsWithInstance(t *testing.T) {
	if got := StateTopic("server-status", "gc01srvr", "", "fs_usage", "root"); got != "server-status/gc01srvr/fs_usage/root" {
		t.Fatalf("instance state topic: %q", got)
	}
	if got := ObjectID("gc01srvr", "", "fs_usage", "root"); got != "gc01srvr_fs_usage_root" {
		t.Fatalf("instance object id: %q", got)
	}
	dev := model.Device{Identifier: "server-status-gc01srvr"}
	if got := UniqueID(dev, model.Metric{Key: "fs_usage", Instance: "root"}); got != "server-status-gc01srvr-fs_usage-root" {
		t.Fatalf("instance unique id: %q", got)
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

func TestUniqueID(t *testing.T) {
	dev := model.Device{Identifier: "server-status-gc01srvr"}
	host := UniqueID(dev, model.Metric{Key: "cpu_usage"})
	if host != "server-status-gc01srvr-cpu_usage" {
		t.Fatalf("host unique_id: %q", host)
	}
	comp := UniqueID(dev, model.Metric{Key: "disk_temperature", Component: "disk-abc"})
	if comp != "server-status-gc01srvr-disk-abc-disk_temperature" {
		t.Fatalf("component unique_id: %q", comp)
	}
}
