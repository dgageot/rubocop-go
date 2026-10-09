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

const sortedMapKeysFixture = `package p
import "sort"
func f(src map[string]int) []string {
 var keys []string
 for k := range src { keys = append(keys, k) }
 sort.Strings(keys)
 return keys
}`

func TestSortedMapKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"strings", `import "sort"; func f(src map[string]int) []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 1},
		{"ints", `import "sort"; func f(src map[int]int) []int { var keys []int; for k := range src { keys = append(keys, k) }; sort.Ints(keys); return keys }`, 1},
		{"slices sort", `import "slices"; func f(src map[uint64]int) []uint64 { var keys []uint64; for k := range src { keys = append(keys, k) }; slices.Sort(keys); return keys }`, 1},
		{"import alias", `import sorting "sort"; func f(src map[string]int) []string { var keys []string; for k := range src { keys = append(keys, k) }; sorting.Strings(keys); return keys }`, 1},
		{"dot import", `import . "sort"; func f(src map[int]int) []int { var keys []int; for k := range src { keys = append(keys, k) }; Ints(keys); return keys }`, 1},
		{"explicit generic sort", `import "slices"; func f(src map[string]int) []string { var keys []string; for k := range src { keys = append(keys, k) }; (slices.Sort[[]string, string])((keys)); return keys }`, 1},
		{"named keys", `import "slices"; type K string; func f(src map[K]int) []K { var keys []K; for k := range src { keys = append(keys, k) }; slices.Sort(keys); return keys }`, 1},
		{"map alias", `import "sort"; type M = map[string]int; func f(src M) []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 1},
		{"slice alias", `import "sort"; type S = []string; func f(src map[string]int) S { var keys S; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 1},
		{"nil conversion", `import "sort"; func f(src map[string]int) []string { keys := []string(nil); for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 1},
		{"var nil initializer", `import "sort"; func f(src map[string]int) []string { var keys []string = nil; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 1},
		{"blank range value", `import "sort"; func f(src map[string]int) []string { var keys []string; for k, _ := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 1},
		{"parentheses", `import "sort"; func f(src map[string]int) []string { keys := ([]string)((nil)); for k := range (src) { keys = (append)((keys), (k)) }; (sort.Strings)((keys)); return keys }`, 1},
		{"shadowed outer key", `import "sort"; func f(src map[string]int, k string) []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys); _ = k; return keys }`, 1},
		{"case body", `import "sort"; func f(src map[string]int) []string { switch { default: var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys } }`, 1},
		{"comm body", `import "sort"; func f(ch chan map[string]int) { select { case src := <-ch: var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys) } }`, 1},
		{"make empty", `import "sort"; func f(src map[string]int) []string { keys := make([]string, 0); for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"empty literal", `import "sort"; func f(src map[string]int) []string { keys := []string{}; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"capacity contract", `import "sort"; func f(src map[string]int) []string { keys := make([]string, 0, len(src)); for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"named slice", `import "sort"; type S []string; func f(src map[string]int) S { var keys S; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"float keys", `import "slices"; func f(src map[float64]int) []float64 { var keys []float64; for k := range src { keys = append(keys, k) }; slices.Sort(keys); return keys }`, 0},
		{"interface keys", `import "sort"; func f(src map[any]int) []string { var keys []string; for k := range src { keys = append(keys, k.(string)) }; sort.Strings(keys); return keys }`, 0},
		{"filter", `import "sort"; func f(src map[string]int) []string { var keys []string; for k := range src { if k != "" { keys = append(keys, k) } }; sort.Strings(keys); return keys }`, 0},
		{"values collected", `import "sort"; func f(src map[string]string) []string { var keys []string; for _, v := range src { keys = append(keys, v) }; sort.Strings(keys); return keys }`, 0},
		{"callback", `import "sort"; func f(src map[string]int, transform func(string) string) []string { var keys []string; for k := range src { keys = append(keys, transform(k)) }; sort.Strings(keys); return keys }`, 0},
		{"extra work", `import "sort"; func f(src map[string]int) []string { var keys []string; for k := range src { keys = append(keys, k); println(k) }; sort.Strings(keys); return keys }`, 0},
		{"source mutation before loop", `import "sort"; func f(src map[string]int) []string { var keys []string; src["new"] = 1; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"intervening work before sort", `import "sort"; func f(src map[string]int) []string { var keys []string; for k := range src { keys = append(keys, k) }; println(keys); sort.Strings(keys); return keys }`, 0},
		{"custom comparator", `import "slices"; func f(src map[string]int) []string { var keys []string; for k := range src { keys = append(keys, k) }; slices.SortFunc(keys, func(a, b string) int { return len(a)-len(b) }); return keys }`, 0},
		{"sort slice", `import "sort"; func f(src map[string]int) []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] }); return keys }`, 0},
		{"assigned range variable", `import "sort"; func f(src map[string]int) []string { var keys []string; var k string; for k = range src { keys = append(keys, k) }; sort.Strings(keys); _ = k; return keys }`, 0},
		{"different sort destination", `import "sort"; func f(src map[string]int, other []string) []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(other); return keys }`, 0},
		{"shadowed append", `import "sort"; func f(src map[string]int, append func([]string, string) []string) []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"shadowed sort", `func f(src map[string]int, sort struct { Strings func([]string) }) []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"shadowed nil", `import "sort"; var nil []string; func f(src map[string]int) []string { keys := []string(nil); for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"field source", `import "sort"; type S struct { src map[string]int }; func f(s S) []string { var keys []string; for k := range s.src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"global source", `import "sort"; var src map[string]int; func f() []string { var keys []string; for k := range src { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
		{"effectful source", `import "sort"; func f(get func() map[string]int) []string { var keys []string; for k := range get() { keys = append(keys, k) }; sort.Strings(keys); return keys }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newSortedMapKeysFile(), "package p\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/SortedMapKeys", offense.CopName)
				assert.Equal(t, cop.Convention, offense.Severity)
				assert.Contains(t, offense.Message, "slices.Sorted(maps.Keys(src))")
				assert.Contains(t, offense.Message, "capacity contract")
				assert.Equal(t, 2, offense.Pos.Line)
				assert.Greater(t, offense.End.Offset, offense.Pos.Offset)
			}
		})
	}
}

func TestSortedMapKeysConfiguration(t *testing.T) {
	t.Parallel()
	c := newSortedMapKeysFile()
	require.NotSame(t, c, newSortedMapKeysFile())
	assert.True(t, c.Types)
	assert.True(t, c.NeedsTypes())
	assert.Nil(t, c.Scope)
	assert.Equal(t, "go1.23", c.MinStdlibVersion)
	assert.Equal(t, "go1.23", newSortedMapKeysFile(cop.WithMinStdlibVersion("go1.20")).MinStdlibVersion)
	assert.Equal(t, "go1.30", newSortedMapKeysFile(cop.WithMinStdlibVersion("go1.30")).MinStdlibVersion)
	assert.Empty(t, runExtractedTypedCop(t, newSortedMapKeysFile(cop.WithMinStdlibVersion("go1.30")), sortedMapKeysFixture))
	assert.Empty(t, runExtractedTypedCop(t, c, "// Code generated by fixture; DO NOT EDIT.\n"+sortedMapKeysFixture))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "internal/sample_test.go", sortedMapKeysFixture, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), FileVersions: make(map[*ast.File]string),
	}
	cfg := types.Config{Importer: importer.Default()}
	_, err = cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	for _, target := range []string{"", "go1.21", "go1.22", "go1.23", "go1.26"} {
		info.FileVersions[file] = target
		pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info}
		c.Check(pass)
		if target == "" || target == "go1.21" || target == "go1.22" {
			assert.Empty(t, pass.Offenses(), target)
		} else {
			assert.Len(t, pass.Offenses(), 1, target)
		}
	}
	pass := &cop.Pass{Cop: c, FileSet: fset, File: file}
	c.Run(pass)
	assert.Empty(t, pass.Offenses(), "missing types")
	assert.True(t, newSortedMapKeysFile(cop.WithScope(cop.UnderDir("internal"))).InScope(pass))
	assert.False(t, newSortedMapKeysFile(cop.WithScope(cop.UnderDir("other"))).InScope(pass))
}
