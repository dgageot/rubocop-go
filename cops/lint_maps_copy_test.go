package cops

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
)

const mapsCopyFixture = `package p
func f(dst, src map[string]int) {
 for k, v := range src { dst[k] = v }
}`

func TestMapsCopy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"plain", `func f(dst, src map[string]int) { for k, v := range src { dst[k] = v } }`, 1},
		{"named maps", `type M map[string]int; func f(dst, src M) { for k, v := range src { dst[k] = v } }`, 1},
		{"aliases", `type M = map[string]int; func f(dst, src M) { for k, v := range src { dst[k] = v } }`, 1},
		{"distinct named maps", `type M map[string]int; type N map[string]int; func f(dst M, src N) { for k, v := range src { dst[k] = v } }`, 1},
		{"local declarations", `func f() { src := map[int]int{1: 2}; dst := map[int]int{}; for k, v := range src { dst[k] = v } }`, 1},
		{"parentheses", `func f(dst, src map[int]int) { for k, v := range (src) { (dst)[(k)] = (v) } }`, 1},
		{"self copy", `func f(src map[int]int) { for k, v := range src { src[k] = v } }`, 1},
		{"NaN keys copy uses same loop", `func f(dst, src map[float64]int) { for k, v := range src { dst[k] = v } }`, 1},
		{"shadowed outer names", `func f(dst, src map[int]int, k, v int) { for k, v := range src { dst[k] = v }; _, _ = k, v }`, 1},
		{"assigned range variables", `func f(dst, src map[int]int) { var k, v int; for k, v = range src { dst[k] = v }; _, _ = k, v }`, 0},
		{"blank value", `func f(dst, src map[int]int) { for k, _ := range src { dst[k] = src[k] } }`, 0},
		{"key only", `func f(dst, src map[int]int) { for k := range src { dst[k] = src[k] } }`, 0},
		{"extra work", `func f(dst, src map[int]int) { for k, v := range src { dst[k] = v; println(k) } }`, 0},
		{"callback", `func f(dst, src map[int]int, transform func(int) int) { for k, v := range src { dst[k] = transform(v) } }`, 0},
		{"filter", `func f(dst, src map[int]int) { for k, v := range src { if k > 0 { dst[k] = v } } }`, 0},
		{"different value", `func f(dst, src map[int]int, v int) { for k := range src { dst[k] = v } }`, 0},
		{"different key", `func f(dst, src map[int]int, key int) { for _, v := range src { dst[key] = v } }`, 0},
		{"global source", `var src map[int]int; func f(dst map[int]int) { for k, v := range src { dst[k] = v } }`, 0},
		{"global destination", `var dst map[int]int; func f(src map[int]int) { for k, v := range src { dst[k] = v } }`, 0},
		{"field destination", `type S struct { dst map[int]int }; func f(s S, src map[int]int) { for k, v := range src { s.dst[k] = v } }`, 0},
		{"field source", `type S struct { src map[int]int }; func f(s S, dst map[int]int) { for k, v := range s.src { dst[k] = v } }`, 0},
		{"effectful source", `func f(dst map[int]int, get func() map[int]int) { for k, v := range get() { dst[k] = v } }`, 0},
		{"effectful destination", `func f(src map[int]int, get func() map[int]int) { for k, v := range src { get()[k] = v } }`, 0},
		{"assignable but different value types", `func f(dst map[int]any, src map[int]int) { for k, v := range src { dst[k] = v } }`, 0},
		{"slice range", `func f(dst map[int]int, src []int) { for k, v := range src { dst[k] = v } }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newMapsCopyFile(), "package p\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/MapsCopy", offense.CopName)
				assert.Equal(t, cop.Convention, offense.Severity)
				assert.Contains(t, offense.Message, "maps.Copy(dst, src)")
				assert.Equal(t, 2, offense.Pos.Line)
				assert.Greater(t, offense.End.Offset, offense.Pos.Offset)
			}
		})
	}
}

func TestMapsCopyGenericShapes(t *testing.T) {
	t.Parallel()
	const loop = "for k, v := range src { dst[k] = v }"
	for _, tc := range []struct {
		name, decl string
		want       int
		copyValid  bool
	}{
		{"type parameters in map", `func f[K comparable, V any](dst, src map[K]V)`, 1, true},
		{"map type parameter", `func f[M ~map[K]V, K comparable, V any](dst, src M)`, 1, true},
		{"exact map constraint", `func f[M map[K]V, K comparable, V any](dst, src M)`, 1, true},
		{"distinct map parameters", `func f[D, S ~map[K]V, K comparable, V any](dst D, src S)`, 1, true},
		{"generic source", `func f[M ~map[K]V, K comparable, V any](dst map[K]V, src M)`, 1, true},
		{"generic destination", `func f[M ~map[K]V, K comparable, V any](dst M, src map[K]V)`, 1, true},
		{"named constraint", `type C[K comparable, V any] interface { ~map[K]V }; func f[M C[K, V], K comparable, V any](dst, src M)`, 1, true},
		{"embedded constraint", `type C interface { ~map[string]int }; type D interface { C }; func f[M D](dst, src M)`, 1, true},
		{"constraint with method", `func f[M interface { ~map[string]int; Mark() }](dst, src M)`, 1, true},
		{"named map union", `type A map[string]int; type B map[string]int; func f[M A | B](dst, src M)`, 1, true},
		{"embedded union", `type A map[string]int; type C interface { A }; type D interface { ~map[string]int }; func f[M C | D](dst, src M)`, 1, true},
		{"generic aliases", `type C[K comparable, V any] = interface { ~map[K]V }; type A[K comparable, V any] = map[K]V; func f[M C[K, V], K comparable, V any](dst A[K, V], src M)`, 1, true},
		{"assignable key not identical", `func f[M ~map[K]V, K comparable, V any](dst map[any]V, src M)`, 0, false},
		{"assignable value not identical", `func f[M ~map[K]V, K comparable, V any](dst map[K]any, src M)`, 0, false},
		{"distinct parameter and concrete value", `func f[M ~map[string]V, V ~int](dst map[string]any, src M)`, 0, false},
		{"slice parameter", `func f[S ~[]V, V any](dst map[int]V, src S)`, 0, false},
		{"intersected constraints", `type C interface { ~map[string]int }; type D interface { ~map[string]int }; func f[M interface { C; D }](dst, src M)`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\n" + tc.decl + " { " + loop + " }"
			require.Len(t, runExtractedTypedCop(t, newMapsCopyFile(), src), tc.want)

			replacement := strings.Replace(src, "package p", "package p\nimport \"maps\"", 1)
			replacement = strings.Replace(replacement, loop, "maps.Copy(dst, src)", 1)
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "sample.go", replacement, 0)
			require.NoError(t, err)
			cfg := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
			_, err = cfg.Check("fixture", fset, []*ast.File{file}, nil)
			if tc.copyValid {
				require.NoError(t, err, "suggested maps.Copy must compile")
			} else {
				require.Error(t, err, "incompatible map shapes must not be reported")
			}
		})
	}
}

func TestMapsCopyRejectsUnsupportedMapConstraints(t *testing.T) {
	t.Parallel()
	for _, constraint := range []string{
		"any",
		"comparable",
		"interface { Mark() }",
		"~[]int",
		"~map[int]int | ~[]int",
		"~map[int]int | ~map[string]int",
		"~map[int]int | ~map[int]string",
		"interface { ~map[int]int; ~map[string]int }",
		"interface { ~map[int]int; comparable }",
	} {
		t.Run(constraint, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "sample.go", "package p; func f[M "+constraint+"](src M) {}", 0)
			require.NoError(t, err)
			cfg := types.Config{GoVersion: "go1.26"}
			pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, nil)
			require.NoError(t, err)
			sig := pkg.Scope().Lookup("f").Type().(*types.Signature)
			assert.Nil(t, mapsCopyMapType(sig.Params().At(0).Type(), make(map[types.Type]*types.Map)))
		})
	}
}

// mapsCopyCountedType counts graph visits without relying on elapsed time.
type mapsCopyCountedType struct {
	types.Type

	t      *testing.T
	visits int
}

func (typ *mapsCopyCountedType) Underlying() types.Type {
	typ.t.Helper()
	typ.visits++
	require.Equal(typ.t, 1, typ.visits, "each node must be resolved once")
	return typ.Type.Underlying()
}

func TestMapsCopyConstraintDAG(t *testing.T) {
	t.Parallel()
	for _, supported := range []bool{true, false} {
		name := "unsupported"
		if supported {
			name = "supported"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var nodes []*mapsCopyCountedType
			wrap := func(typ types.Type) types.Type {
				node := &mapsCopyCountedType{Type: typ, t: t}
				nodes = append(nodes, node)
				return node
			}
			shape := types.NewMap(types.Typ[types.String], types.Typ[types.Int])
			var base types.Type = shape
			if !supported {
				base = types.NewSlice(types.Typ[types.Int])
			}
			left, right := wrap(base), wrap(base)
			for range 64 {
				branch := func() types.Type {
					union := wrap(types.NewUnion([]*types.Term{
						types.NewTerm(false, left), types.NewTerm(false, right),
					}))
					return wrap(types.NewInterfaceType(nil, []types.Type{union}))
				}
				left, right = branch(), branch()
			}
			memo := make(map[types.Type]*types.Map)
			for range 100 {
				for _, root := range []types.Type{left, right} {
					got := mapsCopyConstraintMap(root, memo)
					if supported {
						require.Same(t, shape, got)
					} else {
						require.Nil(t, got)
					}
				}
			}
			// Resolve even nodes skipped by an unsupported branch, twice each.
			for _, node := range nodes {
				mapsCopyConstraintMap(node, memo)
				mapsCopyConstraintMap(node, memo)
				require.Equal(t, 1, node.visits, "each node must be resolved once")
			}
			require.Len(t, memo, len(nodes), "cache size must be linear in graph size")
		})
	}
}

func TestMapsCopyConstraintCycles(t *testing.T) {
	t.Parallel()
	left, right := &mapsCopyCountedType{t: t}, &mapsCopyCountedType{t: t}
	left.Type = types.NewInterfaceType(nil, []types.Type{right})
	right.Type = types.NewInterfaceType(nil, []types.Type{left})
	shape := types.NewMap(types.Typ[types.String], types.Typ[types.Int])
	root := types.NewUnion([]*types.Term{types.NewTerm(false, shape), types.NewTerm(false, left)})
	memo := make(map[types.Type]*types.Map)
	for range 2 {
		require.Nil(t, mapsCopyConstraintMap(root, memo))
		require.Nil(t, mapsCopyConstraintMap(left, memo))
		require.Nil(t, mapsCopyConstraintMap(right, memo))
	}
	require.Equal(t, 1, left.visits)
	require.Equal(t, 1, right.visits)
	require.Len(t, memo, 4)
}

func TestMapsCopyGenericDAG(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	src.WriteString("package p\ntype A0 = interface { ~map[string]int }; type B0 = interface { ~map[string]int }\n")
	for i := 1; i <= 18; i++ {
		fmt.Fprintf(&src, "type A%d = interface { A%d | B%d }; type B%d = interface { A%d | B%d }\n", i, i-1, i-1, i, i-1, i-1)
	}
	src.WriteString("func f[D A18, S B18](dst D, src S) {\n")
	for range 100 {
		src.WriteString("for k, v := range src { dst[k] = v }\n")
	}
	src.WriteString("}\nfunc g[D B18, S A18](dst D, src S) {\n")
	for range 100 {
		src.WriteString("for k, v := range src { dst[k] = v }\n")
	}
	src.WriteString("}\n")
	require.Len(t, runExtractedTypedCop(t, newMapsCopyFile(), src.String()), 200)
}

func TestMapsCopyConfiguration(t *testing.T) {
	t.Parallel()
	c := newMapsCopyFile()
	require.NotSame(t, c, newMapsCopyFile())
	assert.True(t, c.Types)
	assert.True(t, c.NeedsTypes())
	assert.Nil(t, c.Scope)
	assert.Equal(t, "go1.21", c.MinStdlibVersion)
	assert.Equal(t, "go1.21", newMapsCopyFile(cop.WithMinStdlibVersion("go1.20")).MinStdlibVersion)
	assert.Equal(t, "go1.30", newMapsCopyFile(cop.WithMinStdlibVersion("go1.30")).MinStdlibVersion)
	assert.Empty(t, runExtractedTypedCop(t, newMapsCopyFile(cop.WithMinStdlibVersion("go1.30")), mapsCopyFixture))
	assert.Empty(t, runExtractedTypedCop(t, c, "// Code generated by fixture; DO NOT EDIT.\n"+mapsCopyFixture))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "internal/sample_test.go", mapsCopyFixture, parser.ParseComments)
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
	assert.True(t, newMapsCopyFile(cop.WithScope(cop.UnderDir("internal"))).InScope(pass))
	assert.False(t, newMapsCopyFile(cop.WithScope(cop.UnderDir("other"))).InScope(pass))
}
