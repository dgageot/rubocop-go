package cop_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

// passNamed builds a pass without [coptest.RunNamed], which states the
// module-relative path of a relative filename itself.
func passNamed(t *testing.T, filename string) *cop.Pass {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, "package x", parser.PackageClauseOnly)
	require.NoError(t, err)
	return &cop.Pass{FileSet: fset, File: file}
}

func TestModuleRelPath(t *testing.T) {
	root, loose := t.TempDir(), t.TempDir()
	for _, dir := range []string{root, filepath.Join(root, "tools", "lint")} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test\n"), 0o644))
	}
	// A directory named go.mod is not a module root.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "decoy", "go.mod"), 0o755))
	t.Chdir(filepath.Join(root, "pkg"))

	for _, tc := range []struct {
		name, filename, want string
		ok                   bool
	}{
		{"module root", filepath.Join(root, "main.go"), "main.go", true},
		{"subdirectory", filepath.Join(root, "pkg", "tui", "view.go"), "pkg/tui/view.go", true},
		{"relative to the working directory", filepath.Join("tui", "view.go"), "pkg/tui/view.go", true},
		{"above the working directory", filepath.Join("..", "main.go"), "main.go", true},
		{"nested module", filepath.Join(root, "tools", "lint", "cmd", "main.go"), "cmd/main.go", true},
		{"directory named go.mod", filepath.Join(root, "pkg", "decoy", "view.go"), "pkg/decoy/view.go", true},
		{"outside a module", filepath.Join(loose, "main.go"), "", false},
		{"no filename", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := passNamed(t, tc.filename).ModuleRelPath()
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

// A module created after a lookup is seen by the next one: nothing is cached.
func TestModuleRelPathSeesNewModule(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "tools", "lint")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o644))
	p := passNamed(t, filepath.Join(nested, "main.go"))

	got, ok := p.ModuleRelPath()
	require.True(t, ok)
	assert.Equal(t, "tools/lint/main.go", got)

	require.NoError(t, os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.test/tools/lint\n"), 0o644))
	got, ok = p.ModuleRelPath()
	require.True(t, ok)
	assert.Equal(t, "main.go", got)
}

func TestSetModuleRelPath(t *testing.T) {
	p := passNamed(t, filepath.Join(t.TempDir(), "elsewhere.go"))
	p.SetModuleRelPath("./pkg//tui/view.go")

	got, ok := p.ModuleRelPath()
	assert.True(t, ok)
	assert.Equal(t, "pkg/tui/view.go", got)

	views, err := p.TypedFiles()
	require.NoError(t, err)
	require.Len(t, views, 1)
	got, ok = views[0].ModuleRelPath()
	assert.True(t, ok)
	assert.Equal(t, "pkg/tui/view.go", got)
}

func TestModuleScopes(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "pkg", "tui", "view.go")

	for _, tc := range []struct {
		name     string
		scope    cop.CheckScope
		filename string
		want     bool
	}{
		{"directory", cop.UnderModuleDir("pkg/tui"), "pkg/tui/view.go", true},
		{"subdirectory", cop.UnderModuleDir("pkg/tui"), "pkg/tui/sub/view.go", true},
		{"trailing slash", cop.UnderModuleDir("pkg/tui/"), "pkg/tui/view.go", true},
		{"leading dot", cop.UnderModuleDir("./pkg/tui"), "pkg/tui/view.go", true},
		{"dot segments", cop.UnderModuleDir("pkg/x/../tui"), "pkg/tui/view.go", true},
		{"parent directory", cop.UnderModuleDir("../pkg/tui"), "pkg/tui/view.go", false},
		{"rooted directory", cop.UnderModuleDir("/pkg/tui"), "pkg/tui/view.go", false},
		{"same path deeper in the tree", cop.UnderModuleDir("pkg/tui"), "vendor/x/pkg/tui/view.go", false},
		{"unanchored directory matches deeper", cop.UnderDir("pkg/tui"), "vendor/x/pkg/tui/view.go", true},
		{"directory name prefix", cop.UnderModuleDir("pkg/tui"), "pkg/tuiextra/view.go", false},
		{"file named like the directory", cop.UnderModuleDir("pkg/tui"), "pkg/tui.go", false},
		{"directory outside a module", cop.UnderModuleDir("pkg/tui"), outside, false},
		{"whole module, root file", cop.UnderModuleDir("."), "main.go", true},
		{"whole module, nested file", cop.UnderModuleDir("."), "pkg/tui/view.go", true},
		{"whole module, empty directory", cop.UnderModuleDir(""), "pkg/tui/view.go", true},
		{"whole module, outside a module", cop.UnderModuleDir("."), outside, false},
		{"file", cop.OnlyModuleFile("main.go"), "main.go", true},
		{"file, leading dot", cop.OnlyModuleFile("./pkg/tui/view.go"), "pkg/tui/view.go", true},
		{"same file deeper in the tree", cop.OnlyModuleFile("main.go"), "cmd/main.go", false},
		{"unanchored file matches deeper", cop.OnlyFile("main.go"), "cmd/main.go", true},
		{"file outside a module", cop.OnlyModuleFile("pkg/tui/view.go"), outside, false},
		{"whole module is not a file", cop.OnlyModuleFile("."), "main.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			probe := cop.New(cop.Meta{Name: "Test/Scope"}, func(*cop.Pass) { ran = true }, cop.WithScope(tc.scope))
			coptest.RunNamed(t, probe, tc.filename, "package x")
			assert.Equal(t, tc.want, ran)
		})
	}
}
