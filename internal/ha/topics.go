// Package ha builds Home Assistant MQTT discovery payloads and topic names.
package ha

import (
	"fmt"
	"strconv"

	"github.com/giovi321/server-status/internal/model"
)

// StateTopic is where a metric's value is published.
func StateTopic(base, node, component, key string) string {
	if component == "" {
		return fmt.Sprintf("%s/%s/%s", base, node, key)
	}
	return fmt.Sprintf("%s/%s/%s/%s", base, node, component, key)
}

// AvailabilityTopic is the per-host LWT topic.
func AvailabilityTopic(base, node string) string {
	return fmt.Sprintf("%s/%s/availability", base, node)
}

// ObjectID is the human-readable slug used to build the entity_id.
func ObjectID(node, component, key string) string {
	if component == "" {
		return node + "_" + key
	}
	return node + "_" + component + "_" + key
}

// UniqueID is the hidden, stable id. It may contain serials via the component.
func UniqueID(dev model.Device, m model.Metric) string {
	if m.Component == "" {
		return dev.Identifier + "-" + m.Key
	}
	return dev.Identifier + "-" + m.Component + "-" + m.Key
}

// Component maps a metric kind to its Home Assistant discovery component.
func Component(k model.Kind) string {
	if k == model.KindBinarySensor {
		return "binary_sensor"
	}
	return "sensor"
}

// DiscoveryTopic is where the retained discovery config is published.
func DiscoveryTopic(prefix string, k model.Kind, node, objectID string) string {
	return fmt.Sprintf("%s/%s/%s/%s/config", prefix, Component(k), node, objectID)
}

// StateValue renders a metric value into its MQTT string payload.
func StateValue(m model.Metric) string {
	if m.Kind == model.KindBinarySensor {
		if b, ok := m.Value.(bool); ok {
			if b {
				return "ON"
			}
			return "OFF"
		}
	}
	switch v := m.Value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
