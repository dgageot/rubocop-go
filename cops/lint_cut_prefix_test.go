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

const cutPrefixFixture = `package p
import "strings"
func f(s, prefix string) string {
	if strings.HasPrefix(s, prefix) {
		return strings.TrimPrefix(s, prefix)
	}
	return s
}
`

func TestCutPrefix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"trim", `if strings.HasPrefix(s, prefix) { return strings.TrimPrefix(s, prefix) }`},
		{"slice length", `if strings.HasPrefix(s, prefix) { return s[len(prefix):] }`},
		{"slice constant", `if strings.HasPrefix(s, "abc") { return s[3:] }`},
		{"byte length", `if strings.HasPrefix(s, "é") { return s[2:] }`},
		{"empty prefix", `if strings.HasPrefix(s, "") { return s[0:] }`},
		{"constant expression", `const p = "a" + "bc"; if strings.HasPrefix(s, p) { return s[1+2:] }`},
		{"equivalent constants", `if strings.HasPrefix(s, "abc") { return strings.TrimPrefix(s, ` + "`abc`" + `) }`},
		{"prefix concatenation", `if strings.HasPrefix(s, prefix+"/") { return s[len(prefix+"/"):] }`},
		{"parentheses", `if (strings.HasPrefix((s), (prefix))) { return (s)[len((prefix)):] }`},
		{"assignment", `if strings.HasPrefix(s, prefix) { s = strings.TrimPrefix(s, prefix) }`},
		{"declaration", `if strings.HasPrefix(s, prefix) { rest := strings.TrimPrefix(s, prefix); return rest }`},
		{"var declaration", `if strings.HasPrefix(s, prefix) { var rest = strings.TrimPrefix(s, prefix); return rest }`},
		{"nested call", `if strings.HasPrefix(s, prefix) { return strings.TrimSpace(strings.TrimPrefix(s, prefix)) }`},
		{"receiver call", `var b strings.Builder; if strings.HasPrefix(s, prefix) { b.WriteString(strings.TrimPrefix(s, prefix)); return b.String() }`},
		{"else", `if strings.HasPrefix(s, prefix) { return s[len(prefix):] } else { return s }`},
		{"if init", `if s := prefix; strings.HasPrefix(s, prefix) { return s[len(prefix):] }`},
		{"return guard", `if !strings.HasPrefix(s, prefix) { return s }; return s[len(prefix):]`},
		{"continue guard", `for range 2 { if !strings.HasPrefix(s, prefix) { continue }; return s[len(prefix):] }`},
		{"break guard", `for { if !strings.HasPrefix(s, prefix) { break }; return s[len(prefix):] }`},
		{"switch case", `switch { case strings.HasPrefix(s, prefix): return s[len(prefix):] }`},
		{"switch guard", `switch s { case "x": if !strings.HasPrefix(s, prefix) { return s }; return s[len(prefix):] }`},
		{"select guard", `select { default: if !strings.HasPrefix(s, prefix) { return s }; return s[len(prefix):] }`},
		{"leading condition", `if prefix != "" && strings.HasPrefix(s, prefix) { return s[len(prefix):] }`},
		{"trailing condition", `if strings.HasPrefix(s, prefix) && len(s) > 3 { return s[len(prefix):] }`},
		{"compound guard", `if prefix == "" || !strings.HasPrefix(s, prefix) { return s }; return s[len(prefix):]`},
		{"local init", `if !strings.HasPrefix(s, prefix) { return s }; var n = 0; rest := s[len(prefix):]; _ = n; return rest`},
		{"range input", `if !strings.HasPrefix(s, prefix) { return s }; for part := range strings.SplitSeq(s[len(prefix):], "/") { return part }`},
		{"field", `v := struct{ text string }{s}; if strings.HasPrefix(v.text, prefix) { return v.text[len(prefix):] }`},
		{"closure", `return func() string { if strings.HasPrefix(s, prefix) { return s[len(prefix):] }; return s }()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\nimport \"strings\"\nfunc f(s, prefix string) string { " + tc.src + "; return s }"
			offenses := runCutPrefix(t, src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/CutPrefix", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "strings.CutPrefix")
			assert.Contains(t, offenses[0].Message, "short-circuit evaluation")
		})
	}
	for _, alias := range []string{"text", "."} {
		t.Run("import "+alias, func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(cutPrefixFixture, `import "strings"`, `import `+alias+` "strings"`, 1)
			qualifier := alias + "."
			if alias == "." {
				qualifier = ""
			}
			assert.Len(t, runCutPrefix(t, strings.ReplaceAll(src, "strings.", qualifier)), 1)
		})
	}
}

func TestCutPrefixIgnoresOtherPatterns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"check only", `if strings.HasPrefix(s, prefix) { return s }`},
		{"trim only", `return strings.TrimPrefix(s, prefix)`},
		{"already cut", `if rest, ok := strings.CutPrefix(s, prefix); ok { return rest }`},
		{"different input", `if strings.HasPrefix(s, prefix) { return strings.TrimPrefix(prefix, prefix) }`},
		{"different prefix", `if strings.HasPrefix(s, prefix) { return strings.TrimPrefix(s, "other") }`},
		{"different offset", `if strings.HasPrefix(s, "abc") { return s[2:] }`},
		{"rune offset", `if strings.HasPrefix(s, "é") { return s[1:] }`},
		{"bounded slice", `if strings.HasPrefix(s, "abc") { return s[3:4] }`},
		{"unknown offset", `if strings.HasPrefix(s, prefix) { return s[3:] }`},
		{"input call", `get := func() string { return s }; if strings.HasPrefix(get(), prefix) { return strings.TrimPrefix(get(), prefix) }`},
		{"prefix call", `get := func() string { return prefix }; if strings.HasPrefix(s, get()) { return strings.TrimPrefix(s, get()) }`},
		{"input mutation", `if strings.HasPrefix(s, prefix) { s = "other"; return s[len(prefix):] }`},
		{"prefix mutation", `if strings.HasPrefix(s, prefix) { prefix = "other"; return s[len(prefix):] }`},
		{"input shadow", `if strings.HasPrefix(s, prefix) { s := "other"; return s[len(prefix):] }`},
		{"prefix shadow", `if strings.HasPrefix(s, prefix) { prefix := "other"; return s[len(prefix):] }`},
		{"intervening call", `change := func() { s = "other" }; if strings.HasPrefix(s, prefix) { change(); return s[len(prefix):] }`},
		{"initialization call", `change := func() string { s = "other"; return s }; if strings.HasPrefix(s, prefix) { other := change(); _ = other; return s[len(prefix):] }`},
		{"argument call", `change := func() string { s = "other"; return s }; if strings.HasPrefix(s, prefix) { return strings.ReplaceAll(change(), strings.TrimPrefix(s, prefix), "") }`},
		{"receiver call", `change := func() *strings.Builder { s = "other"; return new(strings.Builder) }; if strings.HasPrefix(s, prefix) { change().WriteString(s[len(prefix):]) }`},
		{"lhs call", `change := func() int { s = "other"; return 0 }; a := make([]string, 1); if strings.HasPrefix(s, prefix) { a[change()] = s[len(prefix):] }`},
		{"deferred removal", `if strings.HasPrefix(s, prefix) { defer func() { _ = s[len(prefix):] }() }`},
		{"captured removal", `if strings.HasPrefix(s, prefix) { f := func() string { return s[len(prefix):] }; s = "other"; return f() }`},
		{"nonterminating guard", `if !strings.HasPrefix(s, prefix) { s = "other" }; return s[len(prefix):]`},
		{"nested break", `if !strings.HasPrefix(s, prefix) { for { break } }; return s[len(prefix):]`},
		{"guard else mutation", `if !strings.HasPrefix(s, prefix) { return s } else { s = "other" }; return s[len(prefix):]`},
		{"negative branch", `if !strings.HasPrefix(s, prefix) { return strings.TrimPrefix(s, prefix) }`},
		{"or condition", `if s != "" || strings.HasPrefix(s, prefix) { return strings.TrimPrefix(s, prefix) }`},
		{"condition call", `change := func() bool { s = "other"; return true }; if strings.HasPrefix(s, prefix) && change() { return s[len(prefix):] }`},
		{"multiple cases", `switch { case s != "", strings.HasPrefix(s, prefix): return strings.TrimPrefix(s, prefix) }`},
		{"fallthrough slice", `switch { case s != "": fallthrough; case strings.HasPrefix(s, prefix): return s[len(prefix):] }`},
		{"labeled fallthrough", `switch { case s != "": if s == "x" { goto next }; next: fallthrough; case strings.HasPrefix(s, prefix): return s[len(prefix):] }`},
		{"fallthrough empty statement", `switch { case s != "": fallthrough; ; case strings.HasPrefix(s, prefix): return s[len(prefix):] }`},
		{"fallthrough trim", `switch { case s != "": fallthrough; case strings.HasPrefix(s, prefix): return strings.TrimPrefix(s, prefix) }`},
		{"constant array range", `if strings.HasPrefix(s, prefix) { for range [1]string{s[len(prefix):]} {} }`},
		{"blank value array range", `if strings.HasPrefix(s, prefix) { for i, _ := range [1]string{s[len(prefix):]} { _ = i } }`},
		{"constant pointer array range", `if strings.HasPrefix(s, prefix) { for range &[1]string{s[len(prefix):]} {} }`},
		{"constant len", `if strings.HasPrefix(s, "abc") { _ = len([1]string{s[3:]}) }`},
		{"tagged switch", `switch false { case strings.HasPrefix(s, prefix): return strings.TrimPrefix(s, prefix) }`},
		{"shadowed package", `strings := struct{ HasPrefix func(string, string) bool; TrimPrefix func(string, string) string }{strings.HasPrefix, strings.TrimPrefix}; if strings.HasPrefix(s, prefix) { return strings.TrimPrefix(s, prefix) }`},
		{"shadowed len", `len := func(string) int { return 2 }; if strings.HasPrefix(s, prefix) { return s[len(prefix):] }`},
		{"multi result arguments", `pair := func() (string, string) { return s, prefix }; if strings.HasPrefix(pair()) { return strings.TrimPrefix(pair()) }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\nimport \"strings\"\nfunc f(s, prefix string) string { " + tc.src + "; return s }"
			assert.Empty(t, runCutPrefix(t, src))
		})
	}
}

func TestCutPrefixUnevaluatedOperand(t *testing.T) {
	t.Parallel()
	assert.Empty(t, runCutPrefix(t, `package p
import ("strings"; "unsafe")
func f(s, prefix string) uintptr {
	if strings.HasPrefix(s, prefix) { return unsafe.Sizeof(strings.TrimPrefix(s, prefix)) }
	return 0
}
func genericSize[T any](s, prefix string) uintptr {
	if strings.HasPrefix(s, prefix) { return unsafe.Sizeof(struct{ X T; S string }{S: strings.TrimPrefix(s, prefix)}) }
	return 0
}
func genericAlign[T any](s, prefix string) uintptr {
	if strings.HasPrefix(s, prefix) { return unsafe.Alignof(struct{ X T; S string }{S: strings.TrimPrefix(s, prefix)}) }
	return 0
}`))
}

func TestCutPrefixProgram(t *testing.T) {
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintCutPrefix(), coptest.ProgramFiles{
		"sample.go":                  cutPrefixFixture,
		"generated/generated.go":     "// Code generated by generator. DO NOT EDIT.\n" + cutPrefixFixture,
		"pkg/config/v15/frozen.go":   cutPrefixFixture,
		"pkg/config/latest/types.go": cutPrefixFixture,
	})
	require.Len(t, offenses, 3)
	for _, offense := range offenses {
		assert.Equal(t, "Lint/CutPrefix", offense.CopName)
		assert.Equal(t, 4, offense.Pos.Line)
	}
}

func runCutPrefix(t *testing.T, src string) []cop.Offense {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	cfg := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
	pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	pass := &cop.Pass{Cop: newCutPrefixFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newCutPrefixFile().Check(pass)
	return pass.Offenses()
}

func TestCutPrefixPreservesNamedStringSlices(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, declaration string
		want              int
	}{
		{"named string", "type Path string", 0},
		{"string alias", "type Path = string", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `package p
import "strings"
` + tc.declaration + `
const name Path = "prefix/file"
func trim() any { if strings.HasPrefix(string(name), "prefix/") { return name[7:] }; return name }
`
			assert.Len(t, runCutPrefix(t, src), tc.want)
		})
	}
}
