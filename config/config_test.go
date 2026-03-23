package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dgageot/rubocop-go/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
