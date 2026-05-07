package cop_test

import (
	"go/ast"
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

func TestParseSibling(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.go"), []byte("package x\nconst Other = \"hi\"\n"), 0o644))

	var got string
	probe := newProbe(func(p *cop.Pass) {
		f, err := p.ParseSibling("other.go")
		require.NoError(t, err)
		consts := cop.StringConstsIn(f, nil)
		got = consts["Other"]
	})
	coptest.RunNamed(t, probe, filepath.Join(dir, "main.go"), "package x")

	assert.Equal(t, "hi", got)
}

func TestParseDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\nconst A = \"a\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package x\nconst B = \"b\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.go"), []byte("package x\nconst Skip = \"skip\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c_test.go"), []byte("package x\nconst CT = \"ct\"\n"), 0o644))

	var got map[string]string
	probe := newProbe(func(p *cop.Pass) {
		files, err := p.ParseDir(".", cop.ParseDirOptions{
			SkipTests: true,
			SkipFiles: []string{"skip.go"},
		})
		require.NoError(t, err)
		got = map[string]string{}
		for _, f := range files {
			for k, v := range cop.StringConstsIn(f, nil) {
				got[k] = v
			}
		}
	})
	coptest.RunNamed(t, probe, filepath.Join(dir, "skip.go"), "package x")

	assert.Equal(t, map[string]string{"A": "a", "B": "b"}, got)
}

func TestSuppressions(t *testing.T) {
	src := `package x

import "fmt"

func main() {
	fmt.Println("loud") //rubocop:disable Lint/FmtPrint
	//rubocop:disable Lint/FmtPrint
	fmt.Println("also loud")
	fmt.Println("flagged")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	require.NoError(t, err)

	sup := cop.ScanSuppressions(fset, file)

	// Find the three Println calls and assert which lines are suppressed.
	var prints []*ast.CallExpr
	cop.ForEachCallIn(file, func(call *ast.CallExpr) {
		if cop.IsCallTo(call, "fmt", "Println") {
			prints = append(prints, call)
		}
	})
	require.Len(t, prints, 3)

	line := func(n ast.Node) int { return fset.Position(n.Pos()).Line }

	assert.True(t, sup.Suppresses("Lint/FmtPrint", line(prints[0])), "inline directive should suppress same line")
	assert.True(t, sup.Suppresses("Lint/FmtPrint", line(prints[1])), "full-line directive should suppress next line")
	assert.False(t, sup.Suppresses("Lint/FmtPrint", line(prints[2])), "third call is not suppressed")
	assert.False(t, sup.Suppresses("Lint/Other", line(prints[0])), "different cop must not be suppressed")
}

func TestSuppressionsMultipleNames(t *testing.T) {
	src := `package x

import "fmt"

func main() {
	fmt.Println("x") //rubocop:disable Lint/A, Lint/B
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	require.NoError(t, err)

	sup := cop.ScanSuppressions(fset, file)
	assert.True(t, sup.Suppresses("Lint/A", 6))
	assert.True(t, sup.Suppresses("Lint/B", 6))
	assert.False(t, sup.Suppresses("Lint/C", 6))
}

func TestSuppressionsFile(t *testing.T) {
	src := `//rubocop:disable-file Lint/Foo
package x

func main() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	require.NoError(t, err)

	sup := cop.ScanSuppressions(fset, file)
	assert.True(t, sup.Suppresses("Lint/Foo", 1))
	assert.True(t, sup.Suppresses("Lint/Foo", 99), "file-level directive applies to every line")
	assert.False(t, sup.Suppresses("Lint/Bar", 1), "unrelated cop is not suppressed")
}
