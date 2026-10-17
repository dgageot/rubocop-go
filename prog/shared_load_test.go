package prog_test

import (
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/prog"
)

func TestSharedLoadPreservesPackageViews(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":               "module example.test\n\ngo 1.26\n",
		"p/p.go":               "package p\nfunc Read(c chan int) { <-c }\n",
		"p/p_test.go":          "package p\nfunc Internal() {}\n",
		"p/external_test.go":   "package p_test\nfunc External() {}\n",
		"only/only_test.go":    "package only\nfunc TestOnly() {}\n",
		"command.test/main.go": "package main\nfunc main() {}\n",
		"inactive/disabled.go": "//go:build never_enabled_tag\n\npackage inactive\n",
	} {
		name = filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o700))
		require.NoError(t, os.WriteFile(name, []byte(src), 0o600))
	}
	baseline, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	shared, err := prog.LoadDirWithTests(dir, "./...")
	require.NoError(t, err)
	assert.Equal(t, packageViews(baseline.Packages), packageViews(shared.Packages))
	baselineTests, err := baseline.PackagesWithTests()
	require.NoError(t, err)
	sharedTests, err := shared.PackagesWithTests()
	require.NoError(t, err)
	assert.Equal(t, packageViews(baselineTests), packageViews(sharedTests))
	require.Len(t, shared.SSAPackages, len(shared.Packages))
	for i, pkg := range shared.Packages {
		require.NotNil(t, shared.SSAPackages[i])
		assert.Same(t, pkg.Types, shared.SSAPackages[i].Pkg)
		assert.Nil(t, pkg.Types.Scope().Lookup("Internal"))
		assert.Nil(t, pkg.Types.Scope().Lookup("External"))
		assert.Nil(t, pkg.Types.Scope().Lookup("TestOnly"))
	}
	var foundCommand bool
	for _, pkg := range shared.Packages {
		if pkg.PkgPath == "example.test/command.test" {
			foundCommand = true
		}
	}
	assert.True(t, foundCommand, "a legitimate .test command is a production root")
}

func TestSharedLoadRetainsPartialTests(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26\n")
	write(t, filepath.Join(dir, "source.go"), "package p\nfunc Work() {}\n")
	write(t, filepath.Join(dir, "source_test.go"), "package p\nvar _ = missing\n")
	shared, err := prog.LoadDirWithTests(dir, ".")
	require.NoError(t, err)
	require.Len(t, shared.Packages, 1)
	assert.False(t, shared.Packages[0].IllTyped)
	pkgs, err := shared.PackagesWithTests()
	require.NoError(t, err)
	broken := packageContaining(pkgs, "source_test.go")
	require.NotNil(t, broken)
	assert.True(t, broken.IllTyped)
	assert.NotNil(t, broken.TypesInfo)
}

func TestSharedLoadSnapshotAndRefresh(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26\n")
	write(t, filepath.Join(dir, "source.go"), "package p\n")
	write(t, filepath.Join(dir, "source_test.go"), "package p\nfunc Before() {}\n")
	shared, err := prog.LoadDirWithTests(dir, ".")
	require.NoError(t, err)
	write(t, filepath.Join(dir, "source_test.go"), "package p\nfunc After() {}\n")
	cached, err := shared.PackagesWithTests()
	require.NoError(t, err)
	assert.NotNil(t, packageContaining(cached, "source_test.go").Types.Scope().Lookup("Before"))
	assert.Nil(t, packageContaining(cached, "source_test.go").Types.Scope().Lookup("After"))
	fresh, err := prog.LoadDirWithTests(dir, ".")
	require.NoError(t, err)
	updated, err := fresh.PackagesWithTests()
	require.NoError(t, err)
	assert.NotNil(t, packageContaining(updated, "source_test.go").Types.Scope().Lookup("After"))
}

type packageView struct {
	ID, Path, Dir   string
	Files, Compiled []string
	IllTyped        bool
	Errors          []packages.Error
	Positions       []token.Position
}

func packageViews(pkgs []*packages.Package) []packageView {
	var views []packageView
	for _, pkg := range pkgs {
		v := packageView{ID: pkg.ID, Path: pkg.PkgPath, Dir: pkg.Dir, Files: pkg.GoFiles, Compiled: pkg.CompiledGoFiles, IllTyped: pkg.IllTyped, Errors: pkg.Errors}
		for _, file := range pkg.Syntax {
			v.Positions = append(v.Positions, pkg.Fset.PositionFor(file.Pos(), false))
		}
		views = append(views, v)
	}
	return views
}

func TestSharedLoadSubdirectoryAndBuildTags(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags=sharedfixture")
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":        "module fixture\n\ngo 1.26\n",
		"a/a.go":        "package a\nfunc A(){}\n",
		"b/b.go":        "package b\nfunc B(){}\n",
		"b/selected.go": "//go:build sharedfixture\n\npackage b\nfunc Selected(){}\n",
		"b/b_test.go":   "package b\nfunc Helper(){}\n",
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		write(t, path, src)
	}
	baseline, err := prog.LoadDir(dir, "./b")
	require.NoError(t, err)
	shared, err := prog.LoadDirWithTests(dir, "./b")
	require.NoError(t, err)
	require.Len(t, shared.Packages, 1)
	assert.NotNil(t, shared.Packages[0].Types.Scope().Lookup("Selected"))
	assert.Equal(t, packageViews(baseline.Packages), packageViews(shared.Packages))
	before, err := baseline.PackagesWithTests()
	require.NoError(t, err)
	after, err := shared.PackagesWithTests()
	require.NoError(t, err)
	assert.Equal(t, packageViews(before), packageViews(after))
}

func TestSharedLoadMissingPatterns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26\n")
	baseline, baselineErr := prog.LoadDir(dir, "./missing")
	shared, sharedErr := prog.LoadDirWithTests(dir, "./missing")
	if baselineErr != nil {
		require.Error(t, sharedErr)
		assert.Equal(t, baselineErr.Error(), sharedErr.Error())
	} else {
		require.NoError(t, sharedErr)
		assert.Equal(t, packageViews(baseline.Packages), packageViews(shared.Packages))
	}
}

func TestSharedLoadFallsBackOnTestMainCollision(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":         "module fixture\n\ngo 1.26\n",
		"p/p.go":         "package p\nfunc Work(){}\n",
		"p/p_test.go":    "package p\nfunc Helper(){}\n",
		"p.test/main.go": "package main\nfunc main(){}\n",
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		write(t, path, src)
	}
	baseline, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	shared, err := prog.LoadDirWithTests(dir, "./...")
	require.NoError(t, err)
	assert.Equal(t, packageViews(baseline.Packages), packageViews(shared.Packages))
	require.Len(t, shared.SSAPackages, len(baseline.SSAPackages))
	for i, pkg := range shared.SSAPackages {
		assert.Equal(t, baseline.SSAPackages[i].Pkg.Path(), pkg.Pkg.Path())
	}
	_, beforeErr := baseline.PackagesWithTests()
	_, afterErr := shared.PackagesWithTests()
	require.Error(t, beforeErr)
	require.Error(t, afterErr)
	assert.Equal(t, beforeErr.Error(), afterErr.Error())
}

func TestSharedLoadMixedMissingRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26\n")
	write(t, filepath.Join(dir, "p.go"), "package p\nfunc Work(){}\n")
	baseline, err := prog.LoadDir(dir, ".", "./missing")
	require.NoError(t, err)
	shared, err := prog.LoadDirWithTests(dir, ".", "./missing")
	require.NoError(t, err)
	assert.Equal(t, packageViews(baseline.Packages), packageViews(shared.Packages))
	assert.Equal(t, baseline.HasErrors(), shared.HasErrors())
}

func TestSharedLoadDriverRejectsDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell driver fixture")
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26\n")
	write(t, filepath.Join(dir, "p.go"), "package p\nfunc Work(){}\n")
	driver := filepath.Join(dir, "driver.sh")
	require.NoError(t, os.WriteFile(driver, []byte(`#!/bin/sh
request=$(cat)
case "$request" in
  *'"mode":3,'*) echo 'metadata unsupported' >&2; exit 1;;
esac
printf '{"NotHandled":true}'
`), 0o700))
	t.Setenv("GOPACKAGESDRIVER", driver)
	baseline, err := prog.LoadDir(dir, ".")
	require.NoError(t, err)
	shared, err := prog.LoadDirWithTests(dir, ".")
	require.NoError(t, err)
	assert.Equal(t, packageViews(baseline.Packages), packageViews(shared.Packages))
}

func TestSharedLoadDriverWithoutDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell driver fixture")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "p.go")
	write(t, source, "package p\nfunc Work(){}\n")
	response, err := json.Marshal(packages.DriverResponse{
		Compiler: "gc", Arch: runtime.GOARCH, GoVersion: 26, Roots: []string{"custom-id"},
		Packages: []*packages.Package{{ID: "custom-id", Name: "p", PkgPath: "fixture", GoFiles: []string{source}, CompiledGoFiles: []string{source}}},
	})
	require.NoError(t, err)
	responsePath := filepath.Join(dir, "response.json")
	require.NoError(t, os.WriteFile(responsePath, response, 0o600))
	driver := filepath.Join(dir, "driver.sh")
	require.NoError(t, os.WriteFile(driver, []byte("#!/bin/sh\ncat >/dev/null\ncat \"$DRIVER_RESPONSE\"\n"), 0o700))
	t.Setenv("DRIVER_RESPONSE", responsePath)
	t.Setenv("GOPACKAGESDRIVER", driver)
	baseline, err := prog.LoadDir(dir, ".")
	require.NoError(t, err)
	shared, err := prog.LoadDirWithTests(dir, ".")
	require.NoError(t, err)
	assert.Equal(t, packageViews(baseline.Packages), packageViews(shared.Packages))
	require.Len(t, shared.Packages, 1)
	assert.Empty(t, shared.Packages[0].Dir)
	assert.NotNil(t, shared.Packages[0].Types.Scope().Lookup("Work"))
}
