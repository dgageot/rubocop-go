package runner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestImportResolverSharesModuleGraph(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"a/a.go": "package a\nimport \"net/url\"\nvar _ url.URL\n",
		"b/b.go": "package b\nimport \"net/http\"\nvar _ http.Request\n",
	})
	r := newImportResolver(files)
	a := r.resolve(filepath.Join(dir, "a"), nil)
	b := r.resolve(filepath.Join(dir, "b"), nil)
	require.Same(t, a, b)
	url, err := a.Import("net/url")
	require.NoError(t, err)
	http, err := b.Import("net/http")
	require.NoError(t, err)
	assert.Contains(t, http.Imports(), url)
}

func TestImportResolverSeparatesNestedModules(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":        "module outer.test\ngo 1.26\nrequire dep.test v0.0.0\nreplace dep.test => ./first\n",
		"a.go":          "package outer\nimport \"dep.test\"\nvar _ dep.First\n",
		"first/go.mod":  "module dep.test\ngo 1.26\n",
		"first/dep.go":  "package dep\ntype First int\n",
		"inner/go.mod":  "module inner.test\ngo 1.26\nrequire dep.test v0.0.0\nreplace dep.test => ../second\n",
		"inner/a.go":    "package inner\nimport \"dep.test\"\nvar _ dep.Second\n",
		"second/go.mod": "module dep.test\ngo 1.26\n",
		"second/dep.go": "package dep\ntype Second int\n",
	})
	r := newImportResolver(files)
	outer, err := r.resolve(dir, nil).Import("dep.test")
	require.NoError(t, err)
	inner, err := r.resolve(filepath.Join(dir, "inner"), nil).Import("dep.test")
	require.NoError(t, err)
	assert.NotNil(t, outer.Scope().Lookup("First"))
	assert.Nil(t, outer.Scope().Lookup("Second"))
	assert.NotNil(t, inner.Scope().Lookup("Second"))
	assert.Nil(t, inner.Scope().Lookup("First"))
}

func TestImportResolverRejectsBrokenFiles(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":    "module example.test\ngo 1.26\n",
		"a.go":      "package p\nimport \"net/url\"\nvar _ url.URL\n",
		"broken.go": "package p\nimport \"net/http\"\nfunc broken(\n",
	})
	r := newImportResolver(files)
	loaded, ok := r.resolve(dir, nil).(*resolvedImporter)
	require.True(t, ok)
	assert.Contains(t, loaded.packages, "net/url")
	assert.NotContains(t, loaded.packages, "net/http")
}

func TestImportResolverRefreshesBetweenRuns(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":     "module example.test\ngo 1.26\n",
		"a.go":       "package p\nimport \"example.test/lib\"\nvar _ lib.First\n",
		"lib/lib.go": "package lib\ntype First int\n",
	})
	first, err := newImportResolver(files).resolve(dir, nil).Import("example.test/lib")
	require.NoError(t, err)
	importFixture(t, dir, map[string]string{"lib/lib.go": "package lib\ntype Second int\n"})
	second, err := newImportResolver(files).resolve(dir, nil).Import("example.test/lib")
	require.NoError(t, err)
	assert.NotNil(t, first.Scope().Lookup("First"))
	assert.Nil(t, second.Scope().Lookup("First"))
	assert.NotNil(t, second.Scope().Lookup("Second"))
}

func TestImportResolverCustomEnvironment(t *testing.T) {
	for _, variable := range []string{"GO111MODULE", "GOFLAGS", "GOPACKAGESDRIVER"} {
		t.Run(variable, func(t *testing.T) {
			value := "off"
			if variable == "GOFLAGS" {
				value = "-tags=custom"
			}
			if variable == "GOPACKAGESDRIVER" {
				value = "custom-driver"
			}
			t.Setenv(variable, value)
			assert.Empty(t, newImportResolver([]string{"a.go"}).contexts)
		})
	}
}

func TestImportResolverPersistedEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goenv")
	require.NoError(t, os.WriteFile(path, []byte("GOFLAGS=-tags=custom\n"), 0o600))
	t.Setenv("GOENV", path)
	t.Setenv("GOFLAGS", "")
	assert.False(t, canShareImports())
}

func BenchmarkImportResolver(b *testing.B) {
	isolateImportEnvironment(b)
	dir := b.TempDir()
	files := importFixture(b, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"a/a.go": "package a\nimport \"net/url\"\nvar _ url.URL\n",
		"b/b.go": "package b\nimport \"net/http\"\nvar _ http.Request\n",
		"c/c.go": "package c\nimport \"net/http\"\nvar _ http.Request\n",
	})
	parsed := make(map[string][]*ast.File)
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(b, err)
		parsed[filepath.Dir(path)] = append(parsed[filepath.Dir(path)], file)
	}
	b.Run("per-directory", func(b *testing.B) {
		for b.Loop() {
			for dir, files := range parsed {
				resolveImports(dir, files)
			}
		}
	})
	b.Run("per-module", func(b *testing.B) {
		for b.Loop() {
			r := newImportResolver(files)
			for dir, files := range parsed {
				r.resolve(dir, files)
			}
		}
	})
}

func importFixture(tb testing.TB, dir string, sources map[string]string) []string {
	tb.Helper()
	var files []string
	for name, source := range sources {
		path := filepath.Join(dir, name)
		require.NoError(tb, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(tb, os.WriteFile(path, []byte(source), 0o600))
		if filepath.Ext(path) == ".go" {
			files = append(files, path)
		}
	}
	return files
}

func isolateImportEnvironment(tb testing.TB) {
	tb.Helper()
	tb.Setenv("GOENV", "off")
	tb.Setenv("GOFLAGS", "")
	tb.Setenv("GO111MODULE", "on")
	tb.Setenv("GOPACKAGESDRIVER", "off")
	tb.Setenv("GOWORK", "off")
}

func TestImportResolverBatchFailureRetainsLocalImports(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":           "module example.test\ngo 1.26\n",
		"a/a.go":           "package a\nimport \"example.test/lib\"\nvar _ lib.Value\n",
		"lib/lib.go":       "package lib\ntype Value int\n",
		"broken/broken.go": `package broken; import _ "\x00"`,
	})
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "a", "a.go"), nil, 0)
	require.NoError(t, err)
	r := newImportResolver(files)
	loaded := r.resolve(filepath.Join(dir, "a"), []*ast.File{file})
	pkg, err := loaded.Import("example.test/lib")
	require.NoError(t, err)
	assert.NotNil(t, pkg.Scope().Lookup("Value"))
	assert.True(t, r.failed[r.contexts[filepath.Join(dir, "a")]])
}

func TestImportResolverFallsBackForImportCycles(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":             "module example.test\ngo 1.26\n",
		"a/a.go":             "package a\nimport \"example.test/cycle1\"\nvar _ cycle1.Value\n",
		"b/b.go":             "package b\nimport \"example.test/cycle2\"\nvar _ cycle2.Value\n",
		"c/c.go":             "package c\nimport \"example.test/healthy\"\nvar _ healthy.Value\n",
		"cycle1/cycle.go":    "package cycle1\nimport _ \"example.test/cycle2\"\ntype Value int\n",
		"cycle2/cycle.go":    "package cycle2\nimport _ \"example.test/cycle1\"\ntype Value int\n",
		"healthy/healthy.go": "package healthy\ntype Value int\n",
	})
	resolver := newImportResolver(files)
	for _, name := range []string{"cycle1", "cycle2", "healthy"} {
		consumer := map[string]string{"cycle1": "a", "cycle2": "b", "healthy": "c"}[name]
		path := filepath.Join(dir, consumer, consumer+".go")
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		local := resolveImports(filepath.Dir(path), []*ast.File{file})
		shared := resolver.resolve(filepath.Dir(path), []*ast.File{file})
		_, wantErr := local.Import("example.test/" + name)
		pkg, gotErr := shared.Import("example.test/" + name)
		assert.Equal(t, wantErr == nil, gotErr == nil, name)
		if gotErr == nil {
			assert.NotNil(t, pkg.Scope().Lookup("Value"))
		}
	}
	assert.True(t, resolver.failed[resolver.contexts[filepath.Join(dir, "a")]])
}

func TestImportResolverRelativeGoenv(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	t.Chdir(dir)
	files := importFixture(t, dir, map[string]string{
		"go.mod":        "module example.test\ngo 1.26\n",
		"a/a.go":        "package a\nimport \"example.test/lib\"\nvar _ lib.First\n",
		"b/b.go":        "package b\nimport \"example.test/lib\"\nvar _ lib.Second\n",
		"a/local.goenv": "GOFLAGS=-tags=first\n",
		"b/local.goenv": "GOFLAGS=-tags=second\n",
		"lib/first.go":  "//go:build first\n\npackage lib\ntype First int\n",
		"lib/second.go": "//go:build second\n\npackage lib\ntype Second int\n",
	})
	t.Setenv("GOENV", "local.goenv")
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		resolver := newImportResolver(files)
		assert.Empty(t, resolver.contexts)
		for _, name := range order {
			path := filepath.Join(dir, name, name+".go")
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			require.NoError(t, err)
			pkg, err := resolver.resolve(filepath.Dir(path), []*ast.File{file}).Import("example.test/lib")
			require.NoError(t, err)
			if name == "a" {
				assert.NotNil(t, pkg.Scope().Lookup("First"))
				assert.Nil(t, pkg.Scope().Lookup("Second"))
			} else {
				assert.NotNil(t, pkg.Scope().Lookup("Second"))
				assert.Nil(t, pkg.Scope().Lookup("First"))
			}
		}
	}
}

func TestImportResolverSharesDespiteInactivePlatformImports(t *testing.T) {
	isolateImportEnvironment(t)
	dir := t.TempDir()
	files := importFixture(t, dir, map[string]string{
		"go.mod":               "module example.test\ngo 1.26\n",
		"a/a.go":               "package a\nimport \"net/url\"\nvar _ url.URL\n",
		"b/b.go":               "package b\nimport \"net/http\"\nvar _ http.Request\n",
		"inactive/inactive.go": "//go:build lint_disabled_fixture\n\npackage inactive\nimport _ \"example.test/disabled\"\n",
		"disabled/disabled.go": "//go:build lint_disabled_fixture\n\npackage disabled\ntype Value int\n",
	})
	r := newImportResolver(files)
	a := r.resolve(filepath.Join(dir, "a"), nil)
	b := r.resolve(filepath.Join(dir, "b"), nil)
	require.Same(t, a, b)
	url, err := a.Import("net/url")
	require.NoError(t, err)
	http, err := b.Import("net/http")
	require.NoError(t, err)
	assert.Contains(t, http.Imports(), url)
	_, err = a.Import("example.test/disabled")
	require.Error(t, err)
	assert.False(t, r.failed[r.contexts[filepath.Join(dir, "a")]])
}

func TestBuildExcludedPackageRequiresNoFiles(t *testing.T) {
	loadError := packages.Error{Msg: "build constraints exclude all Go files in /example"}
	assert.True(t, buildExcludedPackage(&packages.Package{Errors: []packages.Error{loadError}}))
	assert.False(t, buildExcludedPackage(&packages.Package{GoFiles: []string{"example.go"}, Errors: []packages.Error{loadError}}))
}
