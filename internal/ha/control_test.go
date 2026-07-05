package ha

import (
	"encoding/json"
	"testing"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

func TestButtonDiscovery(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr"}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := ButtonDiscovery(dev, sc, "refresh", "Refresh")
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/button/gc01srvr/gc01srvr_cmd_refresh/config" {
		t.Fatalf("topic: %q", topic)
	}
	var obj map[string]any
	_ = json.Unmarshal(payload, &obj)
	if obj["command_topic"] != "server-status/gc01srvr/cmd/refresh" {
		t.Fatalf("command_topic: %v", obj["command_topic"])
	}
	if obj["payload_press"] != "1" {
		t.Fatalf("payload_press: %v", obj["payload_press"])
	}
}

func TestUpdateDiscovery(t *testing.T) {
	dev := model.Device{Node: "gc01srvr", Name: "gc01srvr", Identifier: "server-status-gc01srvr"}
	sc := config.SinkConfig{BaseTopic: "server-status", DiscoveryPrefix: "homeassistant"}
	topic, payload, err := UpdateDiscovery(dev, sc)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "homeassistant/update/gc01srvr/gc01srvr_agent/config" {
		t.Fatalf("topic: %q", topic)
	}
	var obj map[string]any
	_ = json.Unmarshal(payload, &obj)
	if obj["command_topic"] != "server-status/gc01srvr/cmd/update" {
		t.Fatalf("command_topic: %v", obj["command_topic"])
	}
}
