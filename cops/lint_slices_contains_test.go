package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

const slicesContainsFixture = `package p
func has(xs []int, needle int) bool {
	for _, value := range xs { if value == needle { return true } }
	return false
}`

func TestSlicesContains(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"parameter", `func f(xs []int, n int) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"reversed comparison", `func f(xs []int, n int) bool { for _, v := range xs { if n == v { return true } }; return false }`, 1},
		{"parentheses", `func f(xs []int, n int) bool { for _, v := range (xs) { if ((v) == (n)) { return (true) } }; return (false) }`, 1},
		{"literal", `func f(xs []string) bool { for _, v := range xs { if v == "x" { return true } }; return false }`, 1},
		{"constant expression", `func f(xs []int) bool { for _, v := range xs { if v == 1+2 { return true } }; return false }`, 1},
		{"constant", `const n = 3; func f(xs []int) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"named slice and element", `type E int; type S []E; func f(xs S, n E) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"type aliases", `type E = int; type S = []E; func f(xs S, n E) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"import alias type", `import clock "time"; func f(xs []clock.Duration, n clock.Duration) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"import alias constant", `import clock "time"; func f(xs []clock.Duration) bool { for _, v := range xs { if v == clock.Hour { return true } }; return false }`, 1},
		{"generic elements", `func f[T comparable](xs []T, n T) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"interface comparisons", `func f(xs []any, n any) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"NaN semantics", `func f(xs []float64, n float64) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"nil pointer", `func f(xs []*int) bool { for _, v := range xs { if v == nil { return true } }; return false }`, 1},
		{"closure parameters", `var f = func(xs []int, n int) bool { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"named bool alias result", `type B = bool; func f(xs []int, n int) B { for _, v := range xs { if v == n { return true } }; return false }`, 1},
		{"shadowed value parameter", `func f(xs []int, v int) bool { for _, v := range xs { if v == v { return true } }; return false }`, 0},
		{"shadowed true", `func f(xs []int, n int, true bool) bool { for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"defaulted interface literal", `func f(xs []any) bool { for _, v := range xs { if v == 1 { return true } }; return false }`, 0},
		{"typed needle requires conversion", `func f(xs []any, n int) bool { for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"typed constant requires conversion", `const n int = 1; func f(xs []any) bool { for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"global needle", `var n int; func f(xs []int) bool { for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"global slice", `var xs []int; func f(n int) bool { for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"captured needle", `func f(xs []int, n int) func() bool { return func() bool { for _, v := range xs { if v == n { return true } }; return false } }`, 0},
		{"repeated call", `func f(xs []int, n func() int) bool { for _, v := range xs { if v == n() { return true } }; return false }`, 0},
		{"field needle", `func f(xs []int, n *struct{ value int }) bool { for _, v := range xs { if v == n.value { return true } }; return false }`, 0},
		{"index needle", `func f(xs, n []int) bool { for _, v := range xs { if v == n[0] { return true } }; return false }`, 0},
		{"dereference needle", `func f(xs []int, n *int) bool { for _, v := range xs { if v == *n { return true } }; return false }`, 0},
		{"conversion needle", `func f(xs []int, n int64) bool { for _, v := range xs { if v == int(n) { return true } }; return false }`, 0},
		{"array", `func f(xs [2]int, n int) bool { for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"string", `func f(xs string, n rune) bool { for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"noncomparable elements", `func f(xs [][]int) bool { for _, v := range xs { if v == nil { return true } }; return false }`, 0},
		{"index used", `func f(xs []int, n int) bool { for i, v := range xs { if v == n+i { return true } }; return false }`, 0},
		{"index-only loop", `func f(xs []int, n int) bool { for i := range xs { if xs[i] == n { return true } }; return false }`, 0},
		{"assignment range", `func f(xs []int, n int) bool { var v int; for _, v = range xs { if v == n { return true } }; return false }`, 0},
		{"extra effect", `func f(xs []int, n int) bool { for _, v := range xs { println(v); if v == n { return true } }; return false }`, 0},
		{"guard init", `func f(xs []int, n int) bool { for _, v := range xs { if n = v; v == n { return true } }; return false }`, 0},
		{"else behavior", `func f(xs []int, n int) bool { for _, v := range xs { if v == n { return true } else { return false } }; return false }`, 0},
		{"inverted predicate", `func f(xs []int, n int) bool { for _, v := range xs { if v != n { return true } }; return false }`, 0},
		{"wrong fallback", `func f(xs []int, n int) bool { for _, v := range xs { if v == n { return true } }; return true }`, 0},
		{"nil-sensitive", `func f(xs []int, n int) bool { if xs == nil { return true }; for _, v := range xs { if v == n { return true } }; return false }`, 0},
		{"named bool result", `type B bool; func f(xs []int, n int) B { for _, v := range xs { if v == n { return true } }; return false }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newSlicesContainsFile(), "package p\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/SlicesContains", offense.CopName)
				assert.Contains(t, offense.Message, "slices.Contains")
				assert.Greater(t, offense.End.Offset, offense.Pos.Offset)
			}
		})
	}
}

func TestSlicesContainsConfiguration(t *testing.T) {
	t.Parallel()
	fresh := newSlicesContainsFile()
	scoped := newSlicesContainsFile(cop.WithScope(cop.UnderDir("internal")))
	require.NotSame(t, fresh, scoped)
	assert.Nil(t, fresh.Scope)
	assert.Empty(t, runSliceModernizationVersion(t, scoped, slicesContainsFixture, "go1.26", ""))
	assert.True(t, fresh.Types)
	assert.True(t, fresh.NeedsTypes())
	assert.Empty(t, coptest.Run(t, fresh, slicesContainsFixture))
	assert.Empty(t, runExtractedTypedCop(t, fresh, "// Code generated by fixture; DO NOT EDIT.\n"+slicesContainsFixture))
	assert.False(t, scoped.InScope(&cop.Pass{FileSet: token.NewFileSet(), File: &ast.File{}}))
	fresh.Run(&cop.Pass{}) // Missing type information must not reach AST matching.
	for _, tc := range []struct {
		name, module string
		opts         []cop.FuncOption
		want         int
	}{
		{"old", "go1.20", nil, 0},
		{"minimum", "go1.21", nil, 1},
		{"unknown", "", nil, 0},
		{"cannot lower", "go1.20", []cop.FuncOption{cop.WithMinStdlibVersion("go1.18")}, 0},
		{"raised API", "go1.21", []cop.FuncOption{cop.WithMinStdlibVersion("go1.22")}, 0},
		{"raised language", "go1.21", []cop.FuncOption{cop.WithMinGoVersion("go1.22")}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, runSliceModernizationVersion(t, newSlicesContainsFile(tc.opts...), slicesContainsFixture, tc.module, ""), tc.want)
		})
	}
}

func runSliceModernizationVersion(t *testing.T, c *cop.Func, src, module, language string) []cop.Offense {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), FileVersions: make(map[*ast.File]string),
	}
	cfg := types.Config{Importer: importer.Default(), GoVersion: module}
	pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	if language != "" {
		info.FileVersions[file] = language
	}
	pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
	if c.InScope(pass) {
		c.Check(pass)
	}
	return pass.Offenses()
}
