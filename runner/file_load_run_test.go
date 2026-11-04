package runner

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

func TestSharedFileLoadIsOptIn(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"p.go":   "package p\nvar Value int\n",
	})
	for _, withTypes := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			calls := 0
			c := cop.New(cop.Meta{Name: "Test/Views"}, func(p *cop.Pass) {
				calls++
				assert.Equal(t, withTypes, p.Info != nil)
				assert.Equal(t, withTypes, p.Package != nil)
				views, err := p.TypedFiles()
				if !shared {
					require.EqualError(t, err, "typed file views unavailable")
					assert.Nil(t, views)
					return
				}
				require.NoError(t, err)
				require.Len(t, views, 1)
				assert.Same(t, p.Cop, views[0].Cop)
				assert.NotSame(t, p.File, views[0].File)
				assert.NotSame(t, p.FileSet, views[0].FileSet)
				assert.NotNil(t, views[0].Info)
				assert.NotNil(t, views[0].Package.Scope().Lookup("Value"))
			})
			c.Types = withTypes
			r := New([]cop.Cop{c}, config.DefaultConfig(), io.Discard)
			if shared {
				assert.Same(t, r, r.WithSharedFileLoad())
			}
			_, err := r.Run([]string{dir})
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
		}
	}
}

func TestSharedFileLoadStaysLazy(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"p.go":   "package p\n",
	})
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, files[0], nil, 0)
	require.NoError(t, err)
	l := newSharedFileLoader(files)
	loads := 0
	l.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		return packages.Load(cfg, patterns...)
	}
	unexpected := func(*cop.Pass) { t.Error("filtered cop must not run") }
	cops := []cop.Cop{
		cop.New(cop.Meta{Name: "Test/Scope"}, unexpected, cop.WithScope(func(*cop.Pass) bool { return false })),
		cop.New(cop.Meta{Name: "Test/Candidate"}, unexpected, cop.WithCandidate(func(*ast.File) bool { return false })),
		cop.New(cop.Meta{Name: "Test/Syntax"}, func(*cop.Pass) {}),
	}
	r := New(cops, config.DefaultConfig(), io.Discard)
	assert.Empty(t, r.runCopsWithTypedFiles(fset, file, nil, nil, l))
	assert.Nil(t, l.contexts, "even environment checks and grouping must stay lazy")
	assert.Zero(t, loads)

	// Scope may request views, and Check must reuse the same lazy load.
	probe := cop.New(cop.Meta{Name: "Test/Probe"}, func(p *cop.Pass) {
		views, err := p.TypedFiles()
		require.NoError(t, err)
		assert.Len(t, views, 1)
	}, cop.WithScope(func(p *cop.Pass) bool {
		views, err := p.TypedFiles()
		require.NoError(t, err)
		return len(views) == 1
	}))
	r.Cops = []cop.Cop{probe, probe}
	r.runCopsWithTypedFiles(fset, file, nil, nil, l)
	assert.Equal(t, 1, loads)
}

func TestSharedFileLoadIncludesUncollectedSiblings(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	t.Chdir(dir)
	importFixture(t, dir, map[string]string{
		"go.mod":     "module example.test\ngo 1.26\n",
		"p.go":       "package p\n//line alternate.go:99\nvar Value Sibling\n",
		"sibling.go": "package p\ntype Sibling int\n",
		"p_test.go":  "package p\nvar TestValue Sibling\n",
	})
	calls := 0
	c := cop.New(cop.Meta{Name: "Test/Siblings"}, func(p *cop.Pass) {
		calls++
		views, err := p.TypedFiles()
		require.NoError(t, err)
		require.Len(t, views, 2)
		for _, view := range views {
			assert.Equal(t, filepath.Join(dir, "p.go"), view.Filename())
			assert.NotNil(t, view.Package.Scope().Lookup("Sibling"))
			id := view.File.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0]
			assert.Same(t, view.Package.Scope().Lookup("Value"), view.Info.Defs[id])
		}
		assert.Nil(t, views[0].Package.Scope().Lookup("TestValue"))
		assert.NotNil(t, views[1].Package.Scope().Lookup("TestValue"))
	})
	r := New([]cop.Cop{c}, config.DefaultConfig(), io.Discard).WithSharedFileLoad()
	_, err := r.Run([]string{"p.go"})
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "loading siblings must not expand file coverage")
}

func TestSharedFileLoadRefreshesEachRun(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	importFixture(t, dir, map[string]string{
		"go.mod":       "module example.test\ngo 1.26\n",
		"p.go":         "package p\nvar First int\n",
		"default.go":   "//go:build !alternate\n\npackage p\nvar Default int\n",
		"alternate.go": "//go:build alternate\n\npackage p\nvar Alternate int\n",
	})
	var snapshots []*cop.Pass
	c := cop.New(cop.Meta{Name: "Test/Refresh"}, func(p *cop.Pass) {
		views, err := p.TypedFiles()
		require.NoError(t, err)
		require.Len(t, views, 1)
		snapshots = append(snapshots, views[0])
	})
	r := New([]cop.Cop{c}, config.DefaultConfig(), io.Discard).WithSharedFileLoad()
	_, err := r.Run([]string{filepath.Join(dir, "p.go")})
	require.NoError(t, err)
	importFixture(t, dir, map[string]string{"p.go": "package p\nvar Second int\n"})
	t.Setenv("GOFLAGS", "-tags=alternate")
	_, err = r.Run([]string{filepath.Join(dir, "p.go")})
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	assert.NotSame(t, snapshots[0].FileSet, snapshots[1].FileSet)
	for _, name := range []string{"First", "Default"} {
		assert.NotNil(t, snapshots[0].Package.Scope().Lookup(name))
		assert.Nil(t, snapshots[1].Package.Scope().Lookup(name))
	}
	for _, name := range []string{"Second", "Alternate"} {
		assert.Nil(t, snapshots[0].Package.Scope().Lookup(name))
		assert.NotNil(t, snapshots[1].Package.Scope().Lookup(name))
	}
}

func TestSharedFileLoadPreservesReportingAndFallback(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	importFixture(t, dir, map[string]string{
		"go.mod":          "module example.test\ngo 1.26\n",
		"p.go":            "package p\nvar Value int\n",
		"suppressed/p.go": "//rubocop:disable-file Test/Report\npackage suppressed\nvar Value int\n",
		"partial/p.go":    "package partial\nvar Value int\nvar _ = undefined\n",
		"inactive/p.go":   "//go:build lint_disabled_fixture\n\npackage inactive\nvar Value int\n",
		"syntax/p.go":     "package syntax\nfunc broken(\n",
	})
	run := func(shared bool) string {
		var out bytes.Buffer
		fallbacks, empty := 0, 0
		report := cop.New(cop.Meta{Name: "Test/Report", Severity: cop.Warning}, func(p *cop.Pass) {
			p.Report(p.File, "found")
		})
		local := WithDirectoryTypes(report)
		wrapper := cop.New(cop.Meta{Name: "Test/Report"}, func(p *cop.Pass) {
			views, err := p.TypedFiles()
			if err != nil {
				fallbacks++
				local.Check(p)
				return
			}
			if len(views) == 0 {
				empty++
				return
			}
			view := views[0]
			p.ReportOffense(cop.NewOffense(report, view.FileSet, view.File, "found"))
		})
		cfg := config.DefaultConfig()
		cfg.Cops["Test/Report"] = config.CopConfig{Severity: "error"}
		r := New([]cop.Cop{wrapper}, cfg, io.Discard)
		r.Reporter = NewJSONReporter(&out)
		if shared {
			r.WithSharedFileLoad()
		}
		count, err := r.Run([]string{dir})
		require.NoError(t, err)
		if shared {
			assert.Equal(t, 1, fallbacks)
			assert.Equal(t, 1, empty)
			assert.Equal(t, 2, count)
		} else {
			assert.Equal(t, 4, fallbacks)
			assert.Zero(t, empty)
			assert.Equal(t, 3, count)
		}
		return out.String()
	}
	assert.Contains(t, run(false), `"files_inspected": 4`)
	got := run(true)
	assert.Contains(t, got, `"files_inspected": 4`)
	assert.Contains(t, got, `"severity": "error"`)
	assert.NotContains(t, got, "suppressed/p.go")
}

func TestSharedFileLoadDoesNotChangeExistingCops(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	importFixture(t, dir, map[string]string{
		"go.mod":          "module example.test\ngo 1.26\n",
		"p.go":            "package p\nvar Value int\n",
		"p_test.go":       "package p\nvar _ = Value\n",
		"ext_test.go":     "package p_test\n",
		"inactive/p.go":   "//go:build lint_disabled_fixture\n\npackage inactive\n",
		"suppressed/p.go": "//rubocop:disable-file Test/Report\npackage suppressed\n",
		"broken/p.go":     "package broken\nfunc bad(\n",
		"scoped/p.go":     "package scoped\n",
	})
	for _, withTypes := range []bool{false, true} {
		run := func(shared bool) string {
			c := cop.New(cop.Meta{Name: "Test/Report"}, func(p *cop.Pass) {
				p.Report(p.File, "found")
			}, cop.WithScope(func(p *cop.Pass) bool { return p.PackageName() != "scoped" }))
			c.Types = withTypes
			var out bytes.Buffer
			r := New([]cop.Cop{c}, config.DefaultConfig(), io.Discard)
			r.Reporter = NewJSONReporter(&out)
			if shared {
				r.WithSharedFileLoad()
			}
			count, err := r.Run([]string{dir})
			require.NoError(t, err)
			assert.Equal(t, 4, count)
			return out.String()
		}
		assert.JSONEq(t, run(false), run(true))
	}
}

func TestSharedFileAndProgramLoadsAreIndependent(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	t.Chdir(dir)
	importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"p.go":   "package p\n",
	})
	var fileSnapshot *cop.Pass
	fileCop := cop.New(cop.Meta{Name: "Test/File"}, func(p *cop.Pass) {
		views, err := p.TypedFiles()
		require.NoError(t, err)
		require.Len(t, views, 1)
		fileSnapshot = views[0]
	})
	// Reporter completion marks the end of the stable-source file phase.
	baseReporter := NewTextReporter(io.Discard)
	called := false
	programCop := prog.FromFile(cop.New(cop.Meta{Name: "Test/Program"}, func(p *cop.Pass) {
		called = true
		assert.NotNil(t, p.Package.Scope().Lookup("Added"))
		assert.Nil(t, fileSnapshot.Package.Scope().Lookup("Added"))
		assert.NotSame(t, fileSnapshot.FileSet, p.FileSet)
	}))
	r := New([]cop.Cop{fileCop}, config.DefaultConfig(), io.Discard).
		WithSharedFileLoad().WithProgramCops([]prog.Cop{programCop}).WithSharedProgramLoad()
	r.Reporter = &fileFinishedReporter{Reporter: baseReporter, finished: func() {
		importFixture(t, dir, map[string]string{"p.go": "package p\nvar Added int\n"})
	}}
	_, err := r.Run([]string{"p.go"})
	require.NoError(t, err)
	assert.True(t, called)
}

type fileFinishedReporter struct {
	Reporter

	finished func()
}

func (r *fileFinishedReporter) FileFinished(path string, offenses []cop.Offense) {
	r.Reporter.FileFinished(path, offenses)
	r.finished()
}
