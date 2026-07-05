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
	Key         string
	Component   string
	Instance    string
	Name        string
	Value       any
	Unit        string
	DeviceClass string
	StateClass  string
	Kind        Kind
	Category    string // "primary" or "diagnostic"
	Icon        string
}

// IsHost reports whether the metric attaches to the host device rather than a sub-device.
func (m Metric) IsHost() bool { return m.Component == "" }

// Device identifies the host (and, via Parent, its parent host).
type Device struct {
	Node         string
	Name         string
	Identifier   string
	Parent       string
	Model        string
	Manufacturer string
	SWVersion    string
}

// Snapshot is the full set of metrics for one host at one instant.
type Snapshot struct {
	Device  Device
	TS      time.Time
	Metrics []Metric
}
