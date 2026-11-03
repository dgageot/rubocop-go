package runner_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
)

func newPackageCop() cop.Cop {
	return cop.New(cop.Meta{Name: "Custom/Package", Description: "Reports every package clause.", Severity: cop.Convention},
		func(p *cop.Pass) { p.Report(p.File.Name, "package clause") })
}

func newCustomProgramCop() prog.Cop {
	return prog.New(cop.Meta{Name: "Custom/Program", Description: "Reports nothing."}, func(*prog.Pass) {})
}

func loadRunnerConfig(t *testing.T, content string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".rubocop-go.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

// TestRunner_CustomCopNamesFollowConfig checks that embedded runners accept
// cop names outside the shipped catalog. Generic configuration and runner
// construction do not validate names.
func TestRunner_CustomCopNamesFollowConfig(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nimport \"os\"\n\nfunc f() { os.Exit(1) }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644))

	for _, tc := range []struct {
		name     string
		cfg      *config.Config
		cops     []string
		severity []string
	}{
		{"zero-value config", &config.Config{}, []string{"Custom/Package", "Lint/OsExit"}, []string{"convention", "warning"}},
		{"default config", config.DefaultConfig(), []string{"Custom/Package", "Lint/OsExit"}, []string{"convention", "warning"}},
		{
			"custom severity and disablement",
			loadRunnerConfig(t, "cops:\n  Custom/Package:\n    severity: error\n  Lint/OsExit:\n    enabled: false\n  Custom/Unused:\n    enabled: true\n"),
			[]string{"Custom/Package"},
			[]string{"error"},
		},
		{
			"default_cops false keeps explicit enables",
			loadRunnerConfig(t, "default_cops: false\ncops:\n  Custom/Package:\n    enabled: true\n"),
			[]string{"Custom/Package"},
			[]string{"convention"},
		},
		{"default_cops false without enables", loadRunnerConfig(t, "default_cops: false\n"), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			r := runner.New([]cop.Cop{newPackageCop(), cops.NewLintOsExit()}, tc.cfg, &out)
			r.Reporter = runner.NewJSONReporter(&out)
			count, err := r.Run([]string{dir})
			require.NoError(t, err)

			var result struct {
				Offenses []struct{ Cop, Severity string }
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			var names, severities []string
			for _, o := range result.Offenses {
				names = append(names, o.Cop)
				severities = append(severities, o.Severity)
			}
			assert.Len(t, tc.cops, count)
			assert.Equal(t, tc.cops, names)
			assert.Equal(t, tc.severity, severities)
		})
	}
}

func TestRunner_CustomProgramCopNamesFollowConfig(t *testing.T) {
	enabled := runner.New(nil, &config.Config{}, io.Discard).WithProgramCops([]prog.Cop{newCustomProgramCop()})
	assert.Len(t, enabled.ProgramCops, 1)

	allowlisted := loadRunnerConfig(t, "default_cops: false\ncops:\n  Custom/Program:\n    enabled: true\n")
	r := runner.New(nil, allowlisted, io.Discard).WithProgramCops([]prog.Cop{newCustomProgramCop(), cops.NewLintContextConnectivity()})
	require.Len(t, r.ProgramCops, 1)
	assert.Equal(t, "Custom/Program", r.ProgramCops[0].Name())

	off := loadRunnerConfig(t, "default_cops: false\n")
	assert.Empty(t, runner.New(nil, off, io.Discard).WithProgramCops([]prog.Cop{newCustomProgramCop()}).ProgramCops)
}
