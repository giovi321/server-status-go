package ha

import (
	"testing"

	"github.com/giovi321/server-status/internal/model"
)

func TestTopics(t *testing.T) {
	if got := StateTopic("server-status", "gc01srvr", "", "cpu_usage"); got != "server-status/gc01srvr/cpu_usage" {
		t.Fatalf("host state topic: %q", got)
	}
	if got := StateTopic("server-status", "gc01srvr", "disk-abc", "disk_temperature"); got != "server-status/gc01srvr/disk-abc/disk_temperature" {
		t.Fatalf("component state topic: %q", got)
	}
	if got := AvailabilityTopic("server-status", "gc01srvr"); got != "server-status/gc01srvr/availability" {
		t.Fatalf("availability: %q", got)
	}
	if got := ObjectID("gc01srvr", "", "cpu_usage"); got != "gc01srvr_cpu_usage" {
		t.Fatalf("object id host: %q", got)
	}
	if got := ObjectID("gc01srvr", "disk-abc", "disk_temperature"); got != "gc01srvr_disk-abc_disk_temperature" {
		t.Fatalf("object id component: %q", got)
	}
	if got := DiscoveryTopic("homeassistant", model.KindSensor, "gc01srvr", "gc01srvr_cpu_usage"); got != "homeassistant/sensor/gc01srvr/gc01srvr_cpu_usage/config" {
		t.Fatalf("discovery topic: %q", got)
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
