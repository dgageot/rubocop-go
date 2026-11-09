package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
)

func TestLoad_DefaultWhenMissing(t *testing.T) {
	cfg, err := config.Load("nonexistent.yml")
	require.NoError(t, err)

	assert.True(t, cfg.IsEnabled("Lint/OsExit"))
	assert.True(t, cfg.IsEnabled("Style/ErrorNaming"))
}

func TestLoad_DisabledCop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".rubocop-go.yml")
	err := os.WriteFile(path, []byte(`cops:
  Lint/OsExit:
    enabled: false
`), 0o644)
	require.NoError(t, err)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.False(t, cfg.IsEnabled("Lint/OsExit"))
	assert.True(t, cfg.IsEnabled("Style/ErrorNaming"))
}

func TestSeverityFor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".rubocop-go.yml")
	err := os.WriteFile(path, []byte(`cops:
  Lint/OsExit:
    severity: error
  Style/ErrorNaming:
    severity: nonsense
`), 0o644)
	require.NoError(t, err)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	sev, ok := cfg.SeverityFor("Lint/OsExit")
	require.True(t, ok)
	assert.Equal(t, cop.Error, sev)

	_, ok = cfg.SeverityFor("Style/ErrorNaming")
	assert.False(t, ok, "unknown severity strings must not produce an override")

	_, ok = cfg.SeverityFor("Lint/Unconfigured")
	assert.False(t, ok, "cops with no entry must not produce an override")
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".rubocop-go.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestLoad_DefaultCops(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    *bool
	}{
		{"omitted", "cops: {}\n", nil},
		{"null", "default_cops:\n", nil},
		{"true", "default_cops: true\n", new(true)},
		{"false", "default_cops: false\n", new(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Load(writeConfig(t, tc.content))
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.DefaultCops)
		})
	}

	cfg, err := config.Load("nonexistent.yml")
	require.NoError(t, err)
	assert.Nil(t, cfg.DefaultCops, "a missing file must keep the default status of every cop")
}

func TestLoad_InvalidValues(t *testing.T) {
	for _, content := range []string{
		"cops: [\n",
		"default_cops: maybe\n",
		"default_cops: [true]\n",
		"cops:\n  Lint/OsExit:\n    enabled: sometimes\n",
		"cops:\n  - Lint/OsExit\n",
		"cops:\n  Lint/OsExit:\n    exclude: gen/\n",
		"cops:\n  Lint/OsExit:\n    exclude: true\n",
		"cops:\n  Lint/OsExit:\n    exclude: {gen: true}\n",
		"cops:\n  Custom/Rule:\n    exclude: [[gen/]]\n",
	} {
		t.Run(content, func(t *testing.T) {
			cfg, err := config.Load(writeConfig(t, content))
			require.ErrorContains(t, err, "parsing config: ")
			assert.Nil(t, cfg)
		})
	}
}

func TestIsEnabledWithDefault(t *testing.T) {
	on, off := new(true), new(false)
	for _, tc := range []struct {
		name             string
		cfg              *config.Config
		enabledByDefault bool
		want             bool
	}{
		{"zero config keeps default on", &config.Config{}, true, true},
		{"zero config keeps default off", &config.Config{}, false, false},
		{"empty map keeps default on", config.DefaultConfig(), true, true},
		{"empty map keeps default off", config.DefaultConfig(), false, false},
		{"default_cops true keeps default on", &config.Config{DefaultCops: on}, true, true},
		{"default_cops true keeps default off", &config.Config{DefaultCops: on}, false, false},
		{"default_cops false disables default on", &config.Config{DefaultCops: off}, true, false},
		{"default_cops false keeps default off", &config.Config{DefaultCops: off}, false, false},
		{"explicit false disables default on", &config.Config{Cops: map[string]config.CopConfig{"X": {Enabled: off}}}, true, false},
		{"explicit true enables default off", &config.Config{Cops: map[string]config.CopConfig{"X": {Enabled: on}}}, false, true},
		{"severity only keeps default on", &config.Config{Cops: map[string]config.CopConfig{"X": {Severity: "error"}}}, true, true},
		{"severity only keeps default off", &config.Config{Cops: map[string]config.CopConfig{"X": {Severity: "error"}}}, false, false},
		{"explicit true wins over default_cops false", &config.Config{DefaultCops: off, Cops: map[string]config.CopConfig{"X": {Enabled: on}}}, false, true},
		{"explicit false wins over default_cops true", &config.Config{DefaultCops: on, Cops: map[string]config.CopConfig{"X": {Enabled: off}}}, true, false},
		{"severity only under default_cops false", &config.Config{DefaultCops: off, Cops: map[string]config.CopConfig{"X": {Severity: "error"}}}, true, false},
		{"other entries do not apply", &config.Config{Cops: map[string]config.CopConfig{"Y": {Enabled: off}}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cfg.IsEnabledWithDefault("X", tc.enabledByDefault))
			assert.Equal(t, tc.cfg.IsEnabledWithDefault("X", true), tc.cfg.IsEnabled("X"),
				"IsEnabled must treat every cop as enabled by default")
		})
	}
}

func TestIsEnabled_CustomNames(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, `default_cops: false
cops:
  Custom/Enabled:
    enabled: true
  Custom/SeverityOnly:
    severity: error
`))
	require.NoError(t, err)

	assert.True(t, cfg.IsEnabled("Custom/Enabled"))
	assert.False(t, cfg.IsEnabled("Custom/SeverityOnly"))
	assert.False(t, cfg.IsEnabled("Custom/Unconfigured"))
	sev, ok := cfg.SeverityFor("Custom/SeverityOnly")
	require.True(t, ok)
	assert.Equal(t, cop.Error, sev)

	assert.True(t, (&config.Config{}).IsEnabled("Custom/Unconfigured"), "zero-value config must enable every cop")
}

// TestCopConfig_Compatibility uses CopConfig from outside the package as code
// written for earlier versions can.
func TestCopConfig_Compatibility(t *testing.T) {
	on := new(true)
	positional := config.CopConfig{on, "error"}
	keyed := config.CopConfig{Enabled: on, Severity: "error"}
	if positional != keyed {
		t.Errorf("positional literal %+v differs from keyed literal %+v", positional, keyed)
	}
	assert.Equal(t, 1, map[config.CopConfig]int{positional: 1}[keyed], "CopConfig values must stay usable as map keys")

	typ := reflect.TypeFor[config.CopConfig]()
	require.Equal(t, 2, typ.NumField())
	for i, want := range []struct{ name, tag string }{
		{"Enabled", `yaml:"enabled"`},
		{"Severity", `yaml:"severity,omitempty"`},
	} {
		assert.Equal(t, want.name, typ.Field(i).Name)
		assert.Equal(t, want.tag, string(typ.Field(i).Tag))
	}
}

// TestLoad_ExcludeKeepsDecodedFields checks that an exclude list changes no
// exported field, and that null and empty values decode as before.
func TestLoad_ExcludeKeepsDecodedFields(t *testing.T) {
	off := new(false)
	for _, tc := range []struct {
		name, content string
		defaultCops   *bool
		cops          map[string]config.CopConfig
	}{
		{"empty document", "", nil, map[string]config.CopConfig{}},
		{"null document", "~\n", nil, map[string]config.CopConfig{}},
		{"null cops", "cops:\n", nil, nil},
		{"empty cops", "cops: {}\n", nil, map[string]config.CopConfig{}},
		{"null entry", "cops:\n  X:\n", nil, map[string]config.CopConfig{"X": {}}},
		{"exclusion-only entry", "cops:\n  X:\n    exclude: [gen/]\n", nil, map[string]config.CopConfig{"X": {}}},
		{
			"every key",
			"default_cops: false\ncops:\n  X:\n    enabled: false\n    severity: error\n    exclude: [gen/]\n  Y:\n    exclude: []\n",
			off,
			map[string]config.CopConfig{"X": {Enabled: off, Severity: "error"}, "Y": {}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Load(writeConfig(t, tc.content))
			require.NoError(t, err)
			assert.Equal(t, tc.defaultCops, cfg.DefaultCops)
			assert.Equal(t, tc.cops, cfg.Cops)
		})
	}
}
