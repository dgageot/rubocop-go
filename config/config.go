// Package config handles loading .rubocop-go.yml configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dgageot/rubocop-go/cop"
)

// Config represents the top-level configuration.
//
// Only [Load] reads the exclude lists of the cops entries. Config keeps them
// in an unexported field, and [Config.Exclusions] matches them. A Config that
// Load did not return, such as [DefaultConfig] or a struct literal, has no
// exclude patterns. A YAML decoder that fills a Config directly ignores the
// exclude lists, and a YAML encoder does not write them. A copy of a loaded
// Config keeps its patterns. Changes to Cops do not add or remove patterns.
type Config struct {
	// DefaultCops controls cops that have no explicit enabled value. Nil (the
	// key is omitted) behaves like true: each cop keeps its default status.
	// False disables every cop that is not explicitly enabled, which turns
	// Cops into an allowlist.
	DefaultCops *bool `yaml:"default_cops,omitempty"`

	Cops map[string]CopConfig `yaml:"cops"`

	// dir is the absolute directory of the file that Load read. Exclude
	// patterns are relative to it. It is empty for other configurations.
	dir string

	// exclude holds the validated exclude patterns that Load read, by cop
	// name. It has only entries with patterns, and it is nil without them.
	exclude map[string][]pattern
}

// CopConfig represents per-cop configuration.
type CopConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	Severity string `yaml:"severity,omitempty"`
}

// excludeFile is the part of a configuration file that Config does not
// decode: the exclude list of each cops entry.
type excludeFile struct {
	Cops map[string]struct {
		Exclude []string `yaml:"exclude"`
	} `yaml:"cops"`
}

// DefaultConfig returns a config with everything enabled.
func DefaultConfig() *Config {
	return &Config{
		Cops: map[string]CopConfig{},
	}
}

// Load reads a .rubocop-go.yml file from the given path.
// If the file does not exist, it returns the default config.
//
// Load also reads the exclude list of each cops entry. See
// [Config.Exclusions] for the pattern syntax. Load records the absolute
// directory of path as the root of the patterns, so a later change of the
// working directory does not move it. It returns an error for an invalid
// exclude list or pattern, also in an entry for an unknown or disabled cop.
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
	// Config has no exclude field, so decode the exclude lists separately.
	var file excludeFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("resolving config directory: %w", err)
	}
	cfg.dir = dir

	if cfg.exclude, err = file.compile(); err != nil {
		return nil, err
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
