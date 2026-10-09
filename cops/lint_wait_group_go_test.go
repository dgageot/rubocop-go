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

const waitGroupGoFixture = `package p
import "sync"
func f() {
 var wg sync.WaitGroup
 wg.Add(1)
 go func() { defer wg.Done(); println("work") }()
 wg.Wait()
}`

func TestWaitGroupGo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"local value", `func f() { var wg sync.WaitGroup; wg.Add(1); go func() { defer wg.Done(); println("work") }() }`, 1},
		{"pointer parameter", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); println("work") }() }`, 1},
		{"value parameter", `func f(wg sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done() }() }`, 1},
		{"local pointer", `func f() { wg := new(sync.WaitGroup); wg.Add(1); go func() { defer wg.Done() }(); wg.Wait() }`, 1},
		{"constant one", `func f(wg *sync.WaitGroup) { const n = 2 - 1; wg.Add(n); go func() { defer wg.Done() }() }`, 1},
		{"parentheses", `func f(wg *sync.WaitGroup) { (wg).Add((1)); go (func() { defer (wg).Done() })() }`, 1},
		{"loop body", `func f(wg *sync.WaitGroup) { for range 3 { wg.Add(1); go func() { defer wg.Done() }() } }`, 1},
		{"switch case", `func f(wg *sync.WaitGroup, n int) { switch n { case 0: wg.Add(1); go func() { defer wg.Done() }() } }`, 1},
		{"select case", `func f(wg *sync.WaitGroup, ch chan int) { select { case <-ch: wg.Add(1); go func() { defer wg.Done() }() } }`, 1},
		{"multiple groups", `func f(a, b *sync.WaitGroup) { a.Add(1); go func() { defer a.Done() }(); b.Add(1); go func() { defer b.Done() }() }`, 2},
		{"unrelated capture", `func f(wg *sync.WaitGroup, n int) { wg.Add(1); go func() { defer wg.Done(); n++; println(n) }() }`, 1},
		{"nested goroutine", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); go func() { println("work") }() }() }`, 1},
		{"nested recovery", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); defer func() { _ = recover() }() }() }`, 1},
		{"shadowed recover", `func f(wg *sync.WaitGroup) { recover := func() {}; wg.Add(1); go func() { defer wg.Done(); recover() }() }`, 1},
		{"shadowed group unrelated", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); wg := new(sync.WaitGroup); wg.Wait() }() }`, 1},
		{"panic needs manual review", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); panic("boom") }() }`, 1},
		{"wrong count", `func f(wg *sync.WaitGroup) { wg.Add(2); go func() { defer wg.Done() }() }`, 0},
		{"zero count", `func f(wg *sync.WaitGroup) { wg.Add(0); go func() { defer wg.Done() }() }`, 0},
		{"negative count", `func f(wg *sync.WaitGroup) { wg.Add(-1); go func() { defer wg.Done() }() }`, 0},
		{"nonconstant count", `func f(wg *sync.WaitGroup, n int) { wg.Add(n); go func() { defer wg.Done() }() }`, 0},
		{"wrong group", `func f(a, b *sync.WaitGroup) { a.Add(1); go func() { defer b.Done() }() }`, 0},
		{"same name different receiver", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { wg := new(sync.WaitGroup); defer wg.Done() }() }`, 0},
		{"same name wrong object", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer func() { wg := new(sync.WaitGroup); wg.Done() }() }() }`, 0},
		{"nonadjacent", `func f(wg *sync.WaitGroup) { wg.Add(1); println("work"); go func() { defer wg.Done() }() }`, 0},
		{"no defer", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { wg.Done() }() }`, 0},
		{"nonfirst defer", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { println("work"); defer wg.Done() }() }`, 0},
		{"deferred wrapper", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer func() { wg.Done() }() }() }`, 0},
		{"empty body", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() {}() }`, 0},
		{"not goroutine", `func f(wg *sync.WaitGroup) { wg.Add(1); func() { defer wg.Done() }() }`, 0},
		{"bare function", `func f(wg *sync.WaitGroup) { work := func() { defer wg.Done() }; wg.Add(1); go work() }`, 0},
		{"receiver argument", `func f(wg *sync.WaitGroup) { wg.Add(1); go func(wg *sync.WaitGroup) { defer wg.Done() }(wg) }`, 0},
		{"other argument", `func f(wg *sync.WaitGroup) { wg.Add(1); go func(n int) { defer wg.Done(); println(n) }(1) }`, 0},
		{"variadic literal", `func f(wg *sync.WaitGroup) { wg.Add(1); go func(...int) { defer wg.Done() }() }`, 0},
		{"literal returns value", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() int { defer wg.Done(); return 1 }() }`, 0},
		{"extra Done", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); wg.Done() }() }`, 0},
		{"extra Add", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); wg.Add(1) }() }`, 0},
		{"extra Wait", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); wg.Wait() }() }`, 0},
		{"extra deferred Done", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); defer wg.Done() }() }`, 0},
		{"nested Done", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); go func() { wg.Done() }() }() }`, 0},
		{"passed receiver", `func f(wg *sync.WaitGroup, work func(*sync.WaitGroup)) { wg.Add(1); go func() { defer wg.Done(); work(wg) }() }`, 0},
		{"assigned receiver before", `func f(wg *sync.WaitGroup) { wg = new(sync.WaitGroup); wg.Add(1); go func() { defer wg.Done() }() }`, 0},
		{"assigned receiver after", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done() }(); wg = new(sync.WaitGroup) }`, 0},
		{"captured receiver mutation", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); wg = new(sync.WaitGroup) }() }`, 0},
		{"other closure mutation", `func f(wg *sync.WaitGroup) { reset := func() { wg = new(sync.WaitGroup) }; wg.Add(1); go func() { defer wg.Done() }(); reset() }`, 0},
		{"pointed value mutation", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done() }(); *wg = sync.WaitGroup{} }`, 0},
		{"address escapes", `func f(wg *sync.WaitGroup) { alias := &wg; wg.Add(1); go func() { defer wg.Done() }(); *alias = new(sync.WaitGroup) }`, 0},
		{"value address escapes", `func f() { var wg sync.WaitGroup; alias := &wg; wg.Add(1); go func() { defer wg.Done() }(); _ = alias }`, 0},
		{"assigned range receiver", `func f(wg *sync.WaitGroup, groups []*sync.WaitGroup) { for _, wg = range groups { wg.Add(1); go func() { defer wg.Done() }() } }`, 0},
		{"parenthesized assigned range receiver", `func f(wg *sync.WaitGroup, groups []*sync.WaitGroup) { for _, (wg) = range groups { wg.Add(1); go func() { defer wg.Done() }() } }`, 0},
		{"range pointed value mutation", `func f(wg *sync.WaitGroup, groups []sync.WaitGroup) { for _, *wg = range groups { wg.Add(1); go func() { defer wg.Done() }() } }`, 0},
		{"declared range receiver", `func f(groups []*sync.WaitGroup) { for _, wg := range groups { wg.Add(1); go func() { defer wg.Done() }() } }`, 0},
		{"range receiver expression", `func f(groups []*sync.WaitGroup) { for wg := range len(groups) { group := groups[wg]; group.Add(1); go func() { defer group.Done() }() } }`, 1},
		{"direct recover", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); _ = recover() }() }`, 0},
		{"direct deferred recover", `func f(wg *sync.WaitGroup) { wg.Add(1); go func() { defer wg.Done(); defer recover() }() }`, 0},
		{"global", `var wg sync.WaitGroup; func f() { wg.Add(1); go func() { defer wg.Done() }() }`, 0},
		{"field", `type S struct { wg sync.WaitGroup }; func f(s *S) { s.wg.Add(1); go func() { defer s.wg.Done() }() }`, 0},
		{"promoted methods", `type S struct { sync.WaitGroup }; func f(wg *S) { wg.Add(1); go func() { defer wg.Done() }() }`, 0},
		{"wrapper methods", `type S sync.WaitGroup; func (*S) Add(int) {}; func (*S) Done() {}; func f(wg *S) { wg.Add(1); go func() { defer wg.Done() }() }`, 0},
		{"method expression", `func f(wg *sync.WaitGroup) { (*sync.WaitGroup).Add(wg, 1); go func() { defer wg.Done() }() }`, 0},
		{"effectful receiver", `func f(get func() *sync.WaitGroup) { get().Add(1); go func() { defer get().Done() }() }`, 0},
		{"explicit dereference", `func f(wg *sync.WaitGroup) { (*wg).Add(1); go func() { defer (*wg).Done() }() }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newWaitGroupGoFile(), "package p\nimport \"sync\"\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/WaitGroupGo", offense.CopName)
				assert.Equal(t, cop.Convention, offense.Severity)
				assert.Contains(t, offense.Message, "consider wg.Go")
				assert.Contains(t, offense.Message, "panic escape")
				assert.Contains(t, offense.Message, "receiver")
				assert.Contains(t, offense.Message, "captures")
				assert.Contains(t, offense.Message, "timing")
				assert.Equal(t, 3, offense.Pos.Line)
				assert.Greater(t, offense.End.Offset, offense.Pos.Offset)
			}
		})
	}
}

func TestWaitGroupGoAliases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"import alias", `import s "sync"; func f(wg *s.WaitGroup) { wg.Add(1); go func() { defer wg.Done() }() }`},
		{"dot import", `import . "sync"; func f(wg *WaitGroup) { wg.Add(1); go func() { defer wg.Done() }() }`},
		{"value type alias", `import "sync"; type Group = sync.WaitGroup; func f() { var wg Group; wg.Add(1); go func() { defer wg.Done() }() }`},
		{"pointer type alias", `import "sync"; type Group = *sync.WaitGroup; func f(wg Group) { wg.Add(1); go func() { defer wg.Done() }() }`},
		{"assigned pointer alias", `import "sync"; func f(original *sync.WaitGroup) { wg := original; wg.Add(1); go func() { defer wg.Done() }() }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Len(t, runExtractedTypedCop(t, newWaitGroupGoFile(), "package p\n"+tc.src), 1)
		})
	}
}

func TestWaitGroupGoConfiguration(t *testing.T) {
	t.Parallel()
	c := newWaitGroupGoFile()
	require.NotSame(t, c, newWaitGroupGoFile())
	assert.True(t, c.Types)
	assert.True(t, c.NeedsTypes())
	assert.Nil(t, c.Scope)
	assert.Empty(t, c.MinGoVersion)
	assert.Equal(t, "go1.25", c.MinStdlibVersion)
	for _, minimum := range []string{"", "invalid", "go1.24"} {
		assert.Equal(t, "go1.25", newWaitGroupGoFile(cop.WithMinStdlibVersion(minimum)).MinStdlibVersion)
	}
	assert.Equal(t, "go1.30", newWaitGroupGoFile(cop.WithMinStdlibVersion("go1.30")).MinStdlibVersion)
	assert.Empty(t, runExtractedTypedCop(t, newWaitGroupGoFile(cop.WithMinStdlibVersion("go1.30")), waitGroupGoFixture))
	assert.Empty(t, runExtractedTypedCop(t, c, "// Code generated by fixture; DO NOT EDIT.\n"+waitGroupGoFixture))

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "internal/sample_test.go", waitGroupGoFixture, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
		FileVersions: make(map[*ast.File]string),
	}
	cfg := types.Config{Importer: importer.Default()}
	_, err = cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	for _, target := range []string{"", "go1.24", "go1.25", "go1.26"} {
		info.FileVersions[file] = target
		pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info}
		c.Check(pass)
		if target == "" || target == "go1.24" {
			assert.Empty(t, pass.Offenses(), target)
		} else {
			assert.Len(t, pass.Offenses(), 1, target)
		}
	}
	pass := &cop.Pass{Cop: c, FileSet: fset, File: file}
	c.Run(pass)
	assert.Empty(t, pass.Offenses(), "missing types")
	pass.Info = &types.Info{}
	c.Run(pass)
	assert.Empty(t, pass.Offenses(), "incomplete types")
	assert.True(t, newWaitGroupGoFile(cop.WithScope(cop.UnderDir("internal"))).InScope(pass))
	assert.False(t, newWaitGroupGoFile(cop.WithScope(cop.UnderDir("other"))).InScope(pass))
}
