package cops

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/coptest"
)

func TestFieldsSeqLookup(t *testing.T) {
	t.Parallel()

	const first = `func f(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 { return "empty" }
	return strings.ToLower(words[0])
}`
	cases := []struct {
		name, imports, body string
		want                bool
	}{
		{"first", `"strings"`, first, true},
		{"var", `"strings"`, strings.Replace(first, "words :=", "var words =", 1), true},
		{"aliased", `text "strings"`, strings.ReplaceAll(first, "strings.", "text."), true},
		{"dot", `. "strings"`, strings.ReplaceAll(first, "strings.", ""), true},
		{"parentheses", `"strings"`, strings.ReplaceAll(strings.ReplaceAll(first, "strings.Fields(s)", "((strings.Fields)(s))"), "words[0]", "(words)[1-1]"), true},
		{"reversed", `"strings"`, strings.Replace(first, "len(words) == 0", "1-1 == len(words)", 1), true},
		{"input_effects", `"strings"`, strings.Replace(first, "strings.Fields(s)", "strings.Fields(read(s))", 1) + `
func read(s string) string { return s }`, true},
		{"assignment", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", "word := words[0]; return word", 1), true},
		{"switch", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `switch words[0] { case "x": return "x" }; return "other"`, 1), true},
		{"if_init", `"strings"`, `func f(s string) string { if words := strings.Fields(s); len(words) > 0 { return words[0] }; return "empty" }`, true},
		{"if_else", `"strings"`, `func f(s string) string { if words := strings.Fields(s); 0 != len(words) { return words[0] } else { return "empty" } }`, true},
		{"if_reversed", `"strings"`, `func f(s string) string { if words := strings.Fields(s); 0 < len(words) { return words[0] }; return "empty" }`, true},
		{"case", `"strings"`, `func f(s string) string { switch s { default: words := strings.Fields(s); if len(words) == 0 { return "empty" }; return words[0] } }`, true},
		{"select", `"strings"`, `func f(c <-chan string) string { select { case s := <-c: words := strings.Fields(s); if len(words) == 0 { return "empty" }; return words[0] } }`, true},
		{"contains", `"strings"; "slices"`, `func f(s, word string) bool { return slices.Contains(strings.Fields(s), word) }`, true},
		{"contains_generic", `text "strings"; list "slices"`, `func f(s string) bool { return list.Contains[[]string, string](text.Fields(s), "x") }`, true},
		{"contains_dot", `text "strings"; . "slices"`, `func f(s string) bool { return Contains(text.Fields(s), "x") }`, true},
		{"contains_effects", `"strings"; "slices"`, `func f(read func() string) bool { return slices.Contains(strings.Fields(read()), read()) }`, true},
		{"shadowed_local", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `word := words[0]; { words := "other"; _ = words }; return word`, 1), true},
		{"unguarded", `"strings"`, `func f(s string) string { return strings.Fields(s)[0] }`, false},
		{"second_field", `"strings"`, strings.Replace(first, "words[0]", "words[1]", 1), false},
		{"count", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `word := words[0]; _ = len(words); return word`, 1), false},
		{"capacity", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `word := words[0]; _ = cap(words); return word`, 1), false},
		{"escaping", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `word := words[0]; _ = strings.Join(words, " "); return word`, 1), false},
		{"slicing", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `return strings.Join(words[:1], " ")`, 1), false},
		{"address", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `ptr := &words[0]; return *ptr`, 1), false},
		{"mutation", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `words[0] = "x"; return "done"`, 1), false},
		{"reassignment", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `words = []string{"x"}; return words[0]`, 1), false},
		{"closure", `"strings"`, strings.Replace(first, "return strings.ToLower(words[0])", `fn := func() string { return words[0] }; return fn()`, 1), false},
		{"shadowed_len", `"strings"`, strings.Replace(first, "words :=", "len := func([]string) int { return 1 }; words :=", 1), false},
		{"shadowed_strings", "", `func f(s string) string { strings := struct{ Fields func(string) []string }{func(s string) []string { return nil }}; words := strings.Fields(s); if len(words) == 0 { return "" }; return words[0] }`, false},
		{"shadowed_slices", `"strings"`, `func f(s string) bool { slices := struct{ Contains func([]string, string) bool }{func([]string, string) bool { return true }}; return slices.Contains(strings.Fields(s), "x") }`, false},
		{"fields_func", `"strings"`, strings.Replace(first, "strings.Fields(s)", "strings.FieldsFunc(s, func(r rune) bool { return r == ' ' })", 1), false},
		{"bytes", `"bytes"`, `func f(s []byte) []byte { words := bytes.Fields(s); if len(words) == 0 { return nil }; return words[0] }`, false},
		{"nonempty_fallback", `"strings"`, strings.Replace(first, "len(words) == 0", "len(words) != 0", 1), false},
		{"exact_count", `"strings"`, strings.Replace(first, "len(words) == 0", "len(words) != 1", 1), false},
		{"nonterminating_guard", `"strings"`, strings.Replace(first, `return "empty"`, `_ = s`, 1), false},
		{"range_owned_by_shared_cop", `"strings"`, `func f(s string) { for _, word := range strings.Fields(s) { _ = word } }`, false},
	}
	files := coptest.ProgramFiles{"go.mod": "module example.test\n\ngo 1.26\n"}
	var want []string
	for _, tc := range cases {
		name := tc.name + "/sample.go"
		imports := ""
		if tc.imports != "" {
			imports = "import (" + tc.imports + ")\n"
		}
		files[name] = "package p\n" + imports + tc.body
		if tc.want {
			want = append(want, tc.name)
		}
	}
	offenses := coptest.RunProgram(t, NewLintFieldsSeqLookup(), files)
	var got []string
	for _, offense := range offenses {
		assert.Equal(t, "Lint/FieldsSeqLookup", offense.CopName)
		assert.Contains(t, offense.Message, "strings.FieldsSeq")
		got = append(got, filepath.Base(filepath.Dir(offense.Pos.Filename)))
	}
	assert.ElementsMatch(t, want, got)
}

func TestFieldsSeqLookupRequiresGo124(t *testing.T) {
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintFieldsSeqLookup(), coptest.ProgramFiles{
		"go.mod": "module example.test\n\ngo 1.23\n",
		"sample.go": `package p
import ("strings"; "slices")
func f(s string) bool { return slices.Contains(strings.Fields(s), "x") }`,
	})
	require.Empty(t, offenses)
}
