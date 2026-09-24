package runner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
)

func TestModuleGoVersion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		dir   string
		want  string
	}{
		{name: "missing", dir: "pkg/deep"},
		{name: "module", files: map[string]string{"go.mod": "module example.test\ngo 1.26\n"}, want: "go1.26"},
		{name: "ancestor", files: map[string]string{"go.mod": "module example.test\ngo 1.26\n"}, dir: "pkg/deep", want: "go1.26"},
		{name: "patch", files: map[string]string{"go.mod": "module example.test\ngo 1.26.2\n"}, want: "go1.26.2"},
		{
			name: "nested module", dir: "nested/pkg", want: "go1.25",
			files: map[string]string{
				"go.mod":        "module example.test\ngo 1.26\n",
				"nested/go.mod": "module nested.test\ngo 1.25\n",
			},
		},
		{
			name: "malformed nested module", dir: "nested/pkg",
			files: map[string]string{
				"go.mod":        "module example.test\ngo 1.26\n",
				"nested/go.mod": "module nested.test\ngo 1.26\nrequire (\n",
			},
		},
		{
			name: "nested module without go directive", dir: "nested/pkg",
			files: map[string]string{
				"go.mod":        "module example.test\ngo 1.26\n",
				"nested/go.mod": "module nested.test\n",
			},
		},
		{name: "no go directive", files: map[string]string{"go.mod": "module example.test\n"}},
		{name: "toolchain is not target", files: map[string]string{"go.mod": "module example.test\ntoolchain go1.26.2\n"}},
		{name: "invalid go directive", files: map[string]string{"go.mod": "module example.test\ngo invalid\n"}},
		{name: "duplicate go directive", files: map[string]string{"go.mod": "module example.test\ngo 1.25\ngo 1.26\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeVersionFiles(t, root, tc.files)
			dir := filepath.Join(root, tc.dir)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			assert.Equal(t, tc.want, moduleGoVersion(dir))
		})
	}
}

func TestModuleGoVersionRelativePath(t *testing.T) {
	root := t.TempDir()
	writeVersionFiles(t, root, map[string]string{
		"go.mod":   "module example.test\ngo 1.26.1\n",
		"pkg/p.go": "package p\n",
	})
	t.Chdir(root)
	assert.Equal(t, "go1.26.1", moduleGoVersion("pkg"))
}

func TestModuleGoVersionUnreadableNestedModule(t *testing.T) {
	root := t.TempDir()
	writeVersionFiles(t, root, map[string]string{"go.mod": "module example.test\ngo 1.26\n"})
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested", "go.mod"), 0o755))
	assert.Empty(t, moduleGoVersion(filepath.Join(root, "nested")))
}

func TestTypeCheckGoVersion(t *testing.T) {
	for _, tc := range []struct {
		name       string
		module     string
		constraint string
		pkgVersion string
		want       string
	}{
		{name: "unknown"},
		{name: "malformed", module: "module example.test\ngo invalid\n"},
		{name: "package", module: "module example.test\ngo 1.26.1\n", pkgVersion: "go1.26.1", want: "go1.26.1"},
		{name: "newer file", module: "module example.test\ngo 1.25\n", constraint: "go1.26", pkgVersion: "go1.25", want: "go1.26"},
		{name: "older file", module: "module example.test\ngo 1.26\n", constraint: "go1.25", pkgVersion: "go1.26", want: "go1.25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.module != "" {
				writeVersionFiles(t, dir, map[string]string{"go.mod": tc.module})
			}
			src := "package p\n"
			if tc.constraint != "" {
				src = "//go:build " + tc.constraint + "\n\n" + src
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(dir, "p.go"), src, parser.ParseComments|parser.SkipObjectResolution)
			require.NoError(t, err)
			info, pkg := typeCheck(fset, dir, []*ast.File{file})
			require.NotNil(t, pkg)
			assert.Equal(t, tc.pkgVersion, pkg.GoVersion())
			v, ok := info.FileVersions[file]
			assert.True(t, ok)
			assert.Equal(t, tc.want, v)
			assert.Equal(t, tc.want, (&cop.Pass{File: file, Info: info, Package: pkg}).GoVersion())
		})
	}
}

func TestRunnerMinGoVersion(t *testing.T) {
	for _, tc := range []struct {
		name       string
		module     string
		constraint string
		want       int
	}{
		{name: "below", module: "module example.test\ngo 1.25\n"},
		{name: "equal", module: "module example.test\ngo 1.26\n", want: 1},
		{name: "newer", module: "module example.test\ngo 1.27\n", want: 1},
		{name: "patch", module: "module example.test\ngo 1.26.1\n", want: 1},
		{name: "unknown"},
		{name: "malformed", module: "module example.test\ngo invalid\n"},
		{name: "missing go directive", module: "module example.test\n"},
		{name: "newer file", module: "module example.test\ngo 1.25\n", constraint: "go1.26", want: 1},
		{name: "older file", module: "module example.test\ngo 1.26\n", constraint: "go1.25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package p\n"
			if tc.constraint != "" {
				src = "//go:build " + tc.constraint + "\n\n" + src
			}
			files := map[string]string{"p.go": src}
			if tc.module != "" {
				files["go.mod"] = tc.module
			}
			writeVersionFiles(t, dir, files)
			var gatedRuns, ungatedRuns int
			gated := cop.New(cop.Meta{Name: "Test/Gated"}, func(p *cop.Pass) {
				gatedRuns++
				p.Report(p.File, "gated")
			}, cop.WithMinGoVersion("go1.26"), cop.WithScope(cop.And()))
			ungated := cop.New(cop.Meta{Name: "Test/Ungated"}, func(p *cop.Pass) {
				ungatedRuns++
				p.Report(p.File, "ungated")
			})
			r := New([]cop.Cop{gated, ungated}, config.DefaultConfig(), io.Discard)
			count, err := r.Run([]string{dir})
			require.NoError(t, err)
			assert.Equal(t, tc.want, gatedRuns)
			assert.Equal(t, 1, ungatedRuns)
			assert.Equal(t, tc.want+1, count)
		})
	}
}

func TestRunnerMinGoVersionNestedModulesAndReload(t *testing.T) {
	dir := t.TempDir()
	writeVersionFiles(t, dir, map[string]string{
		"go.mod":          "module example.test\ngo 1.26\n",
		"p.go":            "package p\n",
		"nested/go.mod":   "module nested.test\ngo 1.25\n",
		"nested/pkg/p.go": "package p\n",
	})
	gated := cop.New(cop.Meta{Name: "Test/Gated"}, func(p *cop.Pass) {
		p.Report(p.File, p.GoVersion())
	}, cop.WithMinGoVersion("go1.26"))
	r := New([]cop.Cop{gated}, config.DefaultConfig(), io.Discard)
	count, err := r.Run([]string{dir})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	writeVersionFiles(t, dir, map[string]string{"nested/go.mod": "module nested.test\ngo 1.26\n"})
	count, err = r.Run([]string{dir})
	require.NoError(t, err)
	assert.Equal(t, 2, count, "module versions must not be cached across Run calls")

	gated.Scope = cop.Or()
	count, err = r.Run([]string{dir})
	require.NoError(t, err)
	assert.Zero(t, count, "the minimum version does not bypass Scope")
}

func writeVersionFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}
