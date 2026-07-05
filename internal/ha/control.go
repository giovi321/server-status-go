package ha

import (
	"encoding/json"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

// CommandTopic is where a control command is received.
func CommandTopic(base, node, cmd string) string {
	return base + "/" + node + "/cmd/" + cmd
}

type buttonPayload struct {
	Name         string      `json:"name"`
	UniqueID     string      `json:"unique_id"`
	ObjectID     string      `json:"object_id"`
	HasEntityN   bool        `json:"has_entity_name"`
	CommandTopic string      `json:"command_topic"`
	PayloadPress string      `json:"payload_press"`
	Availability string      `json:"availability_topic"`
	Device       deviceBlock `json:"device"`
	QoS          int         `json:"qos"`
}

// ButtonDiscovery builds discovery for a press-button that triggers a command.
func ButtonDiscovery(dev model.Device, sc config.SinkConfig, cmd, label string) (string, []byte, error) {
	object := dev.Node + "_cmd_" + cmd
	p := buttonPayload{
		Name:         label,
		UniqueID:     dev.Identifier + "-cmd-" + cmd,
		ObjectID:     object,
		HasEntityN:   true,
		CommandTopic: CommandTopic(sc.BaseTopic, dev.Node, cmd),
		PayloadPress: "1",
		Availability: AvailabilityTopic(sc.BaseTopic, dev.Node),
		QoS:          sc.QoS,
		Device:       deviceBlock{Identifiers: []string{dev.Identifier}, Name: dev.Name},
	}
	body, err := json.Marshal(p)
	topic := sc.DiscoveryPrefix + "/button/" + dev.Node + "/" + object + "/config"
	return topic, body, err
}

type updatePayload struct {
	Name           string      `json:"name"`
	UniqueID       string      `json:"unique_id"`
	ObjectID       string      `json:"object_id"`
	HasEntityN     bool        `json:"has_entity_name"`
	StateTopic     string      `json:"state_topic"`
	CommandTopic   string      `json:"command_topic"`
	PayloadInstall string      `json:"payload_install"`
	Availability   string      `json:"availability_topic"`
	Device         deviceBlock `json:"device"`
	QoS            int         `json:"qos"`
}

// UpdateDiscovery builds discovery for the agent's HA update entity. The state
// topic carries a JSON object {"installed_version","latest_version"}.
func UpdateDiscovery(dev model.Device, sc config.SinkConfig) (string, []byte, error) {
	object := dev.Node + "_agent"
	p := updatePayload{
		Name:           "Agent",
		UniqueID:       dev.Identifier + "-agent-update",
		ObjectID:       object,
		HasEntityN:     true,
		StateTopic:     sc.BaseTopic + "/" + dev.Node + "/agent/update",
		CommandTopic:   CommandTopic(sc.BaseTopic, dev.Node, "update"),
		PayloadInstall: "1",
		Availability:   AvailabilityTopic(sc.BaseTopic, dev.Node),
		QoS:            sc.QoS,
		Device:         deviceBlock{Identifiers: []string{dev.Identifier}, Name: dev.Name},
	}
	body, err := json.Marshal(p)
	topic := sc.DiscoveryPrefix + "/update/" + dev.Node + "/" + object + "/config"
	return topic, body, err
}
