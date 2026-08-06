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
	// The state topic must match what the agent actually publishes, or Home
	// Assistant subscribes to a topic nothing writes and the entity stays unknown.
	if obj["state_topic"] != AgentUpdateTopic("server-status", "gc01srvr") {
		t.Fatalf("state_topic: %v", obj["state_topic"])
	}
}

// Home Assistant only offers Install when latest_version differs from
// installed_version, so both keys have to be present and correct.
func TestAgentUpdateStateOffersAnUpdate(t *testing.T) {
	body, err := AgentUpdateState("v1.0.3", "v1.0.4", "https://github.com/giovi321/server-status-go/releases/tag/v1.0.4")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("payload not valid json: %v", err)
	}
	if obj["installed_version"] != "v1.0.3" || obj["latest_version"] != "v1.0.4" {
		t.Fatalf("versions: %v", obj)
	}
	if obj["release_url"] != "https://github.com/giovi321/server-status-go/releases/tag/v1.0.4" {
		t.Fatalf("release_url: %v", obj["release_url"])
	}
}

func TestAgentUpdateStateUpToDate(t *testing.T) {
	body, _ := AgentUpdateState("v1.0.3", "v1.0.3", "")
	var obj map[string]any
	_ = json.Unmarshal(body, &obj)
	if obj["installed_version"] != obj["latest_version"] {
		t.Fatalf("equal versions must stay equal: %v", obj)
	}
	if _, ok := obj["release_url"]; ok {
		t.Fatal("an empty release url must be omitted, not sent blank")
	}
}

// Before the first successful lookup there is no latest version. Omitting the
// key reads as "not known yet"; sending it empty would read as a real value.
func TestAgentUpdateStateOmitsUnknownLatest(t *testing.T) {
	body, _ := AgentUpdateState("v1.0.3", "", "")
	var obj map[string]any
	_ = json.Unmarshal(body, &obj)
	if obj["installed_version"] != "v1.0.3" {
		t.Fatalf("installed_version: %v", obj)
	}
	if _, ok := obj["latest_version"]; ok {
		t.Fatalf("unknown latest must be omitted: %s", body)
	}
	if _, ok := obj["title"]; ok {
		t.Fatalf("title must be omitted with no latest version: %s", body)
	}
}
