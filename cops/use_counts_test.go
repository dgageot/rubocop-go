package cops

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
)

func TestFileUseCounter(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	first, err := parser.ParseFile(fset, "first.go", `package p
var shared int
func f() {
	first := shared
	_ = first
	second := shared
	_ = func() int { return first + second + shared }
	_ = second
}
`, 0)
	require.NoError(t, err)
	second, err := parser.ParseFile(fset, "second.go", "package p\nvar other = shared\n", 0)
	require.NoError(t, err)
	info := &types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	pkg, err := (&types.Config{}).Check("p", fset, []*ast.File{first, second}, info)
	require.NoError(t, err)
	count := fileUseCounter(&cop.Pass{File: first, Info: info})
	var locals []types.Object
	for id, obj := range info.Defs {
		if id.Name == "first" || id.Name == "second" {
			locals = append(locals, obj)
		}
	}
	require.Len(t, locals, 2)
	for _, obj := range locals {
		require.Equal(t, 2, count(obj))
	}
	require.Equal(t, 3, count(pkg.Scope().Lookup("shared")))
	require.Equal(t, 0, count(pkg.Scope().Lookup("other")))
	countOther := fileUseCounter(&cop.Pass{File: second, Info: info})
	require.Equal(t, 1, countOther(pkg.Scope().Lookup("shared")))
}

func TestLazyUseCountsKeepAllCandidates(t *testing.T) {
	t.Parallel()
	_, encode, found := strings.Cut(jsonMarshalWriteFixture, "func encode")
	require.True(t, found)
	for _, tc := range []struct {
		name   string
		check  *cop.Func
		source string
	}{
		{
			name: "fields range", check: newFieldsSeqFile(),
			source: `package p
import "strings"
func first(s string) { words := strings.Fields(s); for _, word := range words { _ = word } }
func rejected(s string) { words := strings.Fields(s); for _, word := range words { _ = word }; _ = func() []string { return words } }
func last(s string) { words := strings.Fields(s); for _, word := range words { _ = word } }
`,
		},
		{
			name: "first field", check: newFieldsSeqLookupFile(),
			source: `package p
import "strings"
func first(s string) string { words := strings.Fields(s); if len(words) == 0 { return "" }; return words[0] }
func rejected(s string) string { words := strings.Fields(s); if len(words) == 0 { return "" }; word := words[0]; _ = func() []string { return words }; return word }
func last(s string) string { words := strings.Fields(s); if len(words) == 0 { return "" }; return words[0] }
`,
		},
		{
			name: "json buffer", check: newJSONMarshalWriteFile(),
			source: jsonMarshalWriteFixture + "func second" + encode + `
func rejected(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(func() any { _ = buf.Len(); return value }()); err != nil { return "", err }
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "sample.go", tc.source, 0)
			require.NoError(t, err)
			info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection)}
			pkg, err := (&types.Config{GoVersion: "go1.26", Importer: importer.Default()}).Check("p", fset, []*ast.File{file}, info)
			require.NoError(t, err)
			info.FileVersions = map[*ast.File]string{file: "go1.27"}
			pass := &cop.Pass{Cop: tc.check, FileSet: fset, File: file, Info: info, Package: pkg}
			tc.check.Check(pass)
			require.Len(t, pass.Offenses(), 2)
		})
	}
}

func BenchmarkUseCountsWithoutCandidates(b *testing.B) {
	fset := token.NewFileSet()
	var source strings.Builder
	source.WriteString("package p\n")
	for i := range 100 {
		fmt.Fprintf(&source, "func f%d(v int) int { a := v + v; b := a + v; return a + b + v }\n", i)
	}
	file, err := parser.ParseFile(fset, "sample.go", source.String(), 0)
	require.NoError(b, err)
	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	pkg, err := (&types.Config{GoVersion: "go1.26"}).Check("p", fset, []*ast.File{file}, info)
	require.NoError(b, err)
	info.FileVersions = map[*ast.File]string{file: "go1.27"}
	for _, check := range []*cop.Func{newFieldsSeqFile(), newFieldsSeqLookupFile(), newJSONMarshalWriteFile()} {
		b.Run(check.Name(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				pass := &cop.Pass{Cop: check, FileSet: fset, File: file, Info: info, Package: pkg}
				check.Check(pass)
			}
		})
	}
}
