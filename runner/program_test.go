package runner_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

// TestRunner_ProgramCop_EndToEnd loads a real module from disk, runs the
// whole-program context-connectivity cop through the runner, and checks
// that a detached context is reported.
func TestRunner_ProgramCop_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"go.mod": "module example.test\n\ngo 1.22\n",
		"main.go": `package main

import "context"

func use(ctx context.Context) { _ = ctx }

func detached() {
	use(context.Background())
}

func main() {
	use(context.Background())
	detached()
}
`,
	})

	count := runProgramCops(t, dir)
	assert.Equal(t, 1, count, "expected one detached-context offense")
}

// TestRunner_ProgramCop_Suppression checks that a //rubocop:disable comment
// on the detached context's line silences the whole-program offense.
func TestRunner_ProgramCop_Suppression(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"go.mod": "module example.test\n\ngo 1.22\n",
		"main.go": `package main

import "context"

func use(ctx context.Context) { _ = ctx }

func detached() {
	use(context.Background()) //rubocop:disable Lint/ContextConnectivity
}

func main() {
	use(context.Background())
	detached()
}
`,
	})

	count := runProgramCops(t, dir)
	assert.Equal(t, 0, count, "suppressed offense must not be reported")
}

func runProgramCops(t *testing.T, dir string) int {
	t.Helper()
	t.Chdir(dir)

	var buf bytes.Buffer
	r := runner.New(nil, config.DefaultConfig(), &buf).
		WithProgramCops(cops.AllProgram())
	count, err := r.Run([]string{"."})
	require.NoError(t, err)
	return count
}

func writeModule(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}
