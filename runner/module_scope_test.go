package runner_test

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
)

// moduleScopeProbe records the module-relative path of each file it checks.
func moduleScopeProbe(visited *[]string) *cop.Func {
	return cop.New(cop.Meta{Name: "Test/ModuleScope"}, func(p *cop.Pass) {
		rel, _ := p.ModuleRelPath()
		*visited = append(*visited, rel)
	}, cop.WithScope(cop.UnderModuleDir("lib")))
}

// TestRunner_ModuleScope checks that a module-anchored scope selects the same
// files whatever the working directory and the spelling of the paths.
func TestRunner_ModuleScope(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	writeModule(t, root, map[string]string{
		"go.mod":              "module example.test\n\ngo 1.22\n",
		"lib/a.go":            "package lib\n",
		"tests/lib/b.go":      "package lib\n",
		"tools/lint/go.mod":   "module example.test/tools/lint\n\ngo 1.22\n",
		"tools/lint/lib/c.go": "package lib\n",
	})

	// The nested module anchors its own lib directory.
	whole := []string{"lib/a.go", "lib/c.go"}
	for _, tc := range []struct {
		name, dir, path string
		want            []string
	}{
		{"module root", root, ".", whole},
		{"inside the directory", filepath.Join(root, "lib"), ".", []string{"lib/a.go"}},
		{"sibling directory", filepath.Join(root, "tests"), "..", whole},
		{"absolute path", elsewhere, root, whole},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.dir)

			var visited []string
			r := runner.New([]cop.Cop{moduleScopeProbe(&visited)}, config.DefaultConfig(), io.Discard)
			_, err := r.Run([]string{tc.path})
			require.NoError(t, err)
			assert.Equal(t, tc.want, visited)
		})
	}
}

// TestRunner_ModuleScope_ProgramAdapter checks the scope on the absolute
// filenames of a loaded program.
func TestRunner_ModuleScope_ProgramAdapter(t *testing.T) {
	var visited []string
	coptest.RunProgram(t, prog.FromFile(moduleScopeProbe(&visited)), coptest.ProgramFiles{
		"lib/a.go":       "package lib\n",
		"tests/lib/b.go": "package lib\n",
	})

	assert.Equal(t, []string{"lib/a.go"}, visited)
}
