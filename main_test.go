package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/prog"
)

// fixtureSource violates one default file cop (Lint/OsExit), one opt-in file
// cop (Lint/WrapErrors), and one opt-in program cop
// (Lint/ConstructorCommandExec).
const fixtureSource = `package lib

import (
	"fmt"
	"os"
	"os/exec"
)

func exit() {
	os.Exit(2)
}

func wrap(err error) error {
	return fmt.Errorf("wrap: %v", err)
}

// NewCommand prepares a command during construction.
func NewCommand() *exec.Cmd {
	return exec.Command("true")
}
`

const (
	osExitOffense      = "lib.go:10 W Lint/OsExit"
	wrapErrorsOffense  = "lib.go:14 W Lint/WrapErrors"
	commandExecOffense = "lib.go:19 E Lint/ConstructorCommandExec"
	missingPath        = "missing-path"
)

var (
	defaultFileCops    = []string{"Lint/CloneCompleteness", "Lint/FmtPrint", "Lint/OsExit", "Style/EmptyFunc", "Style/ErrorNaming"}
	defaultProgramCops = []string{"Lint/ContextConnectivity"}

	ansiCode    = regexp.MustCompile("\x1b\\[[0-9;]*m")
	bannerLine  = regexp.MustCompile(`(?m)^Inspecting Go files with (\d+) cop\(s\)$`)
	offenseLine = regexp.MustCompile(`(?m)^(\S+):(\d+):\d+: ([CWE]): (\S+): `)
)

type cliResult struct {
	code           int
	stdout, stderr string
}

// copCount returns the number of cops in the run banner, or -1 without a banner.
func (r cliResult) copCount() int {
	m := bannerLine.FindStringSubmatch(r.stdout)
	if m == nil {
		return -1
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// offenses returns "<base name>:<line> <severity> <cop>" for each reported
// offense, sorted. File cops report relative paths and program cops report
// absolute paths, so only the base name is kept.
func (r cliResult) offenses() []string {
	var out []string
	for _, m := range offenseLine.FindAllStringSubmatch(r.stdout, -1) {
		out = append(out, fmt.Sprintf("%s:%s %s %s", filepath.Base(m[1]), m[2], m[3], m[4]))
	}
	slices.Sort(out)
	return out
}

func newFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":     "module example.test\n\ngo 1.26\n",
		"lib/lib.go": fixtureSource,
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

func newFixtureWithConfig(t *testing.T, content string) string {
	t.Helper()
	dir := newFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, configPath), []byte(content), 0o644))
	return dir
}

// runCLI runs the CLI in dir, the way a user runs rubocop-go from a project root.
func runCLI(t *testing.T, dir string, args ...string) cliResult {
	t.Helper()
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return cliResult{code: code, stdout: ansiCode.ReplaceAllString(stdout.String(), ""), stderr: stderr.String()}
}

func enableAll(defaultCops bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "default_cops: %t\ncops:\n", defaultCops)
	// Write the entries in reverse catalog order: selection must not depend
	// on the order of configuration keys.
	for _, e := range slices.Backward(cops.Catalog()) {
		fmt.Fprintf(&b, "  %s:\n    enabled: true\n", e.Name)
	}
	return b.String()
}

func TestRunAppliesConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   *string
		cops     int
		offenses []string
	}{
		{name: "missing file", cops: 6, offenses: []string{osExitOffense}},
		{name: "empty file", config: new(""), cops: 6, offenses: []string{osExitOffense}},
		{
			name:     "explicit defaults with severity overrides",
			config:   new("default_cops: true\ncops:\n  Lint/OsExit:\n    severity: error\n  Lint/WrapErrors:\n    severity: error\n"),
			cops:     6,
			offenses: []string{"lib.go:10 E Lint/OsExit"},
		},
		{name: "disabled default cop", config: new("cops:\n  Lint/OsExit:\n    enabled: false\n"), cops: 5},
		{
			name:     "opt-in file and program cops",
			config:   new("cops:\n  Lint/WrapErrors:\n    enabled: true\n  Lint/ConstructorCommandExec:\n    enabled: true\n"),
			cops:     8,
			offenses: []string{osExitOffense, wrapErrorsOffense, commandExecOffense},
		},
		{
			name: "allowlist with severity overrides",
			config: new(`default_cops: false
cops:
  Lint/WrapErrors:
    enabled: true
    severity: error
  Lint/ConstructorCommandExec:
    enabled: true
    severity: convention
  Lint/OsExit:
    severity: error
  Style/EmptyFunc:
    enabled: false
`),
			cops:     2,
			offenses: []string{"lib.go:14 E Lint/WrapErrors", "lib.go:19 C Lint/ConstructorCommandExec"},
		},
		{name: "allowlist without enabled cops", config: new("default_cops: false\n"), cops: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newFixture(t)
			if tc.config != nil {
				dir = newFixtureWithConfig(t, *tc.config)
			}

			res := runCLI(t, dir)

			assert.Empty(t, res.stderr)
			assert.Equal(t, tc.cops, res.copCount(), res.stdout)
			assert.Equal(t, tc.offenses, res.offenses(), res.stdout)
			if len(tc.offenses) == 0 {
				assert.Equal(t, 0, res.code)
				assert.Contains(t, res.stdout, "1 file(s) inspected, no offenses detected")
			} else {
				assert.Equal(t, 1, res.code)
			}
		})
	}
}

func TestRunSelectsEveryShippedCop(t *testing.T) {
	for _, defaultCops := range []bool{true, false} {
		t.Run(strconv.FormatBool(defaultCops), func(t *testing.T) {
			res := runCLI(t, newFixtureWithConfig(t, enableAll(defaultCops)))

			assert.Empty(t, res.stderr)
			assert.Equal(t, 1, res.code)
			assert.Equal(t, 48, res.copCount(), res.stdout)
			assert.Subset(t, res.offenses(), []string{osExitOffense, wrapErrorsOffense, commandExecOffense}, res.stdout)
		})
	}
}

func TestRunRejectsUnknownCopNamesBeforeScanning(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{
			name:   "disabled unknown name",
			config: "default_cops: false\ncops:\n  Custom/Rule:\n    enabled: false\n",
			want:   `Error: unknown cop names in .rubocop-go.yml: "Custom/Rule"` + "\n",
		},
		{
			name: "several unknown names",
			config: `cops:
  Lint/Nope:
    enabled: true
  lint/osexit:
    enabled: false
  Lint/OsExit:
    enabled: true
  Lint/Bogus:
    severity: error
  Lint/Empty:
`,
			want: `Error: unknown cop names in .rubocop-go.yml: "Lint/Bogus", "Lint/Empty", "Lint/Nope", "lint/osexit"` + "\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newFixtureWithConfig(t, tc.config)
			// Each run reloads the configuration into a new map.
			for range 10 {
				// A missing path proves that validation runs before file collection.
				res := runCLI(t, dir, missingPath)
				assert.Equal(t, 1, res.code)
				assert.Empty(t, res.stdout)
				assert.Equal(t, tc.want, res.stderr)
			}
		})
	}
}

func TestRunRejectsInvalidConfigBeforeScanning(t *testing.T) {
	for _, content := range []string{
		"cops: [\n",
		"default_cops: maybe\n",
		"cops:\n  Lint/OsExit:\n    enabled: sometimes\n",
	} {
		t.Run(content, func(t *testing.T) {
			res := runCLI(t, newFixtureWithConfig(t, content), missingPath)

			assert.Equal(t, 1, res.code)
			assert.Empty(t, res.stdout)
			assert.True(t, strings.HasPrefix(res.stderr, "Error: parsing config: "), res.stderr)
		})
	}
}

func TestRunReportsMissingPath(t *testing.T) {
	res := runCLI(t, newFixtureWithConfig(t, "default_cops: false\n"), missingPath)

	assert.Equal(t, 1, res.code)
	assert.Empty(t, res.stdout)
	assert.True(t, strings.HasPrefix(res.stderr, "Error: stat missing-path: "), res.stderr)
}

func loadConfig(t *testing.T, content string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), configPath)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

func fileNames(list []cop.Cop) []string {
	var names []string
	for _, c := range list {
		names = append(names, c.Name())
	}
	return names
}

func programNames(list []prog.Cop) []string {
	var names []string
	for _, c := range list {
		names = append(names, c.Name())
	}
	return names
}

func TestSelectCops(t *testing.T) {
	withFile := func(names ...string) []string {
		all := append(slices.Clone(defaultFileCops), names...)
		slices.Sort(all)
		return all
	}
	for _, tc := range []struct {
		name, config    string
		files, programs []string
	}{
		{"empty file", "", defaultFileCops, defaultProgramCops},
		{"empty cops map", "cops: {}\n", defaultFileCops, defaultProgramCops},
		{"default_cops true", "default_cops: true\n", defaultFileCops, defaultProgramCops},
		{"default_cops null", "default_cops:\n", defaultFileCops, defaultProgramCops},
		{
			"disabled default file cop", "cops:\n  Lint/OsExit:\n    enabled: false\n",
			[]string{"Lint/CloneCompleteness", "Lint/FmtPrint", "Style/EmptyFunc", "Style/ErrorNaming"},
			defaultProgramCops,
		},
		{"disabled default program cop", "cops:\n  Lint/ContextConnectivity:\n    enabled: false\n", defaultFileCops, nil},
		{"enabled opt-in file cop", "cops:\n  Lint/WrapErrors:\n    enabled: true\n", withFile("Lint/WrapErrors"), defaultProgramCops},
		{
			"enabled opt-in program cop", "cops:\n  Lint/ConstructorCommandExec:\n    enabled: true\n",
			defaultFileCops,
			[]string{"Lint/ConstructorCommandExec", "Lint/ContextConnectivity"},
		},
		{"severity-only opt-in cop", "cops:\n  Lint/WrapErrors:\n    severity: error\n", defaultFileCops, defaultProgramCops},
		{"empty opt-in entry", "cops:\n  Lint/WrapErrors:\n", defaultFileCops, defaultProgramCops},
		{"disabled opt-in cop", "cops:\n  Lint/WrapErrors:\n    enabled: false\n", defaultFileCops, defaultProgramCops},
		{
			"allowlist",
			"default_cops: false\ncops:\n  Lint/NoStdoutInLibraries:\n    enabled: true\n  Lint/NoFatalOutsideMain:\n    enabled: true\n  Lint/OsExit:\n    enabled: true\n    severity: error\n",
			[]string{"Lint/NoFatalOutsideMain", "Lint/NoStdoutInLibraries", "Lint/OsExit"},
			nil,
		},
		{"allowlist with program cop", "default_cops: false\ncops:\n  Lint/ContextConnectivity:\n    enabled: true\n", nil, defaultProgramCops},
		{"allowlist without entries", "default_cops: false\n", nil, nil},
		{"allowlist with empty cops map", "default_cops: false\ncops: {}\n", nil, nil},
		{"allowlist with severity only", "default_cops: false\ncops:\n  Lint/OsExit:\n    severity: error\n", nil, nil},
		{"allowlist with explicit disable", "default_cops: false\ncops:\n  Lint/OsExit:\n    enabled: false\n", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, programs, err := selectCops(loadConfig(t, tc.config))
			require.NoError(t, err)
			assert.Equal(t, tc.files, fileNames(files))
			assert.Equal(t, tc.programs, programNames(programs))
		})
	}

	t.Run("missing file", func(t *testing.T) {
		cfg, err := config.Load(filepath.Join(t.TempDir(), configPath))
		require.NoError(t, err)
		files, programs, err := selectCops(cfg)
		require.NoError(t, err)
		assert.Equal(t, defaultFileCops, fileNames(files))
		assert.Equal(t, defaultProgramCops, programNames(programs))
	})

	t.Run("zero-value config", func(t *testing.T) {
		files, programs, err := selectCops(&config.Config{})
		require.NoError(t, err)
		assert.Equal(t, defaultFileCops, fileNames(files))
		assert.Equal(t, defaultProgramCops, programNames(programs))
	})
}

func TestSelectCopsUsesCatalogOrder(t *testing.T) {
	var wantFiles, wantPrograms []string
	for _, e := range cops.Catalog() {
		if e.File != nil {
			wantFiles = append(wantFiles, e.Name)
		} else {
			wantPrograms = append(wantPrograms, e.Name)
		}
	}
	require.Len(t, wantFiles, 17)
	require.Len(t, wantPrograms, 31)

	cfg := loadConfig(t, enableAll(false))
	var previous []cop.Cop
	for range 20 {
		files, programs, err := selectCops(cfg)
		require.NoError(t, err)
		assert.Equal(t, wantFiles, fileNames(files))
		assert.Equal(t, wantPrograms, programNames(programs))
		if previous != nil {
			assert.NotSame(t, previous[0], files[0], "selection must construct fresh instances")
		}
		previous = files
	}
}

func TestSelectCopsRejectsUnknownNamesDeterministically(t *testing.T) {
	cfg := loadConfig(t, "cops:\n  Z/Last:\n    enabled: false\n  A/First:\n  Lint/OsExit:\n    enabled: true\n  \"Lint/Odd Name\":\n    severity: error\n")
	for range 20 {
		files, programs, err := selectCops(cfg)
		require.EqualError(t, err, `unknown cop names in .rubocop-go.yml: "A/First", "Lint/Odd Name", "Z/Last"`)
		assert.Nil(t, files)
		assert.Nil(t, programs)
	}
}
