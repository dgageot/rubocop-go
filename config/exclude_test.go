package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dgageot/rubocop-go/config"
)

// excludeConfig returns a configuration file in which the cop X excludes
// patterns.
func excludeConfig(patterns ...string) string {
	var b strings.Builder
	b.WriteString("cops:\n  X:\n    exclude:\n")
	for _, p := range patterns {
		fmt.Fprintf(&b, "      - %q\n", p)
	}
	return b.String()
}

// excluding loads a configuration file in which the cop X excludes patterns.
// It returns the configuration and the directory of the file.
func excluding(t *testing.T, patterns ...string) (*config.Config, string) {
	t.Helper()
	path := writeConfig(t, excludeConfig(patterns...))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg, filepath.Dir(path)
}

// slashJoin converts a slash-separated relative path below root to an
// absolute filename for the current platform.
func slashJoin(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

func TestExclusions_Patterns(t *testing.T) {
	for _, tc := range []struct {
		pattern         string
		matches, misses []string
	}{
		{"main.go", []string{"main.go"}, []string{"cmd/main.go", "main.go.orig", "xmain.go"}},
		{"cmd/tool/main.go", []string{"cmd/tool/main.go"}, []string{"cmd/main.go", "x/cmd/tool/main.go"}},
		{"cmd", []string{"cmd/main.go", "cmd/tool/main.go"}, []string{"cmd.go", "cmdx/main.go", "x/cmd/main.go"}},
		{"cmd/", []string{"cmd/main.go", "cmd/tool/main.go"}, []string{"cmd", "cmd.go", "x/cmd/main.go"}},
		// A glob that matches a directory, such as pkg.go, also matches the files below it.
		{"*.go", []string{"a.go", "main.go", "pkg.go/child.go", "pkg.go/x/b.go"}, []string{"pkg/a.go", "a.go.orig"}},
		{"*_test.go", []string{"a_test.go"}, []string{"pkg/a_test.go", "a.go"}},
		{"**/*_test.go", []string{"a_test.go", "pkg/a_test.go", "pkg/x/y/a_test.go"}, []string{"a.go", "pkg/a.go", "pkg/a_test.go.orig"}},
		{"pkg/**/gen.go", []string{"pkg/gen.go", "pkg/a/gen.go", "pkg/a/b/c/gen.go"}, []string{"gen.go", "x/pkg/gen.go", "pkg/gen.go.orig", "pkgx/gen.go"}},
		{"pkg/**", []string{"pkg/a.go", "pkg/a/b.go"}, []string{"pkg.go", "pkgx/a.go", "a/pkg/b.go"}},
		{"**", []string{"a.go", "x/y/z.go"}, nil},
		{"**/", []string{"x/a.go", "x/y/z.go"}, []string{"a.go"}},
		{"**/testdata/", []string{"testdata/a.go", "x/testdata/a.go", "x/testdata/y/a.go"}, []string{"testdata", "x/testdata", "x/testdatax/a.go"}},
		{"**/**/gen", []string{"gen/a.go", "x/gen/a.go", "x/y/gen/a.go"}, []string{"gen.go", "x/gena/a.go"}},
		{"internal/*/gen", []string{"internal/a/gen/x.go"}, []string{"internal/gen/x.go", "internal/a/b/gen/x.go"}},
		{"?.go", []string{"a.go"}, []string{"ab.go", ".go"}},
		{"[ab].go", []string{"a.go", "b.go"}, []string{"c.go"}},
		{"[^a].go", []string{"b.go"}, []string{"a.go"}},
		{"gen[*].go", []string{"gen*.go"}, []string{"gena.go"}},
		{"[**]x.go", []string{"*x.go"}, []string{"ax.go"}},
		{".hidden/", []string{".hidden/a.go"}, []string{"hidden/a.go"}},
		{"...", []string{".../a.go"}, []string{"a.go"}},
		{"a b/c.go", []string{"a b/c.go"}, []string{"a/c.go"}},
		// Normalization.
		{"./cmd", []string{"cmd/main.go"}, []string{"main.go"}},
		{"cmd//tool/./", []string{"cmd/tool/main.go"}, []string{"cmd/main.go", "cmd/tool"}},
		{"./**/./gen.go", []string{"gen.go", "a/gen.go"}, []string{"a/gen.go.orig"}},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			cfg, root := excluding(t, tc.pattern)
			e, err := cfg.Exclusions(root)
			require.NoError(t, err)
			for _, rel := range tc.matches {
				assert.True(t, e.Excludes("X", rel), "relative %s", rel)
				assert.True(t, e.Excludes("X", slashJoin(root, rel)), "absolute %s", rel)
				assert.False(t, e.Excludes("Y", rel), "another cop must not be affected: %s", rel)
			}
			for _, rel := range tc.misses {
				assert.False(t, e.Excludes("X", rel), "relative %s", rel)
				assert.False(t, e.Excludes("X", slashJoin(root, rel)), "absolute %s", rel)
			}
		})
	}
}

func TestExclusions_Filenames(t *testing.T) {
	cfg, root := excluding(t, "**")
	e, err := cfg.Exclusions(root)
	require.NoError(t, err)

	assert.True(t, e.Excludes("X", "a.go"))
	assert.True(t, e.Excludes("X", "./a.go"))
	sep := string(filepath.Separator)
	assert.True(t, e.Excludes("X", "pkg"+sep+".."+sep+"a.go"), "filenames are cleaned lexically")
	assert.True(t, e.Excludes("X", "pkg"+sep+sep+"a.go"), "filenames are cleaned lexically")
	assert.True(t, e.Excludes("X", filepath.Join(root, "pkg", "a.go")))

	assert.False(t, e.Excludes("X", ""), "a finding without a filename must stay")
	assert.False(t, e.Excludes("X", "."), "the root is not a file below the root")
	assert.False(t, e.Excludes("X", root), "the root is not a file below the root")
	assert.False(t, e.Excludes("X", filepath.Join("..", "a.go")), "files outside the root never match")
	assert.False(t, e.Excludes("X", filepath.Join(filepath.Dir(root), "other", "a.go")), "files outside the root never match")
	assert.False(t, e.Excludes("X", filepath.Join(root+"x", "a.go")), "a sibling directory with the same prefix is outside the root")
	assert.False(t, e.Excludes("Y", "a.go"), "cops without patterns exclude nothing")

	var none *config.Exclusions
	assert.False(t, none.Excludes("X", "a.go"), "a nil value excludes nothing")
}

func TestExclusions_EmptyConfigIgnoresWorkDir(t *testing.T) {
	load := func(path string) *config.Config {
		cfg, err := config.Load(path)
		require.NoError(t, err)
		return cfg
	}
	var nilConfig *config.Config
	for name, cfg := range map[string]*config.Config{
		"zero value":     {},
		"default":        config.DefaultConfig(),
		"nil":            nilConfig,
		"struct literal": {Cops: map[string]config.CopConfig{"X": {Enabled: new(true), Severity: "error"}}},
		"missing file":   load(filepath.Join(t.TempDir(), "missing.yml")),
		"empty file":     load(writeConfig(t, "")),
		"null cops":      load(writeConfig(t, "cops:\n")),
		"no exclude key": load(writeConfig(t, "cops:\n  X:\n    enabled: true\n    severity: error\n")),
		"null list":      load(writeConfig(t, "cops:\n  X:\n    exclude:\n")),
		"empty list":     load(writeConfig(t, "cops:\n  X:\n    exclude: []\n")),
		// yaml.v3 drops null items from a list of strings.
		"null items": load(writeConfig(t, "cops:\n  X:\n    exclude:\n      - ~\n      -\n")),
	} {
		t.Run(name, func(t *testing.T) {
			for _, workDir := range []string{"", "relative", t.TempDir()} {
				e, err := cfg.Exclusions(workDir)
				require.NoError(t, err)
				assert.False(t, e.Excludes("X", "a.go"))
			}
		})
	}
}

func TestExclusions_RequireAbsoluteWorkDir(t *testing.T) {
	cfg, _ := excluding(t, "gen/")
	for _, workDir := range []string{"", "relative", "."} {
		_, err := cfg.Exclusions(workDir)
		require.ErrorContains(t, err, "resolving exclude patterns: working directory")
	}
}

// TestExclusions_OnlyLoadReadsPatterns checks that the patterns belong to the
// configuration that Load returned.
func TestExclusions_OnlyLoadReadsPatterns(t *testing.T) {
	loaded, root := excluding(t, "gen/")
	gen := filepath.Join(root, "gen", "a.go")
	e, err := loaded.Exclusions(root)
	require.NoError(t, err)
	require.True(t, e.Excludes("X", gen))

	// A struct literal with the fields of the loaded configuration.
	constructed := &config.Config{DefaultCops: loaded.DefaultCops, Cops: loaded.Cops}

	// A YAML decoder that fills a Config directly ignores the exclude lists.
	decoded := config.DefaultConfig()
	require.NoError(t, yaml.Unmarshal([]byte(excludeConfig("gen/")), decoded))

	// A YAML encoder does not write the exclude lists.
	data, err := yaml.Marshal(loaded)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "exclude")
	encodedPath := filepath.Join(root, "encoded.yml")
	require.NoError(t, os.WriteFile(encodedPath, data, 0o644))
	encoded, err := config.Load(encodedPath)
	require.NoError(t, err)

	for name, cfg := range map[string]*config.Config{"constructed": constructed, "decoded": decoded, "encoded": encoded} {
		e, err := cfg.Exclusions(root)
		require.NoError(t, err, name)
		assert.False(t, e.Excludes("X", gen), name)
	}

	// Changes to Cops do not remove the patterns of a loaded configuration.
	delete(loaded.Cops, "X")
	e, err = loaded.Exclusions(root)
	require.NoError(t, err)
	assert.True(t, e.Excludes("X", gen))
}

func TestLoad_ExclusionsAreRelativeToConfigDirectory(t *testing.T) {
	root := t.TempDir()
	confDir := filepath.Join(root, "conf")
	require.NoError(t, os.MkdirAll(confDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(confDir, ".rubocop-go.yml"), []byte("cops:\n  X:\n    exclude:\n      - gen/\n"), 0o644))
	other := t.TempDir()

	// Load a relative path, then change the working directory.
	t.Chdir(root)
	cfg, err := config.Load(filepath.Join("conf", ".rubocop-go.yml"))
	require.NoError(t, err)
	t.Chdir(other)

	for _, workDir := range []string{root, other} {
		e, err := cfg.Exclusions(workDir)
		require.NoError(t, err)
		assert.True(t, e.Excludes("X", filepath.Join(confDir, "gen", "a.go")), "absolute names resolve against the config directory")
		assert.False(t, e.Excludes("X", filepath.Join(root, "gen", "a.go")), "the working directory is not the pattern root")
		assert.False(t, e.Excludes("X", filepath.Join(workDir, "gen", "a.go")), "the working directory is not the pattern root")
	}

	// Relative names resolve against the working directory.
	e, err := cfg.Exclusions(root)
	require.NoError(t, err)
	assert.True(t, e.Excludes("X", filepath.Join("conf", "gen", "a.go")))
	assert.False(t, e.Excludes("X", filepath.Join("gen", "a.go")))
	e, err = cfg.Exclusions(confDir)
	require.NoError(t, err)
	assert.True(t, e.Excludes("X", filepath.Join("gen", "a.go")))

	// A copy keeps the directory of the loaded file.
	copied := *cfg
	e, err = copied.Exclusions(other)
	require.NoError(t, err)
	assert.True(t, e.Excludes("X", filepath.Join(confDir, "gen", "a.go")))
}

func TestLoad_ExcludeValues(t *testing.T) {
	for _, tc := range []struct {
		name, content   string
		matches, misses []string
	}{
		{"list", "cops:\n  X:\n    exclude: [gen/, '**/*_test.go']\n", []string{"gen/a.go", "a_test.go", "pkg/a_test.go"}, []string{"a.go", "pkg/gen/a.go"}},
		{"scalars become strings", "cops:\n  X:\n    exclude: [1, true]\n", []string{"1", "1/a.go", "true/a.go"}, []string{"a.go", "10"}},
		{"null items are dropped", "cops:\n  X:\n    exclude:\n      - ~\n      - gen/\n      -\n", []string{"gen/a.go"}, []string{"a.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.content)
			cfg, err := config.Load(path)
			require.NoError(t, err)
			e, err := cfg.Exclusions(filepath.Dir(path))
			require.NoError(t, err)
			for _, rel := range tc.matches {
				assert.True(t, e.Excludes("X", rel), rel)
			}
			for _, rel := range tc.misses {
				assert.False(t, e.Excludes("X", rel), rel)
			}
		})
	}
}

func TestLoad_ExclusionOnlyEntryDoesNotEnable(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "cops:\n  X:\n    exclude: [gen/]\n"))
	require.NoError(t, err)
	assert.False(t, cfg.IsEnabledWithDefault("X", false), "an exclusion-only entry must not select an opt-in cop")
	assert.True(t, cfg.IsEnabledWithDefault("X", true), "an exclusion-only entry must not disable a default cop")

	cfg, err = config.Load(writeConfig(t, "default_cops: false\ncops:\n  X:\n    exclude: [gen/]\n"))
	require.NoError(t, err)
	assert.False(t, cfg.IsEnabled("X"))
}

var invalidPatterns = []struct{ pattern, message string }{
	{"", "the pattern is empty"},
	{"/", "the pattern is absolute"},
	{"/abs/gen", "the pattern is absolute"},
	{"//abs", "the pattern is absolute"},
	{"..", `the pattern contains a ".." component`},
	{"../gen", `the pattern contains a ".." component`},
	{"gen/../x", `the pattern contains a ".." component`},
	{"gen/..", `the pattern contains a ".." component`},
	{`gen\x`, `the pattern contains "\"`},
	{`\*.go`, `the pattern contains "\"`},
	{".", "the pattern names only the configuration directory"},
	{"./", "the pattern names only the configuration directory"},
	{"././/", "the pattern names only the configuration directory"},
	{"[", `component "[": syntax error in pattern`},
	{"gen/[a", `component "[a": syntax error in pattern`},
	{"[]", `component "[]": syntax error in pattern`},
	{"[^]", `component "[^]": syntax error in pattern`},
	{"[a-]", `component "[a-]": syntax error in pattern`},
	{"a[/]b", `component "a[": syntax error in pattern`},
	{"**.go", `component "**.go": "**" must be a complete path component`},
	{"gen**", `component "gen**": "**" must be a complete path component`},
	{"a/***/b", `component "***": "**" must be a complete path component`},
	{"[x]**", `component "[x]**": "**" must be a complete path component`},
}

func TestLoad_RejectsInvalidPatterns(t *testing.T) {
	cases := invalidPatterns
	if runtime.GOOS == "windows" {
		cases = append(cases, struct{ pattern, message string }{"C:/gen", "the pattern is absolute"},
			struct{ pattern, message string }{"C:gen", "the pattern is absolute"})
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			want := fmt.Sprintf("invalid config: cop %q: exclude[1] %q: %s", "X", tc.pattern, tc.message)

			// X is not a shipped cop. Load also checks a disabled entry.
			for _, entry := range []string{"X:\n", "X:\n    enabled: false\n"} {
				content := fmt.Sprintf("cops:\n  %s    exclude:\n      - ok/\n      - %q\n", entry, tc.pattern)
				cfg, err := config.Load(writeConfig(t, content))
				require.ErrorContains(t, err, want, content)
				assert.Nil(t, cfg)
			}
		})
	}
}

func TestLoad_FirstExcludeErrorIsDeterministic(t *testing.T) {
	content := "cops:\n  Z/Last:\n    exclude: [../z]\n  A/First:\n    exclude: [ok/, /a]\n  M/Middle:\n    exclude: ['']\n"
	for range 20 {
		_, err := config.Load(writeConfig(t, content))
		require.EqualError(t, err, `invalid config: cop "A/First": exclude[1] "/a": the pattern is absolute; use a path relative to the configuration directory`)
	}
}
