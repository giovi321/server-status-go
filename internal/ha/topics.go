// Package ha builds Home Assistant MQTT discovery payloads and topic names.
package ha

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/giovi321/server-status/internal/model"
)

var instNonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// InstanceSlug turns an instance label (mount, interface, sensor) into a stable [a-z0-9-] slug.
func InstanceSlug(s string) string {
	s = strings.ToLower(s)
	s = instNonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// StateTopic is where a metric's value is published.
func StateTopic(base, node, component, key, instance string) string {
	parts := base + "/" + node
	if component != "" {
		parts += "/" + component
	}
	parts += "/" + key
	if instance != "" {
		parts += "/" + InstanceSlug(instance)
	}
	return parts
}

// AvailabilityTopic is the per-host LWT topic.
func AvailabilityTopic(base, node string) string {
	return fmt.Sprintf("%s/%s/availability", base, node)
}

// ObjectID is the human-readable slug used to build the entity_id.
func ObjectID(node, component, key, instance string) string {
	id := node + "_" + key
	if component != "" {
		id = node + "_" + component + "_" + key
	}
	if instance != "" {
		id += "_" + InstanceSlug(instance)
	}
	return id
}

// UniqueID is the hidden, stable id. It may contain serials/instances via component/instance.
func UniqueID(dev model.Device, m model.Metric) string {
	id := dev.Identifier + "-" + m.Key
	if m.Component != "" {
		id = dev.Identifier + "-" + m.Component + "-" + m.Key
	}
	if m.Instance != "" {
		id += "-" + InstanceSlug(m.Instance)
	}
	return id
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
