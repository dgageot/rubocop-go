package prog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/prog"
)

func TestLoadSkipsIllTypedSSABodies(t *testing.T) {
	for name, load := range map[string]func(string, ...string) (*prog.Program, error){
		"production": prog.LoadDir,
		"shared":     prog.LoadDirWithTests,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for name, src := range map[string]string{
				"go.mod":                 "module example.test\n\ngo 1.26\n",
				"good/good.go":           "package good\nfunc Work() int { return 42 }\n",
				"broken/broken.go":       "package broken\nfunc Work() { x, err := missing(); _, _ = x, err }\n",
				"dependent/dependent.go": "package dependent\nimport \"example.test/broken\"\nfunc Work() { broken.Work() }\n",
			} {
				path := filepath.Join(dir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				write(t, path, src)
			}
			p, err := load(dir, "./...")
			require.NoError(t, err)
			require.True(t, p.HasErrors())
			require.Len(t, p.Packages, 3)
			require.Len(t, p.SSAPackages, 3)
			for i, pkg := range p.Packages {
				require.NotEmpty(t, pkg.Syntax, pkg.PkgPath)
				require.NotNil(t, pkg.TypesInfo, pkg.PkgPath)
				if pkg.PkgPath == "example.test/good" {
					require.NotNil(t, p.SSAPackages[i])
					fn := p.SSAPackages[i].Func("Work")
					assert.NotEmpty(t, fn.Blocks)
					assert.NotNil(t, p.CallGraph.Nodes[fn])
				} else {
					assert.True(t, pkg.IllTyped)
					assert.Nil(t, p.SSAPackages[i])
				}
			}
			for _, fn := range p.AllFunctions() {
				assert.Equal(t, "example.test/good", fn.Pkg.Pkg.Path())
			}
		})
	}
}

func TestLoadEntirelyIllTypedProgram(t *testing.T) {
	p := loadProgram(t, "package main\nfunc main() { x, err := missing(); _, _ = x, err }\n")
	assert.True(t, p.HasErrors())
	require.Len(t, p.SSAPackages, 1)
	assert.Nil(t, p.SSAPackages[0])
	assert.Empty(t, p.AllFunctions())
}
