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
	// DefaultCops controls cops that have no explicit enabled value. Nil (the
	// key is omitted) behaves like true: each cop keeps its default status.
	// False disables every cop that is not explicitly enabled, which turns
	// Cops into an allowlist.
	DefaultCops *bool `yaml:"default_cops,omitempty"`

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
// Cops are enabled by default unless explicitly disabled, or unless
// default_cops is false and the cop is not explicitly enabled.
func (c *Config) IsEnabled(copName string) bool {
	return c.IsEnabledWithDefault(copName, true)
}

// IsEnabledWithDefault returns whether a cop is enabled in this config when
// enabledByDefault is the cop's status without configuration. The rules apply
// in this order:
//
//  1. An explicit per-cop enabled value wins.
//  2. Otherwise, default_cops: false disables the cop.
//  3. Otherwise, enabledByDefault applies.
//
// An entry without an enabled value, such as a severity-only entry, does not
// change the result.
func (c *Config) IsEnabledWithDefault(copName string, enabledByDefault bool) bool {
	if cc, ok := c.Cops[copName]; ok && cc.Enabled != nil {
		return *cc.Enabled
	}
	if c.DefaultCops != nil && !*c.DefaultCops {
		return false
	}
	return enabledByDefault
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
