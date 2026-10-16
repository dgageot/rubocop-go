package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
)

func TestSharedParenthesizedPolicyPatterns(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    cop.Cop
		src  string
		want int
	}{
		{"context parameter", NewLintContextFirstParameter(), `func f(n int, ctx (context.Context)) {}`, 1},
		{"context already first", NewLintContextFirstParameter(), `func f(ctx (context.Context), n int, other context.Context) {}`, 0},
		{"context field", NewLintNoContextField(), `type S struct { ctx (context.Context) }`, 1},
		{"parenthesized struct", NewLintNoContextField(), `type S (struct { ctx context.Context })`, 1},
		{"request", NewLintHTTPRequestWithContext(), `func f() { (http.NewRequest)("GET", "/", nil) }`, 1},
		{"fatal", NewLintNoFatalOutsideMain(), `func f() { (log.Fatal)("failure") }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, tc.c, "package p\n"+tc.src), tc.want)
		})
	}
}

func TestSharedPhysicalTestFileExemption(t *testing.T) {
	c := NewLintNoFatalOutsideMain()
	assert.Len(t, coptest.RunNamed(t, c, "production.go", "//line virtual_test.go:1\npackage p\nfunc f() { log.Fatal(\"failure\") }"), 1)
	assert.Empty(t, coptest.RunNamed(t, c, "production_test.go", "//line virtual.go:1\npackage p\nfunc f() { log.Fatal(\"failure\") }"))
}

func TestSharedCalleeObjectParentheses(t *testing.T) {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "p.go", `package p
	import "strings"
	func f() { _ = (strings.Fields)("a b"); _ = (len)("x") }
	`, 0)
	require.NoError(t, err)
	info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
	_, err = (&types.Config{Importer: importer.Default()}).Check("p", fs, []*ast.File{file}, info)
	require.NoError(t, err)
	var names []string
	cop.ForEachCallIn(file, func(call *ast.CallExpr) {
		obj := calleeObject(info, call)
		require.NotNil(t, obj)
		names = append(names, obj.Name())
	})
	assert.Equal(t, []string{"Fields", "len"}, names)
}

func TestSharedOldTaggedModuleAPIGates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.20\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p.go"), []byte(`//go:build go1.20

package p
func clone(src map[string]int) map[string]int {
 if src == nil { return nil }
 dst := make(map[string]int, len(src))
 for k, v := range src { dst[k] = v }
 return dst
}
func copyMap(dst, src map[string]int) { for k, v := range src { dst[k] = v } }
func contains(xs []int, needle int) bool { for _, x := range xs { if x == needle { return true } }; return false }
func equal(a, b []int) bool { if len(a) != len(b) { return false }; for i, v := range a { if v != b[i] { return false } }; return true }
func cloneSlice(xs []int) []int { return append([]int(nil), xs...) }
`), 0o600))
	program, err := prog.LoadDir(dir, ".")
	require.NoError(t, err)
	require.False(t, program.HasErrors())
	for _, c := range []prog.Cop{NewLintMapsClone(), NewLintMapsCopy(), NewLintSlicesContains(), NewLintSlicesEqual(), NewLintSlicesClone()} {
		t.Run(c.Name(), func(t *testing.T) {
			pass := &prog.Pass{Cop: c, Program: program}
			c.Check(pass)
			assert.Empty(t, pass.Offenses(), "Go 1.21 APIs are unavailable in a Go 1.20 target")
		})
	}
}

func TestProgramPoliciesUsePhysicalFilenames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		c        prog.Cop
		filename string
		source   string
	}{
		{"benchmark", NewLintBenchmarkLoop(), "bench_test.go", "//line generated.go:1\n" + benchmarkLoopFixture},
		{"stream", NewLintStreamCloseSafety(), "stream.go", `//line generated_test.go:1
package p
type stream struct { done bool }
func (s *stream) Next() bool { return s.done }
func (s *stream) Close() { s.done = true }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offenses := coptest.RunProgram(t, tc.c, coptest.ProgramFiles{
				"go.mod":    "module fixture\n\ngo 1.26\n",
				"p.go":      "package p\n",
				tc.filename: tc.source,
			})
			require.Len(t, offenses, 1)
		})
	}
}
