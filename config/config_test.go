package config_test

import (
	"os"
	"path/filepath"
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
