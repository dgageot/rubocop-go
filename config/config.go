// Package config handles loading .rubocop-go.yml configuration.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dgageot/rubocop-go/cop"
)

// Config represents the top-level configuration.
type Config struct {
	Cops map[string]CopConfig `yaml:"cops"`
}

// CopConfig represents per-cop configuration.
type CopConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	Severity string `yaml:"severity,omitempty"`
}

// DefaultConfig returns a config with everything enabled.
func DefaultConfig() *Config {
	return &Config{
		Cops: map[string]CopConfig{},
	}
}

// Load reads a .rubocop-go.yml file from the given path.
// If the file does not exist, it returns the default config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// IsEnabled returns whether a cop is enabled in this config.
// Cops are enabled by default unless explicitly disabled.
func (c *Config) IsEnabled(copName string) bool {
	cc, ok := c.Cops[copName]
	if !ok {
		return true
	}
	if cc.Enabled == nil {
		return true
	}
	return *cc.Enabled
}

// SeverityFor returns the severity to use for the named cop and a flag
// indicating whether the user provided an explicit override. When the second
// return is false, callers should fall back to the cop's default.
//
// Recognised values: "convention", "warning", "error". Unknown values are
// silently ignored (treated as no override).
func (c *Config) SeverityFor(copName string) (cop.Severity, bool) {
	cc, ok := c.Cops[copName]
	if !ok {
		return 0, false
	}
	switch strings.ToLower(cc.Severity) {
	case "convention":
		return cop.Convention, true
	case "warning":
		return cop.Warning, true
	case "error":
		return cop.Error, true
	}
	return 0, false
}
