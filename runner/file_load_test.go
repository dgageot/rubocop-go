package runner

import (
	"errors"
	"go/ast"
	"go/types"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/cop"
)

func TestSharedFileLoaderVariantsAndIdentity(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":         "module example.test\ngo 1.26\n",
		"a/a.go":         "package a\nimport \"net/url\"\nvar URL url.URL\n",
		"a/a_test.go":    "package a\nvar _ = URL\n",
		"a/ext_test.go":  "package a_test\nimport \"example.test/a\"\nvar _ = a.URL\n",
		"a/inactive.go":  "//go:build lint_disabled_fixture\n\npackage a\nvar _ = undefined\n",
		"b/b.go":         "package b\nimport \"net/http\"\nvar Request http.Request\n",
		"only/p_test.go": "package only\nvar Value int\n",
	})
	l := newSharedFileLoader(files)
	loads := 0
	var roots []*packages.Package
	l.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		assert.True(t, cfg.Tests)
		assert.Equal(t, packages.NeedName|packages.NeedFiles|packages.NeedCompiledGoFiles|packages.NeedSyntax|packages.NeedTypes|packages.NeedTypesInfo|packages.NeedImports, cfg.Mode)
		assert.ElementsMatch(t, []string{filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "only")}, patterns)
		var err error
		roots, err = packages.Load(cfg, patterns...)
		return roots, err
	}
	assert.Zero(t, loads)
	c := cop.New(cop.Meta{Name: "Test/Views"}, nil)
	views, err := l.typedFiles(filepath.Join(dir, "a", "a.go"), c)
	require.NoError(t, err)
	require.Len(t, views, 2, "production and augmented test variants")
	assert.NotSame(t, views[0].Package, views[1].Package)

	for _, name := range files {
		got, err := l.typedFiles(name, c)
		require.NoError(t, err)
		var want []*cop.Pass
		for _, pkg := range roots {
			for _, file := range pkg.Syntax {
				if pkg.Fset.PositionFor(file.Pos(), false).Filename == name {
					want = append(want, &cop.Pass{Cop: c, FileSet: pkg.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types})
				}
			}
		}
		require.Len(t, got, len(want), name)
		for i, view := range got {
			assert.Same(t, want[i].FileSet, view.FileSet)
			assert.Same(t, want[i].File, view.File)
			assert.Same(t, want[i].Info, view.Info)
			assert.Same(t, want[i].Package, view.Package)
			assert.Same(t, c, view.Cop)
			assert.Nil(t, view.SeverityOverride)
			assert.Empty(t, view.Offenses())
			_, err := view.TypedFiles()
			require.EqualError(t, err, "typed file views unavailable")
		}
	}
	inactive, err := l.typedFiles(filepath.Join(dir, "a", "inactive.go"), c)
	require.NoError(t, err)
	assert.Nil(t, inactive)
	b, err := l.typedFiles(filepath.Join(dir, "b", "b.go"), c)
	require.NoError(t, err)
	require.Len(t, b, 1)
	request := b[0].Package.Scope().Lookup("Request").Type().Underlying().(*types.Struct)
	var urlType types.Type
	for field := range request.Fields() {
		if field.Name() == "URL" {
			urlType = field.Type().(*types.Pointer).Elem()
		}
	}
	require.NotNil(t, urlType)
	for _, view := range views {
		assert.True(t, types.Identical(urlType, view.Package.Scope().Lookup("URL").Type()))
	}
	again, err := l.typedFiles(filepath.Join(dir, "a", "a.go"), c)
	require.NoError(t, err)
	assert.NotSame(t, views[0], again[0], "each request owns its pass, but shares read-only AST and types")
	assert.Same(t, views[0].File, again[0].File)
	assert.Equal(t, 1, loads)
}

func TestSharedFileLoaderSeparatesNestedModules(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	importFixture(t, dir, map[string]string{
		"go.mod":        "module outer.test\ngo 1.26\nrequire dep.test v0.0.0\nreplace dep.test => ./first\n",
		"a.go":          "package outer\nimport \"dep.test\"\nvar Value dep.First\n",
		"b/b.go":        "package b\n",
		"first/go.mod":  "module dep.test\ngo 1.26\n",
		"first/dep.go":  "package dep\ntype First int\n",
		"inner/go.mod":  "module inner.test\ngo 1.26\nrequire dep.test v0.0.0\nreplace dep.test => ../second\n",
		"inner/a.go":    "package inner\nimport \"dep.test\"\nvar Value dep.Second\n",
		"second/go.mod": "module dep.test\ngo 1.26\n",
		"second/dep.go": "package dep\ntype Second string\n",
	})
	files := []string{filepath.Join(dir, "a.go"), filepath.Join(dir, "b", "b.go"), filepath.Join(dir, "inner", "a.go")}
	l := newSharedFileLoader(files)
	var batches [][]string
	l.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		batches = append(batches, patterns)
		return packages.Load(cfg, patterns...)
	}
	for _, i := range []int{2, 0, 1, 0, 2} {
		views, err := l.typedFiles(files[i], nil)
		require.NoError(t, err)
		require.Len(t, views, 1)
		if i != 1 {
			value := views[0].Package.Scope().Lookup("Value").Type().(*types.Named)
			if i == 0 {
				assert.Equal(t, "First", value.Obj().Name())
			} else {
				assert.Equal(t, "Second", value.Obj().Name())
			}
		}
	}
	assert.Equal(t, [][]string{{filepath.Join(dir, "inner")}, {dir, filepath.Join(dir, "b")}}, batches)
}

func TestSharedFileLoaderDirectoryErrors(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":                "module example.test\ngo 1.26\n",
		"healthy/p.go":          "package healthy\n",
		"badtest/p.go":          "package badtest\nvar Value int\n",
		"badtest/p_test.go":     "package badtest\nvar _ = undefined\n",
		"badexternal/p.go":      "package badexternal\n",
		"badexternal/p_test.go": "package badexternal_test\nvar _ = undefined\n",
		"inactive/p.go":         "//go:build lint_disabled_fixture\n\npackage inactive\nvar _ = undefined\n",
		"baddep/p.go":           "package baddep\nimport _ \"example.test/dep\"\n",
		"dep/p.go":              "package dep\nvar _ = undefined\n",
		"missing/p.go":          "package missing\nimport _ \"example.test/missingdep\"\n",
		"syntax/p.go":           "package syntax\nfunc broken(\n",
	})
	l := newSharedFileLoader(files)
	loads := 0
	l.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		return packages.Load(cfg, patterns...)
	}
	for range 2 {
		for _, subdir := range []string{"badtest", "badexternal", "baddep", "dep", "missing", "syntax"} {
			views, err := l.typedFiles(filepath.Join(dir, subdir, "p.go"), nil)
			require.Error(t, err, subdir)
			assert.Nil(t, views)
		}
		views, err := l.typedFiles(filepath.Join(dir, "healthy", "p.go"), nil)
		require.NoError(t, err)
		assert.Len(t, views, 1)
		views, err = l.typedFiles(filepath.Join(dir, "inactive", "p.go"), nil)
		require.NoError(t, err)
		assert.Nil(t, views)
	}
	assert.Equal(t, 1, loads)
}

func TestSharedFileLoaderCachesBatchFailure(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"a/a.go": "package a\n",
		"b/b.go": "package b\n",
	})
	l := newSharedFileLoader(files)
	loads := 0
	failure := errors.New("batch failed")
	l.load = func(*packages.Config, ...string) ([]*packages.Package, error) {
		loads++
		return nil, failure
	}
	for range 2 {
		for _, file := range files {
			views, err := l.typedFiles(file, nil)
			require.ErrorIs(t, err, failure)
			assert.Nil(t, views)
		}
	}
	assert.Equal(t, 1, loads)
}

func TestSharedFileLoaderUnavailable(t *testing.T) {
	for _, tc := range []struct{ variable, value string }{
		{"GOFLAGS", "-modfile=custom.mod"},
		{"GOFLAGS", "-overlay=relative.json"},
		{"GO111MODULE", "off"},
		{"GOENV", "relative.goenv"},
		{"GOPACKAGESDRIVER", "custom-driver"},
		{"", "loose file"},
	} {
		t.Run(tc.variable+tc.value, func(t *testing.T) {
			isolateImportEnvironment(t)
			dir := t.TempDir()
			files := importFixture(t, dir, map[string]string{"p.go": "package p\n"})
			if tc.variable != "" {
				importFixture(t, dir, map[string]string{"go.mod": "module example.test\ngo 1.26\n"})
				t.Setenv(tc.variable, tc.value)
			}
			l := newSharedFileLoader(files)
			l.load = func(*packages.Config, ...string) ([]*packages.Package, error) {
				t.Error("unavailable views must not load packages")
				return nil, nil
			}
			for range 2 {
				views, err := l.typedFiles(files[0], nil)
				require.ErrorContains(t, err, "typed file views unavailable")
				assert.Nil(t, views)
			}
		})
	}
}

func TestSharedFileLoaderConcurrentRequests(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"a/a.go": "package a\nvar Value int\n",
		"b/b.go": "package b\nvar Value int\n",
	})
	l := newSharedFileLoader(files)
	loads := 0
	l.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		return packages.Load(cfg, patterns...)
	}
	var wg sync.WaitGroup
	for range 16 {
		for _, file := range files {
			wg.Go(func() {
				views, err := l.typedFiles(file, nil)
				if assert.NoError(t, err) && assert.Len(t, views, 1) {
					ast.Inspect(views[0].File, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok && id.Name == "Value" {
							assert.NotNil(t, views[0].Info.Defs[id])
						}
						return true
					})
				}
			})
		}
	}
	wg.Wait()
	assert.Equal(t, 1, loads)
}
