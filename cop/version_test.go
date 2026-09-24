package cop_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
)

func TestPassGoVersion(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pkgVersion string
		constraint string
		want       string
	}{
		{name: "unknown"},
		{name: "package", pkgVersion: "go1.25", want: "go1.25"},
		{name: "patch", pkgVersion: "go1.26.1", want: "go1.26.1"},
		{name: "newer file", pkgVersion: "go1.25", constraint: "go1.26", want: "go1.26"},
		{name: "older file", pkgVersion: "go1.26", constraint: "go1.25", want: "go1.25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := passWithVersion(t, tc.pkgVersion, tc.constraint)
			assert.Equal(t, tc.want, p.GoVersion())
		})
	}

	p := passWithVersion(t, "go1.26", "")
	p.Info = nil
	assert.Equal(t, "go1.26", p.GoVersion(), "package fallback without type info")
	p.Info = &types.Info{}
	assert.Equal(t, "go1.26", p.GoVersion(), "package fallback without file versions")
	p.Info.FileVersions = map[*ast.File]string{p.File: ""}
	assert.Equal(t, "go1.26", p.GoVersion(), "package fallback with an unknown file version")
	p.Info.FileVersions = map[*ast.File]string{new(ast.File): "go1.25"}
	assert.Equal(t, "go1.26", p.GoVersion(), "unrelated file versions are ignored")
	p.Package = nil
	assert.Empty(t, p.GoVersion())
	assert.Empty(t, (&cop.Pass{}).GoVersion())
}

func TestPassStdlibVersion(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pkgVersion string
		constraint string
		want       string
	}{
		{name: "unknown"},
		{name: "package", pkgVersion: "go1.25", want: "go1.25"},
		{name: "patch", pkgVersion: "go1.26.1", want: "go1.26.1"},
		{name: "newer file", pkgVersion: "go1.25", constraint: "go1.26", want: "go1.26"},
		{name: "older file", pkgVersion: "go1.26", constraint: "go1.25", want: "go1.26"},
		{name: "package patch retained", pkgVersion: "go1.26.1", constraint: "go1.26", want: "go1.26.1"},
		{name: "file only", constraint: "go1.26", want: "go1.26"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, passWithVersion(t, tc.pkgVersion, tc.constraint).StdlibVersion())
		})
	}

	p := passWithVersion(t, "go1.26", "")
	p.Info = nil
	assert.Equal(t, "go1.26", p.StdlibVersion(), "package fallback without type info")
	p.Info = &types.Info{}
	assert.Equal(t, "go1.26", p.StdlibVersion(), "package fallback without file versions")
	p.Info.FileVersions = map[*ast.File]string{p.File: ""}
	assert.Equal(t, "go1.26", p.StdlibVersion(), "package fallback with an unknown file version")
	p.Info.FileVersions = map[*ast.File]string{new(ast.File): "go1.27"}
	assert.Equal(t, "go1.26", p.StdlibVersion(), "unrelated file versions are ignored")
	p.Info.FileVersions = map[*ast.File]string{p.File: "1.27"}
	assert.Equal(t, "go1.26", p.StdlibVersion(), "invalid file version does not hide the package version")
	p.Package = nil
	assert.Empty(t, p.StdlibVersion(), "invalid versions are unknown")
	p.Info.FileVersions[p.File] = "go1.27"
	assert.Equal(t, "go1.27", p.StdlibVersion(), "a file version does not require a package")
	assert.Empty(t, (&cop.Pass{}).StdlibVersion(), "unknown must not fall back to the running toolchain")
}

func TestFuncMinGoVersion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		min    string
		want   bool
	}{
		{name: "below", target: "go1.25", min: "go1.26"},
		{name: "equal", target: "go1.26", min: "go1.26", want: true},
		{name: "patch", target: "go1.26.1", min: "go1.26", want: true},
		{name: "newer", target: "go1.27", min: "go1.26", want: true},
		{name: "unknown", min: "go1.26"},
		{name: "invalid target", target: "1.26", min: "go1.26"},
		{name: "invalid minimum", target: "go1.26", min: "1.26"},
		{name: "patch minimum below", target: "go1.26.1", min: "go1.26.2"},
		{name: "patch minimum equal", target: "go1.26.2", min: "go1.26.2", want: true},
		{name: "no gate unknown", want: true},
		{name: "no gate older", target: "go1.25", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ran bool
			c := cop.New(cop.Meta{Name: "Test/Version"}, func(*cop.Pass) { ran = true }, cop.WithMinGoVersion(tc.min))
			assert.Equal(t, tc.min, c.MinGoVersion)
			assert.Equal(t, tc.min != "", c.NeedsTypes())
			file := new(ast.File)
			c.Check(&cop.Pass{File: file, Info: &types.Info{FileVersions: map[*ast.File]string{file: tc.target}}})
			assert.Equal(t, tc.want, ran)
		})
	}
}

func TestFuncMinGoVersionIndependentOfScope(t *testing.T) {
	for _, opts := range [][]cop.FuncOption{
		{cop.WithMinGoVersion("go1.26"), cop.WithScope(cop.And())},
		{cop.WithScope(cop.And()), cop.WithMinGoVersion("go1.26")},
		{cop.WithMinGoVersion("go1.26"), cop.WithScope(nil)},
	} {
		var ran bool
		c := cop.New(cop.Meta{Name: "Test/Version"}, func(*cop.Pass) { ran = true }, opts...)
		p := passWithVersion(t, "go1.25", "")
		assert.True(t, c.InScope(p))
		c.Check(p)
		assert.False(t, ran)
		c.Check(passWithVersion(t, "go1.25", "go1.26"))
		assert.True(t, ran)
		ran = false
		c.Check(passWithVersion(t, "go1.26", "go1.25"))
		assert.False(t, ran)
	}

	var ran bool
	c := &cop.Func{MinGoVersion: "go1.26", Run: func(*cop.Pass) { ran = true }}
	assert.True(t, c.NeedsTypes(), "struct literals also request target version information")
	c.Check(&cop.Pass{})
	assert.False(t, ran)
	assert.True(t, cop.New(cop.Meta{}, nil, cop.WithTypes()).NeedsTypes())
}

func TestFuncMinStdlibVersion(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pkgVersion string
		constraint string
		minGo      string
		minStdlib  string
		want       bool
	}{
		{name: "below", pkgVersion: "go1.25", minStdlib: "go1.26"},
		{name: "equal", pkgVersion: "go1.26", minStdlib: "go1.26", want: true},
		{name: "newer", pkgVersion: "go1.26", minStdlib: "go1.25", want: true},
		{name: "patch", pkgVersion: "go1.26.1", minStdlib: "go1.26", want: true},
		{name: "patch below", pkgVersion: "go1.26.1", minStdlib: "go1.26.2"},
		{name: "patch equal", pkgVersion: "go1.26.2", minStdlib: "go1.26.2", want: true},
		{name: "unknown", minStdlib: "go1.26"},
		{name: "invalid minimum", pkgVersion: "go1.26", minStdlib: "1.26"},
		{name: "no gate unknown", want: true},
		{name: "no gate older", pkgVersion: "go1.25", want: true},
		{name: "newer file", pkgVersion: "go1.25", constraint: "go1.26", minStdlib: "go1.26", want: true},
		{name: "older file", pkgVersion: "go1.26", constraint: "go1.25", minStdlib: "go1.26", want: true},
		{name: "file only", constraint: "go1.26", minStdlib: "go1.26", want: true},
		{name: "both equal", pkgVersion: "go1.26", constraint: "go1.23", minGo: "go1.23", minStdlib: "go1.26", want: true},
		{name: "language too old", pkgVersion: "go1.26", constraint: "go1.22", minGo: "go1.23", minStdlib: "go1.26"},
		{name: "stdlib too old", pkgVersion: "go1.25", constraint: "go1.23", minGo: "go1.23", minStdlib: "go1.26"},
		{name: "both unknown", minGo: "go1.23", minStdlib: "go1.26"},
		{name: "invalid language minimum", pkgVersion: "go1.26", minGo: "1.23", minStdlib: "go1.26"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ran bool
			c := cop.New(cop.Meta{Name: "Test/Version"}, func(*cop.Pass) { ran = true },
				cop.WithMinGoVersion(tc.minGo), cop.WithMinStdlibVersion(tc.minStdlib), cop.WithScope(cop.And()))
			assert.Equal(t, tc.minGo, c.MinGoVersion)
			assert.Equal(t, tc.minStdlib, c.MinStdlibVersion)
			assert.Equal(t, tc.minGo != "" || tc.minStdlib != "", c.NeedsTypes())
			p := passWithVersion(t, tc.pkgVersion, tc.constraint)
			assert.True(t, c.InScope(p))
			c.Check(p)
			assert.Equal(t, tc.want, ran)
		})
	}

	var ran bool
	c := &cop.Func{MinStdlibVersion: "go1.26", Run: func(*cop.Pass) { ran = true }}
	assert.True(t, c.NeedsTypes(), "struct literals also request target version information")
	file := new(ast.File)
	c.Check(&cop.Pass{File: file, Info: &types.Info{FileVersions: map[*ast.File]string{file: "1.26"}}})
	assert.False(t, ran, "invalid targets must not run")
	c.Check(passWithVersion(t, "go1.26", "go1.25"))
	assert.True(t, ran, "struct literals enforce only the requested requirement")
}

func passWithVersion(t *testing.T, pkgVersion, constraint string) *cop.Pass {
	t.Helper()
	src := "package p\n"
	if constraint != "" {
		src = "//go:build " + constraint + "\n\n" + src
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments|parser.SkipObjectResolution)
	require.NoError(t, err)
	info := &types.Info{FileVersions: make(map[*ast.File]string)}
	cfg := &types.Config{GoVersion: pkgVersion}
	pkg, err := cfg.Check("p", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	return &cop.Pass{FileSet: fset, File: file, Info: info, Package: pkg}
}
