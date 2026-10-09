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
)

const mapsCloneFixture = `package p
func clone(src map[string]int) map[string]int {
 if src == nil { return nil }
 dst := make(map[string]int, len(src))
 for k, v := range src { dst[k] = v }
 return dst
}`

func TestMapsClone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"len hint", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 1},
		{"no hint", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int); for k, v := range src { dst[k] = v }; return dst }`, 1},
		{"named map", `type M map[string]int; func f(src M) M { if src == nil { return nil }; dst := make(M, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 1},
		{"alias map", `type M = map[string]int; func f(src M) M { if src == nil { return nil }; dst := make(M, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 1},
		{"parentheses and reversed nil", `func f(src map[string]int) map[string]int { if (nil) == (src) { return (nil) }; dst := (make)(map[string]int, (len)((src))); for k, v := range (src) { (dst)[(k)] = (v) }; return (dst) }`, 1},
		{"literal helper", `var f = func(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 1},
		{"struct and array keys", `type K struct { a [2]string; p *float64; c chan int }; func f(src map[K]any) map[K]any { if src == nil { return nil }; dst := make(map[K]any, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 1},
		{"generic values", `func f[V any](src map[string]V) map[string]V { if src == nil { return nil }; dst := make(map[string]V, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 1},
		{"no nil guard", `func f(src map[string]int) map[string]int { dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"return source when nil", `func f(src map[string]int) map[string]int { if src == nil { return src }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"allocate before guard", `func f(src map[string]int) map[string]int { dst := make(map[string]int, len(src)); if src == nil { return nil }; for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"empty literal", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := map[string]int{}; for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"intentional capacity", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)+10); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"zero capacity hint", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, 0); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"empty map has non-nil result", `func f(src map[string]int) map[string]int { if len(src) == 0 { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"different returned map", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return src }`, 0},
		{"assigned range variables", `var k string; var v int; func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v = range src { dst[k] = v }; return dst }`, 0},
		{"float keys", `func f(src map[float64]int) map[float64]int { if src == nil { return nil }; dst := make(map[float64]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"complex keys", `func f(src map[complex128]int) map[complex128]int { if src == nil { return nil }; dst := make(map[complex128]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"interface keys", `func f(src map[any]int) map[any]int { if src == nil { return nil }; dst := make(map[any]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"nested NaN keys", `type K struct { a [1]float64 }; func f(src map[K]int) map[K]int { if src == nil { return nil }; dst := make(map[K]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"generic keys", `func f[K comparable](src map[K]int) map[K]int { if src == nil { return nil }; dst := make(map[K]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"named type lost", `type M map[string]int; func f(src M) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"destination type differs", `type M map[string]int; func f(src M) M { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"shadowed len", `func len(map[string]int) int { return 2 }; func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"shadowed make", `func make(int, int) map[string]int { return map[string]int{} }; func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(0, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"shadowed nil", `var nil map[string]int; func f(src map[string]int) map[string]int { if len(src) == 0 { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"extra work", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v; println(k) }; return dst }`, 0},
		{"deep copy", `func f(src map[string][]int) map[string][]int { if src == nil { return nil }; dst := make(map[string][]int, len(src)); for k, v := range src { dst[k] = append([]int(nil), v...) }; return dst }`, 0},
		{"filter", `func f(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { if v > 0 { dst[k] = v } }; return dst }`, 0},
		{"guard init", `func f(src map[string]int) map[string]int { if _ = src; src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"deferred work", `func f(src map[string]int) map[string]int { defer println(1); if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
		{"method", `type M map[string]int; func (src M) clone() M { if src == nil { return nil }; dst := make(M, len(src)); for k, v := range src { dst[k] = v }; return dst }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newMapsCloneFile(), "package p\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/MapsClone", offense.CopName)
				assert.Equal(t, cop.Convention, offense.Severity)
				assert.Contains(t, offense.Message, "maps.Clone(src)")
				assert.Contains(t, offense.Message, "named type")
				assert.Equal(t, 2, offense.Pos.Line)
				assert.Greater(t, offense.End.Offset, offense.Pos.Offset)
			}
		})
	}
}

func TestMapsCloneConfiguration(t *testing.T) {
	t.Parallel()
	c := newMapsCloneFile()
	require.NotSame(t, c, newMapsCloneFile())
	assert.True(t, c.Types)
	assert.True(t, c.NeedsTypes())
	assert.Nil(t, c.Scope)
	assert.Equal(t, "go1.21", c.MinStdlibVersion)
	assert.Equal(t, "go1.21", newMapsCloneFile(cop.WithMinStdlibVersion("go1.20")).MinStdlibVersion)
	assert.Equal(t, "go1.30", newMapsCloneFile(cop.WithMinStdlibVersion("go1.30")).MinStdlibVersion)
	assert.Empty(t, runExtractedTypedCop(t, newMapsCloneFile(cop.WithMinStdlibVersion("go1.30")), mapsCloneFixture))
	assert.Empty(t, runExtractedTypedCop(t, c, "// Code generated by fixture; DO NOT EDIT.\n"+mapsCloneFixture))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "internal/sample_test.go", mapsCloneFixture, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), FileVersions: make(map[*ast.File]string),
	}
	cfg := types.Config{Importer: importer.Default()}
	_, err = cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	for _, target := range []string{"", "go1.20", "go1.21", "go1.26"} {
		info.FileVersions[file] = target
		pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info}
		c.Check(pass)
		if target == "" || target == "go1.20" {
			assert.Empty(t, pass.Offenses(), target)
		} else {
			assert.Len(t, pass.Offenses(), 1, target)
		}
	}
	pass := &cop.Pass{Cop: c, FileSet: fset, File: file}
	c.Run(pass)
	assert.Empty(t, pass.Offenses(), "missing types")
	assert.True(t, newMapsCloneFile(cop.WithScope(cop.UnderDir("internal"))).InScope(pass))
	assert.False(t, newMapsCloneFile(cop.WithScope(cop.UnderDir("other"))).InScope(pass))
}
