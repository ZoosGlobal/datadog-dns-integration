// config/config.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all runtime configuration for the DNS monitor.
// Loaded from config.yaml, overridable via environment variables.
type Config struct {
	// DogStatsD transport
	StatsDHost string `yaml:"statsd_host"` // default: 127.0.0.1
	StatsDPort int    `yaml:"statsd_port"` // default: 8125

	// Tagging
	Env        string   `yaml:"env"`         // e.g. production
	GlobalTags []string `yaml:"global_tags"` // extra tags applied to every metric

	// Collection
	ResolutionProbeWarnMs  float64 `yaml:"resolution_warn_ms"`  // default: 100
	ResolutionProbeCritMs  float64 `yaml:"resolution_crit_ms"`  // default: 500
	ResolutionProbeDomain  string  `yaml:"resolution_probe_domain"` // default: www.google.com

	// Forwarder probing
	ForwarderIPs        []string `yaml:"forwarder_ips"`
	ForwarderProbeDomain string  `yaml:"forwarder_probe_domain"` // default: example.com
	ForwarderTimeoutSec  int     `yaml:"forwarder_timeout_sec"`  // default: 5

	// Discovery
	DiscoveryTTLMinutes int `yaml:"discovery_ttl_minutes"` // default: 30
}

// DefaultConfig returns safe production defaults.
func DefaultConfig() *Config {
	return &Config{
		StatsDHost:            "127.0.0.1",
		StatsDPort:            8125,
		Env:                   "production",
		ResolutionProbeWarnMs: 100,
		ResolutionProbeCritMs: 500,
		ResolutionProbeDomain: "www.google.com",
		ForwarderProbeDomain:  "example.com",
		ForwarderTimeoutSec:   5,
		DiscoveryTTLMinutes:   30,
	}
}

// Load reads config from the YAML file at configPath.
// Missing fields fall back to DefaultConfig values.
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	if configPath == "" {
		// Look for config.yaml next to the binary
		exe, err := os.Executable()
		if err == nil {
			configPath = filepath.Join(filepath.Dir(exe), "config.yaml")
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file — use defaults (acceptable for first run)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file %s: %w", configPath, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", configPath, err)
	}

	return cfg, nil
}
