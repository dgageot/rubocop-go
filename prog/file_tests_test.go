package prog_test

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

func TestTypedFilesReusesProgramPackages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":            "module example.test\n\ngo 1.26\n",
		"source.go":         "package p\n",
		"source_test.go":    "package p\nvar _ = missing\n",
		"only/only_test.go": "package only\n",
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	}
	program, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	pkgs, err := program.PackagesWithTests()
	require.NoError(t, err)
	infos := make(map[*ast.File]*types.Info)
	want := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			want[program.Fset.PositionFor(file.Pos(), false).Filename] = true
			if _, ok := infos[file]; !ok {
				infos[file] = pkg.TypesInfo
			}
		}
	}

	seen := make(map[string]bool)
	check := prog.FromFileWithTests(&cop.Func{
		Meta: cop.Meta{Name: "Lint/TestTypedFiles"},
		Run: func(p *cop.Pass) {
			info, ok := infos[p.File]
			assert.True(t, ok, "must reuse cached syntax for %s", p.Filename())
			assert.Same(t, info, p.Info)
			assert.False(t, seen[p.Filename()], "files must be checked once")
			seen[p.Filename()] = true
		},
	})
	check.Check(&prog.Pass{Cop: check, Program: program})
	assert.Equal(t, want, seen)
	for _, name := range []string{"source.go", "source_test.go", "only/only_test.go"} {
		assert.Contains(t, seen, filepath.Join(dir, name))
	}
}

func TestFromFileWithTestsScopesAndSeverity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":           "module fixture\n\ngo 1.26\n",
		"source.go":        "package p\nfunc f() {}\n",
		"internal_test.go": "package p\nvar _ = missing\n",
		"external_test.go": "package p_test\nfunc g() {}\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	}
	program, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	c := cop.New(cop.Meta{Name: "Test/Files", Severity: cop.Warning}, func(p *cop.Pass) {
		assert.NotNil(t, p.Info)
		p.Report(p.File, "found")
	}, cop.WithScope(func(p *cop.Pass) bool { return p.IsTestFile() }), cop.WithMinGoVersion("go1.26"))
	check := prog.FromFileWithTests(c)
	severity := cop.Error
	pass := &prog.Pass{Cop: check, Program: program, SeverityOverride: &severity}
	check.Check(pass)
	require.Len(t, pass.Offenses(), 2)
	for _, offense := range pass.Offenses() {
		assert.Equal(t, cop.Error, offense.Severity)
		assert.Equal(t, c.Name(), offense.CopName)
	}
	c.MinGoVersion = "go1.27"
	pass = &prog.Pass{Cop: check, Program: program}
	check.Check(pass)
	assert.Empty(t, pass.Offenses())
}

func TestFromFileWithTestsLoadError(t *testing.T) {
	t.Parallel()
	program := &prog.Program{
		Fset:     token.NewFileSet(),
		Packages: []*packages.Package{{Dir: filepath.Join(t.TempDir(), "missing")}},
	}
	called := false
	c := cop.New(cop.Meta{Name: "Test/Load"}, func(*cop.Pass) { called = true })
	check := prog.FromFileWithTests(c)
	pass := &prog.Pass{Cop: check, Program: program}
	check.Check(pass)
	require.Len(t, pass.Offenses(), 1)
	assert.Contains(t, pass.Offenses()[0].Message, "cannot inspect packages:")
	assert.False(t, called)
}
