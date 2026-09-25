package cops

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
)

func TestSharedCopsRunnerPolicy(t *testing.T) {
	dir := t.TempDir()
	const src = `package p
import "strings"
func f(s string) *int {
 for _, word := range strings.Fields(s) { _ = word }
 for _, word := range strings.Fields(s) { _ = word } //rubocop:disable Lint/FieldsSeq
 n := 1
 return &n
}
func suppressed() *int {
 n := 1 //rubocop:disable Lint/NewExpr
 return &n
}
`
	for name, content := range map[string]string{
		"go.mod":      "module example.test\n\ngo 1.26\n",
		"p.go":        src,
		"legacy/p.go": src,
		"disabled.go": "//rubocop:disable-file Lint/NewExpr,Lint/FieldsSeq\n" + src,
	} {
		// Keep declarations distinct in files of the same package.
		if name == "disabled.go" {
			content = "//rubocop:disable-file Lint/NewExpr,Lint/FieldsSeq\npackage p\nimport \"strings\"\nfunc disabled(s string) *int { for range strings.Fields(s) {}; n := 1; return &n }\n"
		}
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	t.Chdir(dir)
	scope := cop.WithScope(cop.Not(cop.UnderDir("legacy")))
	cfg := config.DefaultConfig()
	cfg.Cops["Lint/NewExpr"] = config.CopConfig{Severity: "error"}
	cfg.Cops["Lint/FieldsSeq"] = config.CopConfig{Severity: "error"}
	var output bytes.Buffer
	r := runner.New([]cop.Cop{NewLintNewExpr(scope)}, cfg, &output).
		WithProgramCops([]prog.Cop{NewLintFieldsSeq(scope)})
	r.Reporter = runner.NewJSONReporter(&output)
	count, err := r.Run([]string{dir})
	require.NoError(t, err)
	require.Equal(t, 2, count, output.String())
	var result struct {
		Offenses []struct {
			Cop, Severity, File string
			Line                int
			EndColumn           int `json:"end_column"`
		}
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.Len(t, result.Offenses, 2)
	assert.Equal(t, "Lint/FieldsSeq", result.Offenses[0].Cop)
	assert.Equal(t, 4, result.Offenses[0].Line)
	assert.Equal(t, "Lint/NewExpr", result.Offenses[1].Cop)
	assert.Equal(t, 6, result.Offenses[1].Line)
	for _, offense := range result.Offenses {
		assert.Equal(t, "error", offense.Severity)
		assert.Equal(t, filepath.Join(dir, "p.go"), offense.File)
		assert.Positive(t, offense.EndColumn)
	}
}

func TestSharedContextAndFatalRunnerPolicy(t *testing.T) {
	t.Parallel()
	const src = `package p
import ("context"; "log"; "net/http")
func run(id string, ctx context.Context) {}
type Service struct { ctx context.Context }
var req, err = http.NewRequest("GET", "/", nil)
func stop() { log.Fatal("boom") }
func suppressed(id string, ctx context.Context) {} //rubocop:disable Lint/ContextFirstParameter
type Suppressed struct { ctx context.Context } //rubocop:disable Lint/NoContextField
var ignored, ignoredErr = http.NewRequest("GET", "/", nil) //rubocop:disable Lint/HTTPRequestWithContext
func suppressedStop() { log.Fatal("boom") } //rubocop:disable Lint/NoFatalOutsideMain
`
	dir := t.TempDir()
	for name, content := range map[string]string{
		"p.go":          src,
		"p_test.go":     src,
		"legacy/p.go":   src,
		"disabled/p.go": "//rubocop:disable-file Lint/ContextFirstParameter,Lint/NoContextField,Lint/HTTPRequestWithContext,Lint/NoFatalOutsideMain\n" + src,
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	scope := cop.WithScope(cop.Not(cop.UnderDir("legacy")))
	checks := []cop.Cop{
		NewLintContextFirstParameter(scope), NewLintNoContextField(scope),
		NewLintHTTPRequestWithContext(scope), NewLintNoFatalOutsideMain(scope),
	}
	cfg := config.DefaultConfig()
	for _, c := range checks {
		cfg.Cops[c.Name()] = config.CopConfig{Severity: "warning"}
	}
	var output bytes.Buffer
	r := runner.New(checks, cfg, &output)
	r.Reporter = runner.NewJSONReporter(&output)
	count, err := r.Run([]string{dir})
	require.NoError(t, err)
	require.Equal(t, 4, count, output.String())
	var result struct {
		Offenses []struct {
			Cop, Severity, File string
			Line                int
			EndColumn           int `json:"end_column"`
		}
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.Len(t, result.Offenses, len(checks))
	for i, offense := range result.Offenses {
		assert.Equal(t, checks[i].Name(), offense.Cop)
		assert.Equal(t, i+3, offense.Line)
		assert.Equal(t, "warning", offense.Severity)
		assert.Equal(t, filepath.Join(dir, "p.go"), offense.File)
		assert.Positive(t, offense.EndColumn)
	}
}

func TestStdlibUUIDScopeRetainsRandomnessEvidence(t *testing.T) {
	requireGo127(t)
	t.Parallel()
	files := coptest.ProgramFiles{
		"go.mod":                  "module example.test\n\ngo 1.27\nrequire github.com/google/uuid v0.0.0\nreplace github.com/google/uuid => ./google\n",
		"google/go.mod":           "module github.com/google/uuid\n\ngo 1.27\n",
		"google/uuid.go":          "package uuid\nfunc NewString() string { return \"id\" }; func SetRand() {}\n",
		"uuid.go":                 "package p\nimport \"github.com/google/uuid\"\nfunc ID() string { return uuid.NewString() }\n",
		"excluded/random_test.go": "package excluded\nimport \"github.com/google/uuid\"\nvar _ = uuid.SetRand\n",
	}
	c := NewLintStdlibUUID(cop.WithScope(cop.Not(cop.UnderDir("excluded"))))
	assert.Empty(t, coptest.RunProgram(t, c, files))
	delete(files, "excluded/random_test.go")
	require.Len(t, coptest.RunProgram(t, c, files), 1)
}

func TestBenchmarkLoopConfiguredScope(t *testing.T) {
	t.Parallel()
	files := coptest.ProgramFiles{
		"go.mod":               "module example.test\n\ngo 1.26\n",
		"bench_test.go":        benchmarkLoopFixture,
		"legacy/bench_test.go": benchmarkLoopFixture,
	}
	c := NewLintBenchmarkLoop(cop.WithScope(cop.Not(cop.UnderDir("legacy"))))
	offenses := coptest.RunProgram(t, c, files)
	require.Len(t, offenses, 1)
	assert.Equal(t, "bench_test.go", filepath.Base(offenses[0].Pos.Filename))
	assert.NotContains(t, offenses[0].Pos.Filename, "/legacy/")
}
