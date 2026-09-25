package cops

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/coptest"
)

func TestSortStableFunc(t *testing.T) {
	t.Parallel()
	files := coptest.ProgramFiles{}
	var want []string
	for _, tc := range []struct {
		name, src string
		want      bool
	}{
		{"ascending", `import "sort"
func f(xs []int) { sort.SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] }) }`, true},
		{"descending", `import "sort"
type item struct { score int }
func f(xs []item) { sort.SliceStable(xs, func(i, j int) bool { return xs[i].score > xs[j].score }) }`, true},
		{"strings", `import "sort"
func f(xs []string) { sort.SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] }) }`, true},
		{"named_unsigned", `import "sort"
type score uint64
func f(xs []score) { sort.SliceStable(xs, func(i, j int) bool { return xs[i] > xs[j] }) }`, true},
		{"alias", `import order "sort"
func f(xs []int) { order.SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] }) }`, true},
		{"dot_import", `import . "sort"
func f(xs []int) { SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] }) }`, true},
		{"parentheses", `import "sort"
func f(xs []int) { (sort.SliceStable)((xs), (func(a int, b int) bool { return ((xs)[(a)] > (xs)[(b)]) })) }`, true},
		{"nested_keys", `import "sort"
type item struct { result struct { name string } }
func f(xs []item) { sort.SliceStable(xs, func(i, j int) bool { return xs[i].result.name < xs[j].result.name }) }`, true},
		{"tie_breaks", `import "sort"
type item struct { kind int; score uint; name string }
func f(xs []item) {
	sort.SliceStable(xs, func(i, j int) bool {
		if xs[i].kind != xs[j].kind { return xs[i].kind < xs[j].kind }
		if xs[i].score != xs[j].score { return xs[i].score > xs[j].score }
		return xs[i].name < xs[j].name
	})
}`, true},
		{"float", `import "sort"
func f(xs []float64) { sort.SliceStable(xs, func(i, j int) bool { return xs[i] > xs[j] }) }`, false},
		{"float_tie_break", `import "sort"
type item struct { kind int; score float32 }
func f(xs []item) { sort.SliceStable(xs, func(i, j int) bool {
	if xs[i].kind != xs[j].kind { return xs[i].kind < xs[j].kind }; return xs[i].score > xs[j].score
}) }`, false},
		{"unstable", `import "sort"
func f(xs []int) { sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] }) }`, false},
		{"modern", `import ("cmp"; "slices")
func f(xs []int) { slices.SortStableFunc(xs, cmp.Compare[int]) }`, false},
		{"shadowed", `type sorter struct{}
func (sorter) SliceStable([]int, func(int, int) bool) {}
func f(xs []int) { sort := sorter{}; sort.SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] }) }`, false},
		{"dynamic_slice", `import "sort"
func f(xs any) { sort.SliceStable(xs, func(i, j int) bool { return xs.([]int)[i] < xs.([]int)[j] }) }`, false},
		{"other_slice", `import "sort"
func f(xs, ys []int) { sort.SliceStable(xs, func(i, j int) bool { return ys[i] < ys[j] }) }`, false},
		{"indices", `import "sort"
func f(xs []int) { sort.SliceStable(xs, func(i, j int) bool { return i < j }) }`, false},
		{"different_keys", `import "sort"
type item struct { a, b int }
func f(xs []item) { sort.SliceStable(xs, func(i, j int) bool { return xs[i].a < xs[j].b }) }`, false},
		{"mismatched_guard", `import "sort"
type item struct { a, b int }
func f(xs []item) { sort.SliceStable(xs, func(i, j int) bool {
	if xs[i].a != xs[j].a { return xs[i].b < xs[j].b }; return xs[i].a < xs[j].a
}) }`, false},
		{"non_strict", `import "sort"
func f(xs []int) { sort.SliceStable(xs, func(i, j int) bool { return xs[i] <= xs[j] }) }`, false},
		{"calls", `import ("sort"; "strings")
func f(xs []string) { sort.SliceStable(xs, func(i, j int) bool { return strings.ToLower(xs[i]) < strings.ToLower(xs[j]) }) }`, false},
		{"side_effect", `import "sort"
func f(xs []int, tick func()) { sort.SliceStable(xs, func(i, j int) bool { tick(); return xs[i] < xs[j] }) }`, false},
		{"guard_side_effect", `import "sort"
func f(xs []int, tick func()) { sort.SliceStable(xs, func(i, j int) bool {
	if tick(); xs[i] != xs[j] { return xs[i] < xs[j] }; return xs[i] > xs[j]
}) }`, false},
		{"else_branch", `import "sort"
func f(xs []int) { sort.SliceStable(xs, func(i, j int) bool {
	if xs[i] != xs[j] { return xs[i] < xs[j] } else { return false }
}) }`, false},
		{"named_callback", `import "sort"
func f(xs []int, less func(int, int) bool) { sort.SliceStable(xs, less) }`, false},
	} {
		filename := tc.name + "/sample.go"
		files[filename] = "package p\n" + tc.src
		if tc.want {
			want = append(want, filename)
		}
	}

	offenses := coptest.RunProgram(t, NewLintSortStableFunc(), files)
	var got []string
	for _, offense := range offenses {
		assert.Equal(t, "Lint/SortStableFunc", offense.CopName)
		assert.Contains(t, offense.Message, "slices.SortStableFunc with cmp.Compare")
		assert.Contains(t, offense.Message, "descending")
		got = append(got, filepath.Base(filepath.Dir(offense.Pos.Filename))+"/"+filepath.Base(offense.Pos.Filename))
	}
	assert.ElementsMatch(t, want, got)
}

func TestSortStableFuncScope(t *testing.T) {
	t.Parallel()
	const src = `package p
import "sort"
func f(xs []int) { sort.SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] }) }
`
	files := coptest.ProgramFiles{
		"pkg/config/v0/sample.go":     src,
		"pkg/config/v15/sample.go":    src,
		"pkg/config/latest/sample.go": src,
		"generated/sample.go":         "// Code generated by fixture; DO NOT EDIT.\n" + src,
		"tests/sample.go":             "package p\n",
		"tests/sample_test.go":        src,
	}
	offenses := coptest.RunProgram(t, NewLintSortStableFunc(), files)
	require.Len(t, offenses, 3)
	for _, offense := range offenses {
		assert.Contains(t, filepath.ToSlash(offense.Pos.Filename), "/pkg/config/")
	}
	assert.Empty(t, coptest.Run(t, newSortStableFuncFile(), src), "missing type information must not report")
}
