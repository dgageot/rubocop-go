package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

const slicesEqualFixture = `package p
func equal(a, b []int) bool {
	if len(a) != len(b) { return false }
	for i := range a { if a[i] != b[i] { return false } }
	return true
}`

func TestSlicesEqual(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"index loop", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"value loop", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i, v := range a { if v != b[i] { return false } }; return true }`, 1},
		{"blank value", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i, _ := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"reversed operands", `func f(a, b []int) bool { if len(b) != len(a) { return false }; for i, v := range b { if a[i] != v { return false } }; return true }`, 1},
		{"parentheses", `func f(a, b []int) bool { if ((len)((a)) != (len)((b))) { return (false) }; for i := range (a) { if ((a)[(i)] != (b)[(i)]) { return (false) } }; return (true) }`, 1},
		{"same named type", `type S []int; func f(a, b S) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"named and unnamed", `type S []int; func f(a S, b []int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"type aliases", `type E = int; type S = []E; func f(a, b S) bool { if len(a) != len(b) { return false }; for i, v := range a { if v != b[i] { return false } }; return true }`, 1},
		{"import alias type", `import clock "time"; func f(a, b []clock.Duration) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"generic elements", `func f[T comparable](a, b []T) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"interface comparisons", `func f(a, b []any) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"NaN semantics", `func f(a, b []float64) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 1},
		{"closure", `var f = func(a, b []int) bool { if len(a) != len(b) { return false }; for i, v := range a { if v != b[i] { return false } }; return true }`, 1},
		{"distinct named slices", `type A []int; type B []int; func f(a A, b B) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"distinct element types", `type E int; func f(a []E, b []int) bool { if len(a) != len(b) { return false }; for i := range a { if int(a[i]) != b[i] { return false } }; return true }`, 0},
		{"nil sensitivity", `func f(a, b []int) bool { if (a == nil) != (b == nil) { return false }; if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"only nil equals nil", `func f(a, b []int) bool { if a == nil { return b == nil }; if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"wrong length guard", `func f(a, b []int) bool { if len(a) < len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"missing length guard", `func f(a, b []int) bool { for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"shadowed len", `func f(a, b []int, len func([]int) int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"shadowed false", `func f(a, b []int, false bool) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"shadowed slice by value", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i, b := range a { if a[i] != b { return false } }; return true }`, 0},
		{"wrong index", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[0] { return false } }; return true }`, 0},
		{"different slice", `func f(a, b, c []int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != c[i] { return false } }; return true }`, 0},
		{"array", `func f(a, b [2]int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"string", `func f(a, b string) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"effectful repeated source", `func f(a []int, b func() []int) bool { if len(a) != len(b()) { return false }; for i := range a { if a[i] != b()[i] { return false } }; return true }`, 0},
		{"global slice", `var b []int; func f(a []int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"field slices", `type S struct{ a, b []int }; func f(s S) bool { if len(s.a) != len(s.b) { return false }; for i := range s.a { if s.a[i] != s.b[i] { return false } }; return true }`, 0},
		{"element callback", `func f(a, b []int, eq func(int, int) bool) bool { if len(a) != len(b) { return false }; for i := range a { if !eq(a[i], b[i]) { return false } }; return true }`, 0},
		{"extra effect", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i := range a { println(i); if a[i] != b[i] { return false } }; return true }`, 0},
		{"guard effect", `func f(a, b []int) bool { if len(a) != len(b) { println(a); return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
		{"reverse mismatch", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] == b[i] { return false } }; return true }`, 0},
		{"wrong fallback", `func f(a, b []int) bool { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return false }`, 0},
		{"named bool result", `type B bool; func f(a, b []int) B { if len(a) != len(b) { return false }; for i := range a { if a[i] != b[i] { return false } }; return true }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newSlicesEqualFile(), "package p\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/SlicesEqual", offense.CopName)
				assert.Contains(t, offense.Message, "slices.Equal")
				assert.Greater(t, offense.End.Offset, offense.Pos.Offset)
			}
		})
	}
}

func TestSlicesEqualConfiguration(t *testing.T) {
	t.Parallel()
	fresh := newSlicesEqualFile()
	scoped := newSlicesEqualFile(cop.WithScope(cop.UnderDir("internal")))
	require.NotSame(t, fresh, scoped)
	assert.Nil(t, fresh.Scope)
	assert.NotNil(t, scoped.Scope)
	assert.Empty(t, runSliceModernizationVersion(t, scoped, slicesEqualFixture, "go1.26", ""))
	assert.True(t, fresh.Types)
	assert.Len(t, runSliceModernizationVersion(t, fresh, slicesEqualFixture, "go1.26", "go1.20"), 1)
	assert.True(t, fresh.NeedsTypes())
	assert.Empty(t, coptest.Run(t, fresh, slicesEqualFixture))
	assert.Empty(t, runExtractedTypedCop(t, fresh, "// Code generated by fixture; DO NOT EDIT.\n"+slicesEqualFixture))
	fresh.Run(&cop.Pass{})
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
			assert.Len(t, runSliceModernizationVersion(t, newSlicesEqualFile(tc.opts...), slicesEqualFixture, tc.module, ""), tc.want)
		})
	}
}
