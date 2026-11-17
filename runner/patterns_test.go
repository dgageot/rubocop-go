package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackagePattern(t *testing.T) {
	abs := t.TempDir()
	root := filepath.VolumeName(abs) + string(filepath.Separator)

	for _, tc := range []struct {
		path      string
		recursive bool
		want      string
	}{
		{path: ".", recursive: true, want: "./..."},
		{path: ".", want: "."},
		{path: "pkg", recursive: true, want: "./pkg/..."},
		{path: "./pkg", recursive: true, want: "./pkg/..."},
		{path: "pkg/", recursive: true, want: "./pkg/..."},
		{path: "pkg/sub", recursive: true, want: "./pkg/sub/..."},
		{path: "pkg", want: "./pkg"},
		{path: "../other", recursive: true, want: "../other/..."},
		{path: "../other", want: "../other"},
		{path: abs, recursive: true, want: filepath.ToSlash(abs) + "/..."},
		{path: abs, want: filepath.ToSlash(abs)},
		{path: root, recursive: true, want: filepath.ToSlash(root) + "..."},
	} {
		assert.Equal(t, tc.want, packagePattern(tc.path, tc.recursive), "path %q, recursive %t", tc.path, tc.recursive)
	}
}

func TestLoadPatterns(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	file := filepath.Join("cmd", "tool", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte("package main\n"), 0o644))

	assert.Equal(t, []string{"./..."}, loadPatterns(nil))
	assert.Equal(t, []string{"./cmd/...", "./cmd/tool"}, loadPatterns([]string{"cmd", "./cmd", file}))
}
