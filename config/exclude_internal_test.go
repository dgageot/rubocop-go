package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad_StoresOnlyPatterns checks the unexported state that Load returns.
func TestLoad_StoresOnlyPatterns(t *testing.T) {
	load := func(content string) (*Config, string) {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, ".rubocop-go.yml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		cfg, err := Load(path)
		require.NoError(t, err)
		return cfg, dir
	}

	for _, content := range []string{
		"",
		"cops:\n",
		"cops:\n  X:\n",
		"cops:\n  X:\n    exclude:\n",
		"cops:\n  X:\n    exclude: []\n",
		"cops:\n  X:\n    exclude: [~]\n",
	} {
		cfg, dir := load(content)
		assert.Nil(t, cfg.exclude, "no patterns must mean no map: %q", content)
		assert.Equal(t, dir, cfg.dir, content)
	}

	cfg, _ := load(`cops:
  Custom/Unknown:
    exclude: [a, b/]
  Lint/OsExit:
    enabled: false
    exclude: ["**"]
  Y:
    exclude: []
`)
	assert.Equal(t, map[string][]pattern{
		"Custom/Unknown": {{parts: []string{"a"}}, {parts: []string{"b"}, dirOnly: true}},
		"Lint/OsExit":    {{parts: []string{"**"}, recursive: true}},
	}, cfg.exclude, "unknown and disabled entries keep their patterns, and empty lists add no entry")

	missing, err := Load(filepath.Join(t.TempDir(), "missing.yml"))
	require.NoError(t, err)
	assert.Empty(t, missing.dir)
	assert.Nil(t, missing.exclude)
}
