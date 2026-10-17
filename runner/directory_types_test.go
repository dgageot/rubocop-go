package runner_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

func TestDirectoryTypesPreservesInactiveAndPartialFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const src = `package p
import command "os/exec"
func NewCommand(cmd *command.Cmd) *command.Cmd {
 _ = cmd.Run()
 return command.Command("echo")
}
`
	files := map[string]string{
		"go.mod":                  "module example.test\n\ngo 1.26\n",
		"healthy/sample.go":       src,
		"external/sample.go":      "package p\n",
		"external/sample_test.go": "package p_test\n" + src[len("package p\n"):] + "\nvar _ = missing\n",
		"testonly/sample_test.go": src + "\nvar _ = missing\n",
		"inactive/sample.go":      "//go:build never_enabled_tag\n\n" + src,
		"split/sample.go":         "package p\nimport \"os/exec\"\ntype Command = exec.Cmd\n",
		"split/feature.go":        "//go:build never_enabled_tag\n\npackage p\nfunc NewCommand(cmd *Command) *Command { _ = cmd.Run(); return cmd }\n",
		"suppressed/sample.go":    "//rubocop:disable-file Lint/ConstructorCommandExec\n" + src,
	}
	for name, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o700))
		require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
	}
	var out bytes.Buffer
	cfg := config.DefaultConfig()
	c := runner.WithDirectoryTypes(cops.NewLintConstructorCommandExecFile())
	r := runner.New([]cop.Cop{c}, cfg, &out)
	r.Reporter = runner.NewJSONReporter(&out)
	count, err := r.Run([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, 9, count, out.String())
	var result struct{ Offenses []struct{ File string } }
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	got := make(map[string]int)
	for _, o := range result.Offenses {
		path, err := filepath.Rel(dir, o.File)
		require.NoError(t, err)
		got[filepath.ToSlash(path)]++
	}
	assert.Equal(t, map[string]int{
		"healthy/sample.go": 2, "external/sample_test.go": 2, "testonly/sample_test.go": 2, "inactive/sample.go": 2, "split/feature.go": 1,
	}, got)
}

func TestDirectoryTypesDoesNotPrepareNonCandidates(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	file := &ast.File{Name: &ast.Ident{Name: "p"}}
	called := false
	c := runner.WithDirectoryTypes(cop.New(cop.Meta{Name: "Test/Candidate"}, func(*cop.Pass) { called = true }, cop.WithTypes(), cop.WithCandidate(func(*ast.File) bool { return false })))
	p := &cop.Pass{Cop: c, FileSet: fset, File: file}
	c.Check(p)
	assert.False(t, called)
	assert.Empty(t, p.Offenses())
}

func TestDirectoryTypesForwardsPositionsAndSeverity(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile("go.mod", []byte("module fixture\n\ngo 1.26\n"), 0o600))
	require.NoError(t, os.WriteFile("first.go", []byte("package p\nvar A = 1\n"), 0o600))
	require.NoError(t, os.WriteFile("second.go", []byte("package p\n//line generated.go:42:7\nvar B = 2\n"), 0o600))
	c := runner.WithDirectoryTypes(cop.New(cop.Meta{Name: "Test/Position", Severity: cop.Warning}, func(p *cop.Pass) {
		if filepath.Base(p.Filename()) != "first.go" {
			return
		}
		p.ReportAt(token.NoPos, token.NoPos, "no position")
		p.Report(p.File, "file")
		obj := p.Package.Scope().Lookup("B")
		require.NotNil(t, obj)
		p.ReportAt(obj.Pos(), obj.Pos()+1, "cross file")
	}))
	cfg := config.DefaultConfig()
	var out bytes.Buffer
	r := runner.New([]cop.Cop{c}, cfg, &out)
	r.Reporter = runner.NewJSONReporter(&out)
	count, err := r.Run([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	var result struct {
		Offenses []struct {
			File, Message, Severity string
			Line                    int
		}
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	for _, o := range result.Offenses {
		assert.Equal(t, "warning", o.Severity)
		if o.Message == "cross file" {
			assert.Equal(t, filepath.Join(dir, "generated.go"), o.File)
			assert.Equal(t, 42, o.Line)
		}
		if o.Message == "no position" {
			assert.Empty(t, o.File)
			assert.Zero(t, o.Line)
		}
	}
}

func TestDirectoryTypesRefreshesBetweenRuns(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile("go.mod", []byte("module fixture\n\ngo 1.26\n"), 0o600))
	require.NoError(t, os.WriteFile("source.go", []byte("package p\nimport \"os/exec\"\nfunc New() *exec.Cmd { return exec.Command(\"echo\") }\n"), 0o600))
	c := runner.WithDirectoryTypes(cops.NewLintConstructorCommandExecFile())
	var out bytes.Buffer
	r := runner.New([]cop.Cop{c}, config.DefaultConfig(), &out)
	count, err := r.Run([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.NoError(t, os.WriteFile("source.go", []byte("package p\nimport \"os/exec\"\nfunc New() *exec.Cmd { _ = exec.Command(\"first\"); return exec.Command(\"second\") }\n"), 0o600))
	count, err = r.Run([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}
