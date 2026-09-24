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

const fieldsSeqFixture = `package p
import "strings"
func words(input string) string {
	words := strings.Fields(input)
	if len(words) == 0 { return input }
	var result string
	width := 0
	for _, word := range words { result += word; width++ }
	return result
}
`

func TestFieldsSeq(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"empty fallback", fieldsSeqFixture},
		{"alias import", strings.ReplaceAll(strings.Replace(fieldsSeqFixture, `import "strings"`, `import text "strings"`, 1), "strings.", "text.")},
		{"dot import", strings.ReplaceAll(strings.Replace(fieldsSeqFixture, `import "strings"`, `import . "strings"`, 1), "strings.", "")},
		{"var declaration", strings.Replace(fieldsSeqFixture, "words := strings.Fields(input)", "var words = strings.Fields(input)", 1)},
		{"parentheses", strings.ReplaceAll(fieldsSeqFixture, "range words", "range (words)")},
		{"reversed comparison", strings.Replace(fieldsSeqFixture, "len(words) == 0", "0 == len(words)", 1)},
		{"constant zero", strings.Replace(fieldsSeqFixture, "len(words) == 0", "len(words) == 1-1", 1)},
		{"no fallback", strings.Replace(fieldsSeqFixture, "if len(words) == 0 { return input }", "", 1)},
		{"input evaluation", strings.Replace(fieldsSeqFixture+"\nfunc read(s string) string { return s }\n", "strings.Fields(input)", "strings.Fields(read(input))", 1)},
		{"closure", strings.Replace(fieldsSeqFixture, "func words(input string) string", "var words = func(input string) string", 1)},
		{"switch case", `package p
import "strings"
func f(input string) { switch input { case "x": words := strings.Fields(input); for _, word := range words { _ = word } } }`},
		{"select case", `package p
import "strings"
func f(inputs <-chan string) { select { case input := <-inputs: words := strings.Fields(input); for _, word := range words { _ = word } } }`},
		{"nested recovery unchanged", `package p
import "strings"
func f(input string) { for _, word := range strings.Fields(input) { func() { _ = recover(); _ = word }() } }`},
		{"shadowed recover", `package p
import "strings"
func f(input string) { recover := func() {}; for range strings.Fields(input) { recover() } }`},
		{"direct", `package p
import "strings"
func f(input string) { for _, word := range strings.Fields(input) { _ = word } }`},
		{"bare range", `package p
import "strings"
func f(input string) { for range strings.Fields(input) {} }`},
		{"continue fallback", `package p
import "strings"
func f(inputs []string) []string {
	var output []string
	for _, input := range inputs {
		words := strings.Fields(input)
		if len(words) == 0 { output = append(output, input); continue }
		var current strings.Builder
		width := 0
		for _, word := range words { current.WriteString(word); width++ }
		output = append(output, current.String())
	}
	return output
}`},
		{"shadowed unrelated local", strings.Replace(fieldsSeqFixture, "return result", "{ words := 1; _ = words }; return result", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runFieldsSeq(t, tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/FieldsSeq", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "strings.FieldsSeq")
			assert.Contains(t, offenses[0].Message, "input evaluation")
			assert.Contains(t, offenses[0].Message, "empty-input fallback")
		})
	}
}

func TestFieldsSeqIgnoresOtherUses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"index needed", strings.Replace(fieldsSeqFixture, "for _, word := range words {", "for i, word := range words { _ = i;", 1)},
		{"index only", strings.Replace(fieldsSeqFixture, "for _, word := range words { result += word; width++ }", "for i := range words { width += i }", 1)},
		{"direct index", `package p
import "strings"
func f(input string) { for i := range strings.Fields(input) { _ = i } }`},
		{"index access", strings.Replace(fieldsSeqFixture, "return result", "return words[0] + result", 1)},
		{"slice access", strings.Replace(fieldsSeqFixture, "return result", `return strings.Join(words[1:], "") + result`, 1)},
		{"capacity", strings.Replace(fieldsSeqFixture, "var result string", "buffer := make([]string, 0, len(words)); _ = buffer; var result string", 1)},
		{"count", strings.Replace(fieldsSeqFixture, "return result", "_ = len(words); return result", 1)},
		{"mutated slice", strings.Replace(fieldsSeqFixture, "result += word", `words[0] = "changed"; result += word`, 1)},
		{"reassigned slice", strings.Replace(fieldsSeqFixture, "var result string", `words = strings.Fields("other"); var result string`, 1)},
		{"address taken", strings.Replace(fieldsSeqFixture, "return result", "_ = &words; return result", 1)},
		{"escape", strings.Replace(fieldsSeqFixture+"\nfunc save([]string) {}", "return result", "save(words); return result", 1)},
		{"captured slice", strings.Replace(fieldsSeqFixture, "return result", "defer func() { _ = words }(); return result", 1)},
		{"multiple ranges", strings.Replace(fieldsSeqFixture, "return result", "for _, w := range words { result += w }; return result", 1)},
		{"nested range", strings.Replace(fieldsSeqFixture, "for _, word := range words { result += word; width++ }", "for range 2 { for _, word := range words { result += word; width++ } }", 1)},
		{"captured range", strings.Replace(fieldsSeqFixture, "for _, word := range words { result += word; width++ }", "func() { for _, word := range words { result += word; width++ } }()", 1)},
		{"existing slice", strings.Replace(fieldsSeqFixture, "words := strings.Fields(input)", "var words []string; words = strings.Fields(input)", 1)},
		{"nonempty guard", strings.Replace(fieldsSeqFixture, "len(words) == 0", "len(words) > 0", 1)},
		{"guard init", strings.Replace(fieldsSeqFixture, "if len(words)", "if _ = 0; len(words)", 1)},
		{"guard else", strings.Replace(fieldsSeqFixture, "return input }", "return input } else { _ = input }", 1)},
		{"nonterminating guard", strings.Replace(fieldsSeqFixture, "return input }", "input = input + input }", 1)},
		{"shadowed len", strings.Replace(fieldsSeqFixture, "words :=", "len := func([]string) int { return 0 }; words :=", 1)},
		{"shadowed package", strings.Replace(fieldsSeqFixture, "words :=", "strings := struct { Fields func(string) []string }{strings.Fields}; words :=", 1)},
		{"side effect before range", strings.Replace(fieldsSeqFixture, "width := 0", "width := len(strings.TrimSpace(input))", 1)},
		{"possible panic before range", strings.Replace(fieldsSeqFixture, "width := 0", "width := 1 / len(input)", 1)},
		{"guard binding changes", `package p
import "strings"
func f(input string) string {
	{
		words := strings.Fields(input)
		if len(words) == 0 { return input }
		input := "shadowed"
		for _, word := range words { _ = word }
		return input
	}
}`},
		{"shadowed guard function", `package p
import "strings"
func fallback(s string) string { return s }
func f(input string) string {
	words := strings.Fields(input)
	if len(words) == 0 { return fallback(input) }
	fallback := 0
	for _, word := range words { _ = word }
	_ = fallback
	return ""
}`},
		{"bare return shadowed", `package p
import "strings"
func f(input string) (result string) {
	{
		words := strings.Fields(input)
		if len(words) == 0 { return }
		var result string
		for _, word := range words { result += word }
		return result
	}
}`},
		{"recover direct", `package p
import "strings"
func f() { defer func() { for range strings.Fields("x") { recover() } }(); panic("x") }`},
		{"recover parenthesized", `package p
import "strings"
func f() { defer func() { for range strings.Fields("x") { (recover)() } }(); panic("x") }`},
		{"recover assignment target", `package p
import "strings"
func f() { defer func() { a := []string{""}; for _, a[recover().(int)] = range strings.Fields("x") {} }(); panic(0) }`},
		{"recover local assignment target", `package p
import "strings"
func f() { defer func() { a := []string{""}; words := strings.Fields("x"); for _, a[(recover)().(int)] = range words {} }(); panic(0) }`},
		{"recover local", `package p
import "strings"
func f() { defer func() { words := strings.Fields("x"); for range words { recover() } }(); panic("x") }`},
		{"mutable bytes", `package p
import "bytes"
func f(input []byte) { words := bytes.Fields(input); for _, word := range words { input[0] = ' '; _ = word } }`},
		{"predicate timing", `package p
import "strings"
func f(input string) {
	count := 0
	words := strings.FieldsFunc(input, func(r rune) bool { count++; return r == ' ' })
	for _, word := range words { _ = word; count = 0 }
}`},
		{"already lazy", `package p
import "strings"
func f(input string) { for word := range strings.FieldsSeq(input) { _ = word } }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, runFieldsSeq(t, tc.src))
		})
	}
}

func TestFieldsSeqProgram(t *testing.T) {
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintFieldsSeq(), coptest.ProgramFiles{
		"go.mod":   "module example.test\n\ngo 1.26\n",
		"words.go": fieldsSeqFixture,
	})
	require.Len(t, offenses, 1)
	assert.Equal(t, 4, offenses[0].Pos.Line)
	assert.Equal(t, "Lint/FieldsSeq", offenses[0].CopName)
}

func runFieldsSeq(t *testing.T, src string) []cop.Offense {
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
	pass := &cop.Pass{Cop: newFieldsSeqFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newFieldsSeqFile().Check(pass)
	return pass.Offenses()
}
