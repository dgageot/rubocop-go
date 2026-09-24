package cops

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/runner"
)

func TestNewExpr(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"short declaration", `func f() *int { v := 42; return &v }`},
		{"var declaration", `func f() *int { var v = 42; return &v }`},
		{"grouped declaration", `func f() *int { var (v = 42); return &v }`},
		{"constant expression", `func f() *float64 { v := 1.0 + 2; return &v }`},
		{"typed conversion", `func f() *int64 { v := int64(42); return &v }`},
		{"named type", `type Number int; func f(n Number) *Number { v := n; return &v }`},
		{"generic assertion", `func f[T any](raw any) *T { v := raw.(T); return &v }`},
		{"generic conversion", `func f[T any](raw int64) *T { v := any(int(raw)).(T); return &v }`},
		{"call", `func f(g func() int) *int { v := g(); return &v }`},
		{"receive", `func f(ch <-chan int) *int { v := <-ch; return &v }`},
		{"index", `func f(s []int) *int { v := s[0]; return &v }`},
		{"copy", `func f(p *int) *int { v := *p; return &v }`},
		{"nil pointer", `func f() **int { v := (*int)(nil); return &v }`},
		{"make", `func f(n int) *[]int { v := make([]int, n); return &v }`},
		{"parentheses", `func f() *int { v := (42); return (&(v)) }`},
		{"nested block", `func f() *int { { v := 42; return &v } }`},
		{"switch", `func f(n int) *int { switch n { default: v := 42; return &v } }`},
		{"type switch", `func f(raw any) *int { switch n := raw.(type) { case int: v := n; return &v }; return nil }`},
		{"select", `func f(ch <-chan int) *int { select { case n := <-ch: v := n; return &v } }`},
		{"closure", `var f = func() *int { v := 42; return &v }`},
		{"nil result", `func f() (*int, error) { v := 42; return &v, nil }`},
		{"preceding constant", `func f() (int, *int) { v := 42; return 1, &v }`},
		{"following constant", `const ok = true; func f() (*int, bool) { v := 42; return &v, ok }`},
		{"shadowed local", `func f(v int) *int { { v := v + 1; return &v } }`},
		{"unrelated new shadow", `func f() *int { { new := 1; _ = new }; v := 42; return &v }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runNewExpr(t, "sample.go", "package p\n"+tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/NewExpr", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "new(expr)")
			assert.Contains(t, offenses[0].Message, "initializer unchanged")
		})
	}
}

func TestNewExprIgnoresOtherPatterns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"already new", `func f() *int { return new(42) }`},
		{"explicit type", `func f() *int64 { var v int64 = 42; return &v }`},
		{"interface type", `func f() *any { var v any = 42; return &v }`},
		{"zero declaration", `func f() *int { var v int; return &v }`},
		{"assignment", `func f() *int { v := 0; v = 42; return &v }`},
		{"multiple variables", `func f() (*int, int) { v, n := 42, 1; return &v, n }`},
		{"multiple specs", `func f() (*int, int) { var (v = 42; n = 1); return &v, n }`},
		{"comma ok", `func f(raw any) *int { v, ok := raw.(int); _ = ok; return &v }`},
		{"if initializer", `func f(raw any) *int { if v, ok := raw.(int); ok { return &v }; return nil }`},
		{"for initializer", `func f() *int { for v := 42; ; { return &v } }`},
		{"parameter", `func f(v int) *int { return &v }`},
		{"field address", `func f() *int { v := struct{ n int }{42}; return &v.n }`},
		{"slice element address", `func f(s []int) *int { v := s; return &v[0] }`},
		{"struct literal", `type S struct{ n int }; func f() *S { v := S{42}; return &v }`},
		{"parenthesized literal", `type S struct{ n int }; func f() *S { v := (S{42}); return &v }`},
		{"slice literal", `func f() *[]int { v := []int{42}; return &v }`},
		{"intervening statement", `func f(g func()) *int { v := 42; g(); return &v }`},
		{"capture", `func f() *int { v := 42; defer func() { v++ }(); return &v }`},
		{"unreachable use", `func f() *int { v := 42; return &v; _ = v; return nil }`},
		{"second address", `func f() (*int, *int) { v := 42; return &v, &v }`},
		{"other return use", `func f() (*int, int) { v := 42; return &v, v }`},
		{"capture in result", `func f() (*int, func()) { v := 42; return &v, func() { v++ } }`},
		{"preceding call", `func f(g func() int) (int, *int) { v := g(); return g(), &v }`},
		{"following call", `func f(g func() int) (*int, int) { v := g(); return &v, g() }`},
		{"companion variable", `func f(n int) (*int, int) { v := 42; return &v, n }`},
		{"companion index", `func f(s []int) (*int, int) { v := 42; return &v, s[0] }`},
		{"shadowed nil", `func f(nil error) (*int, error) { v := 42; return &v, nil }`},
		{"shadowed new local", `func f() *int { new := 1; _ = new; v := 42; return &v }`},
		{"shadowed new parameter", `func f(new int) *int { v := 42; return &v }`},
		{"shadowed new result", `func f() (new *int) { v := 42; return &v }`},
		{"shadowed new type parameter", `func f[new any]() *int { v := 42; return &v }`},
		{"shadowed new package", `func f() *int { v := 42; return &v }; var new = 1`},
		{"shadowed new import", `import new "strings"; var _ = new.TrimSpace; func f() *int { v := 42; return &v }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, runNewExpr(t, "sample.go", "package p\n"+tc.src))
		})
	}
}

func TestNewExprScope(t *testing.T) {
	t.Parallel()
	const src = "package p\nfunc f() *int { v := 42; return &v }"
	for _, filename := range []string{"pkg/config/v0/types.go", "pkg/config/v15/types.go"} {
		assert.Len(t, runNewExpr(t, filename, src), 1)
	}
	for _, filename := range []string{"pkg/config/latest/types.go", "pkg/config/version/types.go", "pkg/p/p_test.go"} {
		assert.Len(t, runNewExpr(t, filename, src), 1)
	}
	assert.Empty(t, runNewExpr(t, "generated.go", "// Code generated by generator. DO NOT EDIT.\n"+src))
}

func TestNewExprRunner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod": "module fixture\n\ngo 1.26\n",
		"copy.go": `package p
import "time"
func copyTime(t time.Time) *time.Time {
	v := t
	return &v
}
func assertType[T any](raw any) *T {
	switch raw.(type) {
	default:
		v := raw.(T)
		return &v
	}
}
func suppressed() *int {
	v := 42 //rubocop:disable Lint/NewExpr
	return &v
}`,
		"copy_test.go": `package p
func testHelper() *int { v := 42; return &v }`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	}
	var output bytes.Buffer
	r := runner.New([]cop.Cop{NewLintNewExpr()}, config.DefaultConfig(), &output)
	count, err := r.Run([]string{dir})
	require.NoError(t, err)
	assert.Equal(t, 3, count, output.String())

	require.NoError(t, os.WriteFile(filepath.Join(dir, "shadow.go"), []byte("package p\nvar new = 1"), 0o600))
	count, err = r.Run([]string{dir})
	require.NoError(t, err)
	assert.Zero(t, count, output.String())
}

func TestNewExprUnresolvedImportName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "memory"), 0o700))
	for name, src := range map[string]string{
		"go.mod":           "module fixture\n\ngo 1.26\n",
		"memory/memory.go": "package new\nconst Value = 1\n",
		"copy.go": `package p
import "fixture/memory"
var _ = new.Value
func f() *int { v := 42; return &v }
`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)

	var output bytes.Buffer
	r := runner.New([]cop.Cop{NewLintNewExpr()}, config.DefaultConfig(), &output)
	count, err := r.Run([]string{dir})
	require.NoError(t, err)
	assert.Zero(t, count, output.String())
}

func TestNewExprNumericAssertions(t *testing.T) {
	t.Parallel()
	const src = `package p
func f[T any](raw any) *T {
	switch v := raw.(type) {
	case int:
		switch any(*new(T)).(type) {
		case int: value := any(v).(T); return &value
		case float64: value := any(float64(v)).(T); return &value
		}
	case int64:
		switch any(*new(T)).(type) {
		case int: value := any(int(v)).(T); return &value
		case float64: value := any(float64(v)).(T); return &value
		}
	case uint64:
		switch any(*new(T)).(type) {
		case int: value := any(int(v)).(T); return &value
		case float64: value := any(float64(v)).(T); return &value
		}
	case float64:
		switch any(*new(T)).(type) {
		case int: value := any(int(v)).(T); return &value
		case float64: value := any(v).(T); return &value
		}
	}
	return nil
}`
	assert.Len(t, runNewExpr(t, "sample.go", src), 8)
}

func runNewExpr(t *testing.T, filename, src string) []cop.Offense {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	cfg := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
	pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	pass := &cop.Pass{Cop: NewLintNewExpr(), FileSet: fset, File: file, Info: info, Package: pkg}
	NewLintNewExpr().Check(pass)
	return pass.Offenses()
}
