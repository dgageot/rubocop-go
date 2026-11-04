package runner

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/cop"
)

func TestTypedFilesFallbackCachesPartialDirectory(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":      "module example.test\ngo 1.26\n",
		"p.go":        "package p\nimport \"sync\"\nvar Group sync.WaitGroup\n",
		"p_test.go":   "package p\nvar _ = undefined\nfunc f() { Group.Add(1) }\n",
		"ext_test.go": "package p_test\nimport p \"example.test\"\nfunc f() { p.Group.Add(1) }\n",
	})
	l := newSharedFileLoader(files)
	loads := 0
	l.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		return packages.Load(cfg, patterns...)
	}
	for range 2 {
		for _, filename := range files {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filename, nil, 0)
			require.NoError(t, err)
			views, err := l.typedFiles(&cop.Pass{FileSet: fset, File: file})
			require.NoError(t, err)
			require.NotEmpty(t, views)
			for _, view := range views {
				require.NotNil(t, view.Info)
				cop.ForEachCallIn(view.File, func(call *ast.CallExpr) {
					sel := call.Fun.(*ast.SelectorExpr)
					method, ok := view.Info.Uses[sel.Sel].(*types.Func)
					require.True(t, ok)
					assert.Equal(t, "sync", method.Pkg().Path())
				})
			}
		}
	}
	assert.Equal(t, 2, loads, "one shared attempt and one cached directory load")
}

func TestTypedFilesFallbackCachesFailedLoads(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"p.go":   "package p\nimport \"sync\"\nvar Group sync.WaitGroup\n",
	})
	l := newSharedFileLoader(files)
	loads := 0
	l.load = func(*packages.Config, ...string) ([]*packages.Package, error) {
		loads++
		return nil, errors.New("load failed")
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, files[0], nil, 0)
	require.NoError(t, err)
	for range 2 {
		views, err := l.typedFiles(&cop.Pass{FileSet: fset, File: file})
		require.NoError(t, err)
		require.Len(t, views, 1)
		assert.NotNil(t, views[0].Package.Scope().Lookup("Group"))
	}
	assert.Equal(t, 2, loads)
}

func TestTypedFilesFallbackUsesDirectoryBuildContext(t *testing.T) {
	isolateImportEnvironment(t)
	t.Setenv("GOFLAGS", "-mod=mod")
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":   "module example.test\ngo 1.26\n",
		"state.go": "package p\nimport \"sync\"\nvar Group sync.WaitGroup\n",
		"p.go":     "package p\nfunc f() { Group.Add(1) }\n",
	})
	l := newSharedFileLoader(files)
	loads := 0
	l.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		assert.Equal(t, dir, cfg.Dir)
		assert.Equal(t, []string{"."}, patterns)
		return packages.Load(cfg, patterns...)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, "p.go"), nil, 0)
	require.NoError(t, err)
	views, err := l.typedFiles(&cop.Pass{FileSet: fset, File: file})
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.NotNil(t, views[0].Package.Scope().Lookup("Group"))
	assert.Equal(t, 1, loads)
}

func TestTypedFilesFallbackInactiveSource(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"p.go":   "//go:build never_enabled\n\npackage p\nimport \"sync\"\nvar Group sync.WaitGroup\n",
	})
	l := newSharedFileLoader(files)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, files[0], nil, 0)
	require.NoError(t, err)
	views, err := l.typedFiles(&cop.Pass{FileSet: fset, File: file})
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Same(t, file, views[0].File)
	group := views[0].Package.Scope().Lookup("Group")
	require.NotNil(t, group)
	assert.Equal(t, "sync.WaitGroup", types.TypeString(group.Type(), nil))
}
