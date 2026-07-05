// Package config loads and defaults the agent configuration.
package config

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// SinkConfig configures one output transport. Phase 1 uses only type "mqtt".
type SinkConfig struct {
	Type            string `yaml:"type"`
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	BaseTopic       string `yaml:"base_topic"`
	DiscoveryPrefix string `yaml:"discovery_prefix"`
	Retain          bool   `yaml:"retain"`
	QoS             int    `yaml:"qos"`
}

// Config is the whole agent configuration.
type Config struct {
	Node         string            `yaml:"node"`
	FriendlyName string            `yaml:"friendly_name"`
	Parent       string            `yaml:"parent"`
	Hierarchy    string            `yaml:"hierarchy"`
	Sinks        []SinkConfig      `yaml:"sinks"`
	Disks        map[string]string `yaml:"disks"`
}

// DiskName returns the configured friendly alias for a disk serial/WWN, or fallback.
func (c Config) DiskName(serial, fallback string) string {
	if alias, ok := c.Disks[serial]; ok && alias != "" {
		return alias
	}
	return fallback
}

var envRefs = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces ${VAR} references with their environment values.
// A bare $ that is not part of ${...} is left untouched.
func ExpandEnv(raw []byte) []byte {
	return envRefs.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := envRefs.FindSubmatch(m)[1]
		return []byte(os.Getenv(string(name)))
	})
}

// Load reads a YAML config file, interpolates ${ENV}, and applies defaults.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(ExpandEnv(raw), &cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Hierarchy == "" {
		c.Hierarchy = "grouped"
	}
	for i := range c.Sinks {
		s := &c.Sinks[i]
		if s.Port == 0 {
			s.Port = 1883
		}
		if s.BaseTopic == "" {
			s.BaseTopic = "server-status"
		}
		if s.DiscoveryPrefix == "" {
			s.DiscoveryPrefix = "homeassistant"
		}
	}
}
