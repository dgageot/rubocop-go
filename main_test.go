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

// unknownEntries configures names that are not in the catalog, as entries of a
// cops map. It covers custom names, a typo, wrong-case spellings of shipped
// cops, and enabled, disabled, severity-only, and null entries. None of these
// entries may change selection, severities, output, or the exit status.
const unknownEntries = `  Custom/Rule:
    enabled: true
  Custom/Disabled:
    enabled: false
  Lint/OsExti:
    enabled: true
  lint/osexit:
    enabled: false
  lint/wraperrors:
    enabled: true
  LINT/OSEXIT:
    severity: convention
  "Lint/Odd Name":
    severity: error
  Lint/Empty:
`

var unknownNames = []string{
	"Custom/Rule", "Custom/Disabled", "Lint/OsExti", "lint/osexit",
	"lint/wraperrors", "LINT/OSEXIT", "Lint/Odd Name", "Lint/Empty",
}

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

// TestRunIgnoresUnknownCopNames checks that names outside the catalog have no
// effect, as before catalog selection: the CLI scans the files, runs only the
// known selected cops, and does not mention the unknown names.
func TestRunIgnoresUnknownCopNames(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		cops         int
		offenses     []string
	}{
		{name: "defaults", config: "cops:\n" + unknownEntries, cops: 6, offenses: []string{osExitOffense}},
		{name: "explicit defaults", config: "default_cops: true\ncops:\n" + unknownEntries, cops: 6, offenses: []string{osExitOffense}},
		{
			name:     "opt-in file and program cops",
			config:   "cops:\n" + unknownEntries + "  Lint/WrapErrors:\n    enabled: true\n  Lint/ConstructorCommandExec:\n    enabled: true\n",
			cops:     8,
			offenses: []string{osExitOffense, wrapErrorsOffense, commandExecOffense},
		},
		{
			name: "allowlist with severity override",
			config: "default_cops: false\ncops:\n" + unknownEntries +
				"  Lint/WrapErrors:\n    enabled: true\n    severity: error\n  Lint/ConstructorCommandExec:\n    enabled: true\n",
			cops:     2,
			offenses: []string{"lib.go:14 E Lint/WrapErrors", commandExecOffense},
		},
		{name: "allowlist with only unknown names", config: "default_cops: false\ncops:\n" + unknownEntries, cops: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t, newFixtureWithConfig(t, tc.config))

			assert.Empty(t, res.stderr)
			assert.Equal(t, tc.cops, res.copCount(), res.stdout)
			assert.Equal(t, tc.offenses, res.offenses(), res.stdout)
			for _, name := range unknownNames {
				assert.NotContains(t, res.stdout, name)
			}
			if len(tc.offenses) == 0 {
				assert.Equal(t, 0, res.code)
				assert.Contains(t, res.stdout, "1 file(s) inspected, no offenses detected")
			} else {
				assert.Equal(t, 1, res.code)
				assert.Contains(t, res.stdout, "1 file(s) inspected")
			}
		})
	}
}

func TestRunRejectsInvalidConfigBeforeScanning(t *testing.T) {
	for _, content := range []string{
		"cops: [\n",
		"default_cops: maybe\n",
		"cops:\n  Lint/OsExit:\n    enabled: sometimes\n",
		// Entries with unknown names are still decoded, so their values must
		// have the right types.
		"cops:\n  Custom/Rule:\n    enabled: sometimes\n",
		"cops:\n  Lint/Typo: 5\n",
		"cops:\n  lint/osexit:\n    severity: [error]\n",
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
	for _, tc := range []struct{ name, config string }{
		{"allowlist", "default_cops: false\n"},
		// Unknown names do not stop the CLI before it collects files.
		{"unknown names", "cops:\n" + unknownEntries},
		{"allowlist with only unknown names", "default_cops: false\ncops:\n" + unknownEntries},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t, newFixtureWithConfig(t, tc.config), missingPath)

			assert.Equal(t, 1, res.code)
			assert.Empty(t, res.stdout)
			assert.True(t, strings.HasPrefix(res.stderr, "Error: stat missing-path: "), res.stderr)
		})
	}
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
		{"unknown names only", "cops:\n" + unknownEntries, defaultFileCops, defaultProgramCops},
		{
			"known and unknown names",
			"cops:\n" + unknownEntries + "  Lint/OsExit:\n    enabled: false\n  Lint/WrapErrors:\n    enabled: true\n  Lint/ConstructorCommandExec:\n    enabled: true\n",
			[]string{"Lint/CloneCompleteness", "Lint/FmtPrint", "Lint/WrapErrors", "Style/EmptyFunc", "Style/ErrorNaming"},
			[]string{"Lint/ConstructorCommandExec", "Lint/ContextConnectivity"},
		},
		{"allowlist with only unknown names", "default_cops: false\ncops:\n" + unknownEntries, nil, nil},
		{
			"allowlist with known and unknown names",
			"default_cops: false\ncops:\n" + unknownEntries + "  Lint/NoFatalOutsideMain:\n    enabled: true\n  Lint/ContextConnectivity:\n    enabled: true\n",
			[]string{"Lint/NoFatalOutsideMain"},
			defaultProgramCops,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, programs := selectCops(loadConfig(t, tc.config))
			assert.Equal(t, tc.files, fileNames(files))
			assert.Equal(t, tc.programs, programNames(programs))
		})
	}

	t.Run("missing file", func(t *testing.T) {
		cfg, err := config.Load(filepath.Join(t.TempDir(), configPath))
		require.NoError(t, err)
		files, programs := selectCops(cfg)
		assert.Equal(t, defaultFileCops, fileNames(files))
		assert.Equal(t, defaultProgramCops, programNames(programs))
	})

	t.Run("zero-value config", func(t *testing.T) {
		files, programs := selectCops(&config.Config{})
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

	cfg := loadConfig(t, enableAll(false)+unknownEntries)
	var previous []cop.Cop
	for range 20 {
		files, programs := selectCops(cfg)
		assert.Equal(t, wantFiles, fileNames(files))
		assert.Equal(t, wantPrograms, programNames(programs))
		if previous != nil {
			assert.NotSame(t, previous[0], files[0], "selection must construct fresh instances")
		}
		previous = files
	}
}

// TestSelectCopsIgnoresUnknownNamesDeterministically loads the configuration
// into a new map in each iteration, so the map iteration order changes. The
// selection must stay exact and in catalog order, with fresh instances.
func TestSelectCopsIgnoresUnknownNamesDeterministically(t *testing.T) {
	known := "  Lint/WrapErrors:\n    enabled: true\n  Lint/ConstructorCommandExec:\n    enabled: true\n"
	for _, tc := range []struct {
		name, config    string
		files, programs []string
	}{
		{
			"defaults", "cops:\n" + unknownEntries + known,
			[]string{"Lint/CloneCompleteness", "Lint/FmtPrint", "Lint/OsExit", "Lint/WrapErrors", "Style/EmptyFunc", "Style/ErrorNaming"},
			[]string{"Lint/ConstructorCommandExec", "Lint/ContextConnectivity"},
		},
		{
			"allowlist", "default_cops: false\ncops:\n" + known + unknownEntries,
			[]string{"Lint/WrapErrors"},
			[]string{"Lint/ConstructorCommandExec"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				previousFile    cop.Cop
				previousProgram prog.Cop
			)
			for range 20 {
				files, programs := selectCops(loadConfig(t, tc.config))
				require.Equal(t, tc.files, fileNames(files))
				require.Equal(t, tc.programs, programNames(programs))
				if previousFile != nil {
					assert.NotSame(t, previousFile, files[0], "selection must construct fresh file cops")
					assert.NotSame(t, previousProgram, programs[0], "selection must construct fresh program cops")
				}
				previousFile, previousProgram = files[0], programs[0]
			}
		})
	}
}
