package ha

import (
	"encoding/json"

	"github.com/giovi321/server-status/internal/config"
	"github.com/giovi321/server-status/internal/model"
)

type deviceBlock struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	SWVersion    string   `json:"sw_version,omitempty"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

type discoveryPayload struct {
	Name              string      `json:"name"`
	StateTopic        string      `json:"state_topic"`
	UniqueID          string      `json:"unique_id"`
	ObjectID          string      `json:"object_id"`
	HasEntityName     bool        `json:"has_entity_name"`
	Unit              string      `json:"unit_of_measurement,omitempty"`
	DeviceClass       string      `json:"device_class,omitempty"`
	StateClass        string      `json:"state_class,omitempty"`
	EntityCategory    string      `json:"entity_category,omitempty"`
	Icon              string      `json:"icon,omitempty"`
	PayloadOn         string      `json:"payload_on,omitempty"`
	PayloadOff        string      `json:"payload_off,omitempty"`
	AvailabilityTopic string      `json:"availability_topic"`
	PayloadAvailable  string      `json:"payload_available"`
	PayloadNotAvail   string      `json:"payload_not_available"`
	QoS               int         `json:"qos"`
	Device            deviceBlock `json:"device"`
}

// Discovery builds the retained discovery config topic and JSON payload for a metric.
func Discovery(dev model.Device, m model.Metric, sc config.SinkConfig) (string, []byte, error) {
	objectID := ObjectID(dev.Node, m.Component, m.Key, m.Instance)
	p := discoveryPayload{
		Name:              m.Name,
		StateTopic:        StateTopic(sc.BaseTopic, dev.Node, m.Component, m.Key, m.Instance),
		UniqueID:          UniqueID(dev, m),
		ObjectID:          objectID,
		HasEntityName:     true,
		AvailabilityTopic: AvailabilityTopic(sc.BaseTopic, dev.Node),
		PayloadAvailable:  "online",
		PayloadNotAvail:   "offline",
		QoS:               sc.QoS,
	}
	if m.Component != "" && dev.Hierarchy != "flat" {
		// Sub-device: its own identifier, linked to the host via via_device.
		compName := m.ComponentName
		if compName == "" {
			compName = m.Component
		}
		p.Device = deviceBlock{
			Identifiers: []string{dev.Identifier + "-" + m.Component},
			Name:        dev.Name + " " + compName,
			ViaDevice:   dev.Identifier,
		}
	} else {
		p.Device = deviceBlock{
			Identifiers:  []string{dev.Identifier},
			Name:         dev.Name,
			Manufacturer: dev.Manufacturer,
			Model:        dev.Model,
			SWVersion:    dev.SWVersion,
		}
		if dev.Parent != "" {
			p.Device.ViaDevice = "server-status-" + dev.Parent
		}
	}
	if m.Category == "diagnostic" {
		p.EntityCategory = "diagnostic"
	}
	if m.Kind == model.KindBinarySensor {
		p.PayloadOn = "ON"
		p.PayloadOff = "OFF"
	} else {
		p.Unit = m.Unit
		p.StateClass = m.StateClass
	}
	p.DeviceClass = m.DeviceClass
	p.Icon = m.Icon

	payload, err := json.Marshal(p)
	if err != nil {
		return "", nil, err
	}
	topic := DiscoveryTopic(sc.DiscoveryPrefix, m.Kind, dev.Node, objectID)
	return topic, payload, nil
}
