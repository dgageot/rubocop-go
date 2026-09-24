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

const splitTrimFixture = `package p
import "strings"
func trim(input string) string {
	parts := strings.Split(input, "\n")
	end := len(parts)
	for end > 1 && strings.TrimSpace(parts[end-1]) == "" {
		end--
	}
	return strings.Join(parts[:end], "\n")
}
`

func TestSplitTrimJoin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"cursor", splitTrimFixture},
		{"alias", strings.ReplaceAll(strings.Replace(splitTrimFixture, `import "strings"`, `import text "strings"`, 1), "strings.", "text.")},
		{"dot import", strings.ReplaceAll(strings.Replace(splitTrimFixture, `import "strings"`, `import . "strings"`, 1), "strings.", "")},
		{"constant separator", strings.ReplaceAll(strings.Replace(splitTrimFixture, "func trim", "const newline = \"\\n\"\nfunc trim", 1), `"\n")`, `newline)`)},
		{"equivalent separators", strings.Replace(splitTrimFixture, `parts[:end], "\n"`, `parts[:end], "\x0a"`, 1)},
		{"explicit zero", strings.Replace(splitTrimFixture, "parts[:end]", "parts[0:end]", 1)},
		{"at least two", strings.Replace(splitTrimFixture, "end > 1", "end >= 2", 1)},
		{"post decrement", strings.Replace(splitTrimFixture, "for end > 1 && strings.TrimSpace(parts[end-1]) == \"\" {\n\t\tend--", "for ; end > 1 && strings.TrimSpace(parts[end-1]) == \"\"; end-- {", 1)},
		{"nested predicate", strings.Replace(splitTrimFixture, "strings.TrimSpace(parts[end-1])", "strings.TrimSpace(strings.Trim(parts[end-1], \"x\"))", 1)},
		{"reslicing", `package p
import "strings"
func trim(input string) string {
	parts := strings.Split(input, "/")
	for len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "/")
}`},
		{"reslicing post", `package p
import "strings"
func trim(input string) string {
	parts := strings.Split(input, "/")
	for ; len(parts) > 1 && parts[len(parts)-1] == ""; parts = parts[:len(parts)-1] {}
	return strings.Join(parts, "/")
}`},
		{"closure", strings.Replace(splitTrimFixture, "func trim(input string) string", "var trim = func(input string) string", 1)},
		{"side effect in input", strings.Replace(splitTrimFixture+"\nfunc read(s string) string { return s }\n", "strings.Split(input,", "strings.Split(read(input),", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.src, "strings.CutLast") {
				requireGo127(t)
			}
			t.Parallel()
			offenses := runSplitTrimJoin(t, tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/SplitTrimJoin", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "strings.CutLast")
			assert.Contains(t, offenses[0].Message, "predicate evaluation order")
		})
	}
}

func TestSplitTrimJoinIgnoresOtherPatterns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"different separator", strings.Replace(splitTrimFixture, `parts[:end], "\n"`, `parts[:end], "/"`, 1)},
		{"empty separator", strings.ReplaceAll(splitTrimFixture, `"\n"`, `""`)},
		{"overlapping separator", strings.ReplaceAll(splitTrimFixture, `"\n"`, `"aa"`)},
		{"dynamic separator", strings.ReplaceAll(strings.Replace(splitTrimFixture, "input string", "input, sep string", 1), `"\n"`, "sep")},
		{"full join", strings.Replace(splitTrimFixture, "parts[:end]", "parts", 1)},
		{"skip first part", strings.Replace(splitTrimFixture, "parts[:end]", "parts[1:end]", 1)},
		{"full slice", strings.Replace(splitTrimFixture, "parts[:end]", "parts[:end:end]", 1)},
		{"trim all parts", strings.Replace(splitTrimFixture, "end > 1", "end > 0", 1)},
		{"first part predicate", strings.Replace(splitTrimFixture, "parts[end-1]", "parts[0]", 1)},
		{"cursor in predicate", strings.Replace(splitTrimFixture, "&& strings.TrimSpace", "&& end%2 == 0 && strings.TrimSpace", 1)},
		{"parts escape", strings.Replace(splitTrimFixture, `strings.TrimSpace(parts[end-1])`, `strings.Join(parts, "")`, 1)},
		{"mutation", strings.Replace(splitTrimFixture, "end--", "parts[end-1] = \"\"; end--", 1)},
		{"step changes", strings.Replace(splitTrimFixture, "end--", "end -= 2", 1)},
		{"other use", strings.Replace(splitTrimFixture, "end := len(parts)", "_ = parts; end := len(parts)", 1)},
		{"reassignment", strings.Replace(splitTrimFixture, "end := len(parts)", "parts = strings.Split(input, \"/\"); end := len(parts)", 1)},
		{"captured slice", strings.Replace(splitTrimFixture, `strings.TrimSpace(parts[end-1]) == ""`, `func() bool { return parts[end-1] == "" }()`, 1)},
		{"defer breaks adjacency", strings.Replace(splitTrimFixture, "end := len(parts)", "defer func() { _ = parts }(); end := len(parts)", 1)},
		{"predicate mutates element", strings.Replace(splitTrimFixture+"\nfunc mutate(s *string) bool { *s = \"changed\"; return false }\n", `strings.TrimSpace(parts[end-1]) == ""`, "mutate(&parts[end-1])", 1)},
		{"predicate retains element", strings.Replace(splitTrimFixture+"\nvar saved *string\nfunc retain(s *string) bool { saved = s; return false }\n", `strings.TrimSpace(parts[end-1]) == ""`, "retain(&(parts[end-1]))", 1)},
		{"predicate escapes slice", strings.Replace(splitTrimFixture, `strings.TrimSpace(parts[end-1]) == ""`, `strings.TrimSpace(parts[end-1]) == "" && strings.Join(parts, "") == ""`, 1)},
		{"predicate mutates in closure", strings.Replace(splitTrimFixture, `strings.TrimSpace(parts[end-1]) == ""`, `strings.TrimSpace(parts[end-1]) == "" && func() bool { parts[end-1] = "changed"; return false }()`, 1)},
		{"reslicing predicate mutates element", `package p
import "strings"
func mutate(s *string) bool { *s = "changed"; return false }
func trim(input string) string {
	parts := strings.Split(input, "/")
	for len(parts) > 1 && mutate(&parts[len(parts)-1]) {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "/")
}`},
		{"shadowed strings", strings.Replace(splitTrimFixture, "parts := strings.Split", "strings := struct { Split func(string, string) []string; Join func([]string, string) string; TrimSpace func(string) string }{strings.Split, strings.Join, strings.TrimSpace}\nparts := strings.Split", 1)},
		{"shadowed len", strings.Replace(splitTrimFixture, "parts := strings.Split", "len := func(s []string) int { return 1 }; parts := strings.Split", 1)},
		{"existing slice", strings.Replace(splitTrimFixture, "parts := strings.Split(input, \"\\n\")", "var parts []string; parts = strings.Split(input, \"\\n\")", 1)},
		{"different slice joined", strings.Replace(splitTrimFixture, "parts[:end]", `strings.Split(input, "/")[:end]`, 1)},
		{"different cursor joined", strings.Replace(splitTrimFixture, "parts[:end]", "parts[:len(parts)]", 1)},
		{"return extra value", strings.Replace(strings.Replace(splitTrimFixture, "string) string", "string) (string, int)", 1), `return strings.Join(parts[:end], "\n")`, `return strings.Join(parts[:end], "\n"), end`, 1)},
		{"already using CutLast", `package p
import "strings"
func trim(input string) string {
	for {
		before, last, found := strings.CutLast(input, "\n")
		if !found || strings.TrimSpace(last) != "" { return input }
		input = before
	}
}`},
		{"reslice keeps different suffix", `package p
import "strings"
func trim(input string) string {
	parts := strings.Split(input, "\n")
	for len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[1:]
	}
	return strings.Join(parts, "\n")
}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.src, "strings.CutLast") {
				requireGo127(t)
			}
			t.Parallel()
			assert.Empty(t, runSplitTrimJoin(t, tc.src))
		})
	}
}

func TestSplitTrimJoinProgram(t *testing.T) {
	requireGo127(t)
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintSplitTrimJoin(), coptest.ProgramFiles{
		"go.mod":  "module example.test\n\ngo 1.27\n",
		"trim.go": splitTrimFixture,
	})
	require.Len(t, offenses, 1)
	assert.Equal(t, 4, offenses[0].Pos.Line)
	assert.Equal(t, "Lint/SplitTrimJoin", offenses[0].CopName)
}

// coptest.RunTyped does not resolve imports; this cop needs stdlib identities.
func runSplitTrimJoin(t *testing.T, src string) []cop.Offense {
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
	info.FileVersions = map[*ast.File]string{file: "go1.27"}
	pass := &cop.Pass{Cop: newSplitTrimJoinFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newSplitTrimJoinFile().Check(pass)
	return pass.Offenses()
}
