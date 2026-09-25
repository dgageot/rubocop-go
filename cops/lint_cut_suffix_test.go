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

const cutSuffixFixture = `package p
import "strings"
func trim(s, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return strings.TrimSuffix(s, suffix)
	}
	return s
}
`

func TestCutSuffix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"trim", cutSuffixFixture},
		{"alias import", strings.ReplaceAll(strings.Replace(cutSuffixFixture, `import "strings"`, `import text "strings"`, 1), "strings.", "text.")},
		{"dot import", strings.ReplaceAll(strings.Replace(cutSuffixFixture, `import "strings"`, `import . "strings"`, 1), "strings.", "")},
		{"parentheses", cutSuffixSource(`if (strings.HasSuffix((s), (".go"))) { return (s)[(0):(len((s))-3)] }; return s`)},
		{"string alias", `package p
import "strings"
type Path = string
const name Path = "file.go"
func trim() any { if strings.HasSuffix(string(name), ".go") { return name[:len(name)-3] }; return name }`},
		{"length slice", strings.Replace(cutSuffixFixture, "strings.TrimSuffix(s, suffix)", "s[:len(s)-len(suffix)]", 1)},
		{"zero lower bound", strings.Replace(cutSuffixFixture, "strings.TrimSuffix(s, suffix)", "s[0:len(s)-len(suffix)]", 1)},
		{"constant byte count", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return s[:len(s)-3] }; return s`)},
		{"unicode byte count", cutSuffixSource(`if strings.HasSuffix(s, "é") { return s[:len(s)-2] }; return s`)},
		{"empty suffix", cutSuffixSource(`if strings.HasSuffix(s, "") { return s[:len(s)-0] }; return s`)},
		{"equivalent suffixes", cutSuffixSource(`const suffix = "\n"; if strings.HasSuffix(s, suffix) { return strings.TrimSuffix(s, "\x0a") }; return s`)},
		{"constant source", cutSuffixSource(`if strings.HasSuffix("name.go", ".go") { return strings.TrimSuffix("name.go", ".go") }; return s`)},
		{"local declaration", cutSuffixSource(`if strings.HasSuffix(s, ".go") { name := strings.TrimSuffix(s, ".go"); return name }; return s`)},
		{"var declaration", cutSuffixSource(`if strings.HasSuffix(s, ".go") { var name string = s[:len(s)-3]; return name }; return s`)},
		{"outer assignment", cutSuffixSource(`if strings.HasSuffix(s, ".go") { s = strings.TrimSuffix(s, ".go") }; return s`)},
		{"shadowed assignment", cutSuffixSource(`if strings.HasSuffix(s, ".go") { s := strings.TrimSuffix(s, ".go"); return s }; return s`)},
		{"else branch", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return s[:len(s)-3] } else { return s }`)},
		{"else if", cutSuffixSource(`if s == "" { return "empty" } else if strings.HasSuffix(s, ".go") { return s[:len(s)-3] }; return s`)},
		{"strings wrapper", cutSuffixSource(`if strings.HasSuffix(s, ".go") { name := strings.TrimRight(s[:len(s)-3], " \t"); return name }; return s`)},
		{"nested wrapper", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(s, ".go"))) }; return s`)},
		{"multiple results", `package p
import "strings"
func trim(s string) (string, bool) { if strings.HasSuffix(s, ":") { return s[:len(s)-1], true }; return "", false }`},
		{"negative return guard", cutSuffixSource(`if !strings.HasSuffix(s, ".go") { return s }; name := strings.TrimSuffix(s, ".go"); return name`)},
		{"negative guard return", cutSuffixSource(`if !strings.HasSuffix(s, ".go") { return s }; return s[:len(s)-3]`)},
		{"negative guard parentheses", cutSuffixSource(`if (!(strings.HasSuffix(s, ".go"))) { return s }; return s[:len(s)-3]`)},
		{"continue guard", cutSuffixSource(`for _, s := range []string{s} { if !strings.HasSuffix(s, ".go") { continue }; return s[:len(s)-3] }; return s`)},
		{"switch guard", cutSuffixSource(`switch s { default: if !strings.HasSuffix(s, ".go") { return s }; return s[:len(s)-3] }`)},
		{"type switch guard", cutSuffixSource(`switch any(s).(type) { default: if !strings.HasSuffix(s, ".go") { return s }; return s[:len(s)-3] }`)},
		{"select guard", cutSuffixSource(`select { default: if !strings.HasSuffix(s, ".go") { return s }; return s[:len(s)-3] }`)},
		{"closure", strings.Replace(cutSuffixFixture, "func trim(s, suffix string) string", "var trim = func(s, suffix string) string", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runCutSuffix(t, tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/CutSuffix", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "strings.CutSuffix")
			assert.Contains(t, offenses[0].Message, "assignment scope")
			assert.Contains(t, offenses[0].Message, "evaluation order")
		})
	}
}

func TestCutSuffixExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"standalone trim", cutSuffixSource(`return strings.TrimSuffix(s, ".go")`)},
		{"standalone check", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return "yes" }; return s`)},
		{"already cut", cutSuffixSource(`if name, ok := strings.CutSuffix(s, ".go"); ok { return name }; return s`)},
		{"different source", cutSuffixSource(`other := "other.go"; if strings.HasSuffix(s, ".go") { return strings.TrimSuffix(other, ".go") }; return s`)},
		{"different suffix", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return strings.TrimSuffix(s, ".txt") }; return s`)},
		{"different dynamic suffix", strings.Replace(cutSuffixFixture, "strings.TrimSuffix(s, suffix)", "strings.TrimSuffix(s, s)", 1)},
		{"wrong byte count", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return s[:len(s)-2] }; return s`)},
		{"rune count", cutSuffixSource(`if strings.HasSuffix(s, "é") { return s[:len(s)-1] }; return s`)},
		{"different length source", cutSuffixSource(`other := "other.go"; if strings.HasSuffix(s, ".go") { return s[:len(other)-3] }; return s`)},
		{"nonzero lower bound", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return s[1:len(s)-3] }; return s`)},
		{"missing high bound", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return s[:] }; return s`)},
		{"unknown slice size", strings.Replace(cutSuffixFixture, "strings.TrimSuffix(s, suffix)", "s[:len(s)-1]", 1)},
		{"shadowed len", cutSuffixSource(`len := func(string) int { return 4 }; if strings.HasSuffix(s, ".go") { return s[:len(s)-3] }; return s`)},
		{"shadowed strings", cutSuffixSource(`strings := struct { HasSuffix func(string, string) bool; TrimSuffix func(string, string) string }{strings.HasSuffix, strings.TrimSuffix}; if strings.HasSuffix(s, ".go") { return strings.TrimSuffix(s, ".go") }; return s`)},
		{"function input", cutSuffixSource(`read := func() string { return s }; if strings.HasSuffix(read(), ".go") { return strings.TrimSuffix(read(), ".go") }; return s`)},
		{"function suffix", cutSuffixSource(`suffix := func() string { return ".go" }; if strings.HasSuffix(s, suffix()) { return strings.TrimSuffix(s, suffix()) }; return s`)},
		{"field input", cutSuffixSource(`v := struct{ name string }{s}; if strings.HasSuffix(v.name, ".go") { return strings.TrimSuffix(v.name, ".go") }; return s`)},
		{"index input", cutSuffixSource(`v := []string{s}; if strings.HasSuffix(v[0], ".go") { return strings.TrimSuffix(v[0], ".go") }; return s`)},
		{"pointer input", cutSuffixSource(`p := &s; if strings.HasSuffix(*p, ".go") { return strings.TrimSuffix(*p, ".go") }; return s`)},
		{"global input", cutSuffixSource(`if strings.HasSuffix(global, ".go") { return strings.TrimSuffix(global, ".go") }; return s`) + "\nvar global string\n"},
		{"global suffix", cutSuffixSource(`if strings.HasSuffix(s, global) { return strings.TrimSuffix(s, global) }; return s`) + "\nvar global string\n"},
		{"mutation", cutSuffixSource(`if strings.HasSuffix(s, ".go") { s = "other.go"; return strings.TrimSuffix(s, ".go") }; return s`)},
		{"intervening effect", cutSuffixSource(`if strings.HasSuffix(s, ".go") { println(s); return strings.TrimSuffix(s, ".go") }; return s`)},
		{"captured removal", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return func() string { return strings.TrimSuffix(s, ".go") }() }; return s`)},
		{"conditional removal", cutSuffixSource(`if strings.HasSuffix(s, ".go") { if len(s) > 3 { return strings.TrimSuffix(s, ".go") } }; return s`)},
		{"compound condition", cutSuffixSource(`if len(s) > 3 && strings.HasSuffix(s, ".go") { return strings.TrimSuffix(s, ".go") }; return s`)},
		{"or condition", cutSuffixSource(`if strings.HasSuffix(s, ".go") || s == "other" { return strings.TrimSuffix(s, ".go") }; return s`)},
		{"initializer", cutSuffixSource(`if s := s; strings.HasSuffix(s, ".go") { return strings.TrimSuffix(s, ".go") }; return s`)},
		{"unrelated nested trim", cutSuffixSource(`if strings.HasSuffix(s, ".go") { return strings.TrimSuffix(strings.TrimSpace(s), ".go") }; return s`)},
		{"overlapping delimiters", cutSuffixSource(`if strings.HasPrefix(s, "x") && strings.HasSuffix(s, "x") { return strings.TrimSuffix(strings.TrimPrefix(s, "x"), "x") }; return s`)},
		{"effectful wrapper argument", cutSuffixSource(`change := func() string { s = "changed"; return " " }; if strings.HasSuffix(s, ".go") { return strings.TrimRight(s[:len(s)-3], change()) }; return s`)},
		{"effectful assignment target", cutSuffixSource(`v := []string{s}; index := func() int { s = "changed"; return 0 }; if strings.HasSuffix(s, ".go") { v[index()] = strings.TrimSuffix(s, ".go") }; return s`)},
		{"multiple assignment", cutSuffixSource(`if strings.HasSuffix(s, ".go") { s, _ = strings.TrimSuffix(s, ".go"), 0 }; return s`)},
		{"effectful companion result", `package p
import "strings"
func trim(s string) (string, int) { change := func() int { s = "changed"; return 0 }; if strings.HasSuffix(s, ".go") { return s[:len(s)-3], change() }; return s, 0 }`},
		{"bare return", `package p
import "strings"
func trim(s string) (result string) { if strings.HasSuffix(s, ".go") { return }; return s }`},
		{"strings method", cutSuffixSource(`replacer := strings.NewReplacer("x", "y"); if strings.HasSuffix(s, ".go") { return replacer.Replace(strings.TrimSuffix(s, ".go")) }; return s`)},
		{"negative body trim", cutSuffixSource(`if !strings.HasSuffix(s, ".go") { return strings.TrimSuffix(s, ".go") }; return s`)},
		{"negative nonterminating", cutSuffixSource(`if !strings.HasSuffix(s, ".go") { println(s) }; return strings.TrimSuffix(s, ".go")`)},
		{"negative intervening effect", cutSuffixSource(`if !strings.HasSuffix(s, ".go") { return s }; println(s); return strings.TrimSuffix(s, ".go")`)},
		{"negative else", cutSuffixSource(`if !strings.HasSuffix(s, ".go") { return s } else { s = "other" }; return strings.TrimSuffix(s, ".go")`)},
		{"negative initializer", cutSuffixSource(`if println(); !strings.HasSuffix(s, ".go") { return s }; return strings.TrimSuffix(s, ".go")`)},
		{"negative compound guard", cutSuffixSource(`if s == "" || !strings.HasSuffix(s, ".go") { return s }; return strings.TrimSuffix(s, ".go")`)},
		{"break guard", cutSuffixSource(`for { if !strings.HasSuffix(s, ".go") { break }; return strings.TrimSuffix(s, ".go") }; return s`)},
		{"labelled continue", cutSuffixSource(`outer: for { if !strings.HasSuffix(s, ".go") { continue outer }; return strings.TrimSuffix(s, ".go") }`)},
		{"shadowed source", cutSuffixSource(`if strings.HasSuffix(s, ".go") { s := "other"; return strings.TrimSuffix(s, ".go") }; return s`)},
		{"named string slice", `package p
import "strings"
type Path string
const name Path = "file.go"
func trim() any { if strings.HasSuffix(string(name), ".go") { return name[:len(name)-3] }; return name }`},
		{"byte slice", `package p
import "bytes"
func trim(s []byte) []byte { if bytes.HasSuffix(s, []byte(".go")) { return bytes.TrimSuffix(s, []byte(".go")) }; return s }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, runCutSuffix(t, tc.src))
		})
	}
}

func TestCutSuffixProgram(t *testing.T) {
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintCutSuffix(), coptest.ProgramFiles{
		"trim.go":                    cutSuffixFixture,
		"generated/trim.go":          "// Code generated by fixture. DO NOT EDIT.\n" + cutSuffixFixture,
		"pkg/config/v15/trim.go":     cutSuffixFixture,
		"pkg/config/latest/trim.go":  cutSuffixFixture,
		"pkg/config/version/trim.go": cutSuffixFixture,
	})
	require.Len(t, offenses, 4)
	for _, offense := range offenses {
		assert.Equal(t, "Lint/CutSuffix", offense.CopName)
		assert.Equal(t, 4, offense.Pos.Line)
	}
}

func cutSuffixSource(body string) string {
	return "package p\nimport \"strings\"\nfunc trim(s string) string { " + body + " }"
}

func runCutSuffix(t *testing.T, src string) []cop.Offense {
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
	pass := &cop.Pass{Cop: newCutSuffixFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newCutSuffixFile().Check(pass)
	return pass.Offenses()
}
