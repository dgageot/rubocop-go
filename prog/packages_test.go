package prog_test

import (
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/prog"
)

func TestPackagesWithTestsVariants(t *testing.T) {
	t.Parallel()
	p := testPackageProgram(t)
	require.Len(t, p.Packages, 2)
	assert.Empty(t, packageContaining(p.Packages, "internal_test.go"))

	pkgs, err := p.PackagesWithTests()
	require.NoError(t, err)
	for _, name := range []string{"source.go", "internal_test.go", "external_test.go", "only_test.go"} {
		pkg := packageContaining(pkgs, name)
		require.NotNil(t, pkg, name)
		assert.NotNil(t, pkg.TypesInfo, name)
		assert.Same(t, p.Fset, pkg.Fset, name)
		assert.NotNil(t, pkg.Module, name)
	}
	require.Len(t, p.Packages, 2, "the production/SSA view must remain unchanged")
}

func TestPackagesWithTestsCache(t *testing.T) {
	t.Parallel()
	p := testPackageProgram(t)
	const workers = 8
	results := make([][]*packages.Package, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() { results[i], errs[i] = p.PackagesWithTests() })
	}
	wg.Wait()
	for i := range workers {
		require.NoError(t, errs[i])
		require.NotEmpty(t, results[i])
		assert.Same(t, results[0][0], results[i][0])
	}

	dir := p.Packages[0].Dir
	write(t, filepath.Join(dir, "added.go"), "package p\nfunc Added() {}\n")
	cached, err := p.PackagesWithTests()
	require.NoError(t, err)
	assert.Nil(t, packageContaining(cached, "source.go").Types.Scope().Lookup("Added"))

	fresh, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	updated, err := fresh.PackagesWithTests()
	require.NoError(t, err)
	assert.NotSame(t, cached[0], updated[0])
	assert.NotNil(t, packageContaining(updated, "source.go").Types.Scope().Lookup("Added"))
}

func TestPackagesWithTestsPartialErrors(t *testing.T) {
	t.Parallel()
	p := testPackageProgram(t)
	write(t, filepath.Join(p.Packages[0].Dir, "broken_test.go"), "package p\nvar _ = missing\n")
	pkgs, err := p.PackagesWithTests()
	require.NoError(t, err, "package errors must remain available to each cop's policy")
	broken := packageContaining(pkgs, "broken_test.go")
	require.NotNil(t, broken)
	assert.True(t, broken.IllTyped)
	assert.NotEmpty(t, broken.Errors)
	assert.False(t, packageContaining(pkgs, "source.go").IllTyped)
}

func TestPackagesWithTestsCachesLoadError(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "missing")
	p := &prog.Program{Fset: token.NewFileSet(), Packages: []*packages.Package{{Dir: dir}}}
	_, originalErr := p.PackagesWithTests()
	require.Error(t, originalErr)

	require.NoError(t, os.Mkdir(dir, 0o700))
	write(t, filepath.Join(dir, "go.mod"), "module example.test\n\ngo 1.26\n")
	write(t, filepath.Join(dir, "source.go"), "package p\n")
	_, cachedErr := p.PackagesWithTests()
	assert.Equal(t, originalErr, cachedErr)

	fresh, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	pkgs, err := fresh.PackagesWithTests()
	require.NoError(t, err)
	assert.NotEmpty(t, pkgs)
}

func TestPackagesWithTestsEmpty(t *testing.T) {
	t.Parallel()
	var p prog.Program
	pkgs, err := p.PackagesWithTests()
	require.NoError(t, err)
	assert.Empty(t, pkgs)
}

func BenchmarkPackagesWithTests(b *testing.B) {
	p := testPackageProgram(b)
	b.Run("uncached", func(b *testing.B) {
		for b.Loop() {
			fresh := &prog.Program{Fset: token.NewFileSet(), Packages: p.Packages}
			_, err := fresh.PackagesWithTests()
			require.NoError(b, err)
		}
	})
	b.Run("cached", func(b *testing.B) {
		_, err := p.PackagesWithTests()
		require.NoError(b, err)
		for b.Loop() {
			_, err := p.PackagesWithTests()
			require.NoError(b, err)
		}
	})
}

func testPackageProgram(tb testing.TB) *prog.Program {
	tb.Helper()
	dir := tb.TempDir()
	for name, src := range map[string]string{
		"go.mod":                     "module example.test\n\ngo 1.26\n",
		"source.go":                  "package p\n",
		"internal_test.go":           "package p\nfunc internalHelper() {}\n",
		"external_test.go":           "package p_test\nfunc externalHelper() {}\n",
		"testonly/only_test.go":      "package testonly\nfunc helper() {}\n",
		"inactive/disabled_linux.go": "//go:build never_enabled_test_tag\n\npackage inactive\n",
	} {
		path := filepath.Join(dir, name)
		require.NoError(tb, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(tb, os.WriteFile(path, []byte(src), 0o600))
	}
	p, err := prog.LoadDir(dir, "./...")
	require.NoError(tb, err)
	return p
}

func packageContaining(pkgs []*packages.Package, basename string) *packages.Package {
	for _, pkg := range pkgs {
		if slices.ContainsFunc(pkg.GoFiles, func(path string) bool { return filepath.Base(path) == basename }) {
			return pkg
		}
	}
	return nil
}
