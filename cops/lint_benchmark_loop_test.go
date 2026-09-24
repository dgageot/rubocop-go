package cops

import (
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
	"github.com/dgageot/rubocop-go/coptest"
)

const benchmarkLoopFixture = `package p
import "testing"
func work() {}
func BenchmarkWork(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N { work() }
}
`

func TestBenchmarkLoop(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"range", benchmarkLoopFixture},
		{"blank range", strings.Replace(benchmarkLoopFixture, "for range", "for _ = range", 1)},
		{"indexed unused counter", strings.Replace(benchmarkLoopFixture, "for range b.N", "for i := 0; i < b.N; i++", 1)},
		{"constant zero", strings.Replace(benchmarkLoopFixture, "for range b.N", "for i := 1-1; i < b.N; i++", 1)},
		{"parentheses", strings.ReplaceAll(strings.Replace(benchmarkLoopFixture, "for range b.N", "for range (b).N", 1), "b.ResetTimer()", "(b.ResetTimer)()")},
		{"alias import", strings.ReplaceAll(strings.Replace(benchmarkLoopFixture, `import "testing"`, `import test "testing"`, 1), "testing.", "test.")},
		{"dot import", strings.ReplaceAll(strings.Replace(benchmarkLoopFixture, `import "testing"`, `import . "testing"`, 1), "testing.", "")},
		{"type alias", strings.Replace(benchmarkLoopFixture, "*testing.B", "*Alias", 1) + "\ntype Alias = testing.B\n"},
		{"report allocations after reset", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()\n\tb.ResetTimer()", "b.ResetTimer(); b.ReportAllocs()", 1)},
		{"report bytes setup", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "b.ReportAllocs(); b.SetBytes(32)", 1)},
		{"setup loop", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "for range 3 { work() }; b.ReportAllocs()", 1)},
		{"setup context and directory", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "_ = b.Context(); _ = b.TempDir(); b.ReportAllocs()", 1)},
		{"local results", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ result := 1; result++; _ = result; work() }", 1)},
		{"subbenchmark", `package p
import "testing"
func work() {}
func BenchmarkWork(parent *testing.B) {
	for _, name := range []string{"one", "two"} {
		parent.Run(name, func(b *testing.B) { b.ResetTimer(); for range b.N { work() } })
	}
}`},
		{"shadowing subbenchmark", `package p
import "testing"
func work() {}
func BenchmarkWork(b *testing.B) {
	b.Run("sub", func(b *testing.B) { b.ResetTimer(); for i := 0; i < b.N; i++ { work() } })
}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runBenchmarkLoop(t, "sample_test.go", tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/BenchmarkLoop", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "b.Loop()")
			assert.Contains(t, offenses[0].Message, "review benchmark measurement effects")
			assert.Contains(t, offenses[0].Message, "rather than applying a mechanical rewrite")
		})
	}
}

func TestBenchmarkLoopExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"no reset", strings.Replace(benchmarkLoopFixture, "b.ResetTimer()", "", 1)},
		{"already loop", strings.Replace(benchmarkLoopFixture, "range b.N", "b.Loop()", 1)},
		{"used range counter", strings.Replace(benchmarkLoopFixture, "for range b.N { work() }", "for i := range b.N { _ = i; work() }", 1)},
		{"used indexed counter", strings.Replace(benchmarkLoopFixture, "for range b.N { work() }", "for i := 0; i < b.N; i++ { _ = i; work() }", 1)},
		{"one based counter", strings.Replace(benchmarkLoopFixture, "for range b.N", "for i := 1; i < b.N; i++", 1)},
		{"inclusive limit", strings.Replace(benchmarkLoopFixture, "for range b.N", "for i := 0; i <= b.N; i++", 1)},
		{"stepped counter", strings.Replace(benchmarkLoopFixture, "for range b.N", "for i := 0; i < b.N; i += 2", 1)},
		{"outer counter", strings.Replace(benchmarkLoopFixture, "for range b.N", "i := 0; for ; i < b.N; i++", 1)},
		{"break", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ work(); break }", 1)},
		{"conditional break", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ work(); if true { break } }", 1)},
		{"continue", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ work(); continue }", 1)},
		{"return", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ work(); return }", 1)},
		{"goto", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ again: work(); goto again }", 1)},
		{"defer", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ defer work() }", 1)},
		{"goroutine", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ go work() }", 1)},
		{"parallel", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ b.RunParallel(func(pb *testing.PB) { for pb.Next() { work() } }) }", 1)},
		{"set parallelism", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "b.SetParallelism(2)", 1)},
		{"timer stopped in body", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ b.StopTimer(); work(); b.StartTimer() }", 1)},
		{"timer stopped in setup", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "b.StopTimer(); work(); b.StartTimer()", 1)},
		{"second reset", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "b.ResetTimer()", 1)},
		{"nested reset", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ b.ResetTimer(); work() }", 1)},
		{"setup uses N", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "_ = make([]int, b.N)", 1)},
		{"multiple loops", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "for range b.N { work() }", 1)},
		{"nested loops", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ for range 2 { work() } }", 1)},
		{"result accounting", strings.Replace(benchmarkLoopFixture, "for range b.N { work() }", "for range b.N { work() }; b.ReportMetric(float64(b.N), \"work/op\")", 1)},
		{"custom metrics", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "b.ReportMetric(1, \"work/op\")", 1)},
		{"result accumulator", strings.Replace(strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "total := 0; _ = total", 1), "{ work() }", "{ total++; work() }", 1)},
		{"assigned result", strings.Replace(strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "total := 0; _ = total", 1), "{ work() }", "{ total = 1; work() }", 1)},
		{"result field accumulator", strings.Replace(strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "state := struct{ total int }{}", 1), "{ work() }", "{ state.total++; work() }", 1)},
		{"indexed result accumulator", strings.Replace(strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "total := []int{0}", 1), "{ work() }", "{ total[0]++; work() }", 1)},
		{"panic", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ work(); panic(\"stop\") }", 1)},
		{"goexit", strings.Replace(strings.Replace(benchmarkLoopFixture, `import "testing"`, `import "testing"; import "runtime"`, 1), "{ work() }", "{ work(); runtime.Goexit() }", 1)},
		{"post loop work", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ work() }; work()", 1)},
		{"post reset setup", strings.Replace(benchmarkLoopFixture, "b.ResetTimer()", "b.ResetTimer(); work()", 1)},
		{"fatal in body", strings.Replace(benchmarkLoopFixture, "{ work() }", "{ work(); b.Fatal(\"failed\") }", 1)},
		{"escaping B", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "helper(b)", 1) + "\nfunc helper(*testing.B) {}\n"},
		{"alias B", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "alias := b; alias.ReportAllocs()", 1)},
		{"method value", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "reset := b.ResetTimer; reset()", 1)},
		{"captured B", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "func() { b.ResetTimer() }()", 1)},
		{"capture allowed method", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "_ = func() { b.ReportAllocs() }", 1)},
		{"captured reset", strings.Replace(benchmarkLoopFixture, "b.ReportAllocs()", "_ = func() { b.ResetTimer() }", 1)},
		{"lookalike type", `package p
type B struct { N int }
func (*B) ResetTimer() {}
func BenchmarkWork(b *B) { b.ResetTimer(); for range b.N {} }`},
		{"embedded type", `package p
import "testing"
type B struct { *testing.B }
func BenchmarkWork(b *B) { b.ResetTimer(); for range b.N {} }`},
		{"shadowed B", `package p
import "testing"
type fake struct { N int }
func (*fake) ResetTimer() {}
func BenchmarkWork(b *testing.B) { { b := &fake{}; b.ResetTimer(); for range b.N {} } }`},
		{"defined type", `package p
import "testing"
type B testing.B
func (*B) ResetTimer() {}
func BenchmarkWork(b *B) { b.ResetTimer(); for range b.N {} }`},
		{"not benchmark", strings.Replace(benchmarkLoopFixture, "BenchmarkWork", "helper", 1)},
		{"lowercase benchmark suffix", strings.Replace(benchmarkLoopFixture, "BenchmarkWork", "Benchmarkwork", 1)},
		{"method", strings.Replace(benchmarkLoopFixture, "func BenchmarkWork", "type suite struct{}; func (suite) BenchmarkWork", 1)},
		{"unused callback", `package p
import "testing"
var callback = func(b *testing.B) { b.ResetTimer(); for range b.N {} }`},
		{"fake subbenchmark Run", `package p
import "testing"
type runner struct{}
func (runner) Run(string, func(*testing.B)) {}
func BenchmarkWork(b *testing.B) { runner{}.Run("sub", func(b *testing.B) { b.ResetTimer(); for range b.N {} }) }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, runBenchmarkLoop(t, "sample_test.go", tc.src))
		})
	}
}

func TestBenchmarkLoopScope(t *testing.T) {
	t.Parallel()
	assert.Empty(t, runBenchmarkLoop(t, "sample.go", benchmarkLoopFixture))
	assert.Empty(t, runBenchmarkLoop(t, "sample_test.go", "// Code generated by fixture; DO NOT EDIT.\n"+benchmarkLoopFixture))
	assert.Len(t, runBenchmarkLoop(t, "pkg/config/v3/sample_test.go", benchmarkLoopFixture), 1)
	require.Len(t, runBenchmarkLoop(t, "pkg/config/latest/sample_test.go", benchmarkLoopFixture), 1)
}

func TestBenchmarkLoopProgram(t *testing.T) {
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintBenchmarkLoop(), coptest.ProgramFiles{
		"go.mod":         "module example.test\n\ngo 1.26\n",
		"fixture.go":     "package p\n",
		"sample_test.go": benchmarkLoopFixture,
	})
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/BenchmarkLoop", offenses[0].CopName)
	assert.Equal(t, 7, offenses[0].Pos.Line)
}

func TestBenchmarkLoopProgramSubbenchmarks(t *testing.T) {
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintBenchmarkLoop(), coptest.ProgramFiles{
		"go.mod":     "module example.test\n\ngo 1.26\n",
		"fixture.go": "package p\n",
		"sample_test.go": `package p_test
import test "testing"
func BenchmarkOuter(parent *test.B) {
	parent.Run("inner", func(b *test.B) { b.ResetTimer(); for range b.N {} })
}`,
	})
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/BenchmarkLoop", offenses[0].CopName)
}

func runBenchmarkLoop(t *testing.T, filename, src string) []cop.Offense {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	cfg := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
	pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	pass := &cop.Pass{Cop: newBenchmarkLoopFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newBenchmarkLoopFile().Check(pass)
	return pass.Offenses()
}

func TestBenchmarkLoopTestOnlyPackage(t *testing.T) {
	t.Parallel()
	for _, pkg := range []string{"p", "p_test"} {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()
			offenses := coptest.RunProgram(t, NewLintBenchmarkLoop(), coptest.ProgramFiles{
				"go.mod":         "module example.test\n\ngo 1.26\n",
				"sample_test.go": strings.Replace(benchmarkLoopFixture, "package p", "package "+pkg, 1),
			})
			require.Len(t, offenses, 1)
		})
	}
}
