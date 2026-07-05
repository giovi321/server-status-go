// Package model holds the transport-agnostic data types shared by collectors and sinks.
package model

import "time"

// Kind maps a metric to a Home Assistant entity component.
type Kind string

const (
	KindSensor       Kind = "sensor"
	KindBinarySensor Kind = "binary_sensor"
	KindText         Kind = "text"
	KindUpdate       Kind = "update"
)

// Metric is one published value. Component is the sub-device id; empty means the host device.
type Metric struct {
	Key           string `json:"key"`
	Component     string `json:"component,omitempty"`
	ComponentName string `json:"component_name,omitempty"`
	Instance      string `json:"instance,omitempty"`
	Name          string `json:"name"`
	Value         any    `json:"value"`
	Unit          string `json:"unit,omitempty"`
	DeviceClass   string `json:"device_class,omitempty"`
	StateClass    string `json:"state_class,omitempty"`
	Kind          Kind   `json:"kind"`
	Category      string `json:"category,omitempty"` // "primary" or "diagnostic"
	Icon          string `json:"icon,omitempty"`
}

// IsHost reports whether the metric attaches to the host device rather than a sub-device.
func (m Metric) IsHost() bool { return m.Component == "" }

// Device identifies the host (and, via Parent, its parent host).
type Device struct {
	Node         string `json:"node"`
	Name         string `json:"name"`
	Identifier   string `json:"identifier"`
	Parent       string `json:"parent,omitempty"`
	Hierarchy    string `json:"hierarchy,omitempty"`
	Model        string `json:"model,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	SWVersion    string `json:"sw_version,omitempty"`
}

// Snapshot is the full set of metrics for one host at one instant.
type Snapshot struct {
	Device  Device    `json:"device"`
	TS      time.Time `json:"ts"`
	Metrics []Metric  `json:"metrics"`
}
