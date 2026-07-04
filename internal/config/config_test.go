package config

import (
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_MQTT_PASSWORD", "s3cret")
	got := string(ExpandEnv([]byte("password: ${TEST_MQTT_PASSWORD}")))
	if got != "password: s3cret" {
		t.Fatalf("got %q", got)
	}
	// Unset variables expand to empty, and a literal $ that is not ${...} is left alone.
	if string(ExpandEnv([]byte("a: ${NOPE} b: $5"))) != "a:  b: $5" {
		t.Fatalf("unexpected expansion: %q", ExpandEnv([]byte("a: ${NOPE} b: $5")))
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("TEST_MQTT_PASSWORD", "s3cret")
	cfg, err := Load("testdata/minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node != "gc01srvr" {
		t.Fatalf("node: %q", cfg.Node)
	}
	if cfg.Hierarchy != "grouped" {
		t.Fatalf("default hierarchy: %q", cfg.Hierarchy)
	}
	if len(cfg.Sinks) != 1 {
		t.Fatalf("sinks: %d", len(cfg.Sinks))
	}
	s := cfg.Sinks[0]
	if s.Host != "192.168.1.65" || s.Port != 1883 {
		t.Fatalf("host/port: %q/%d", s.Host, s.Port)
	}
	if s.Password != "s3cret" {
		t.Fatalf("password not interpolated: %q", s.Password)
	}
	if s.BaseTopic != "server-status" || s.DiscoveryPrefix != "homeassistant" {
		t.Fatalf("topic defaults: %q/%q", s.BaseTopic, s.DiscoveryPrefix)
	}
}
