package cops_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/prog"
)

var (
	defaultFileCops    = []string{"Lint/CloneCompleteness", "Lint/FmtPrint", "Lint/OsExit", "Style/EmptyFunc", "Style/ErrorNaming"}
	defaultProgramCops = []string{"Lint/ContextConnectivity"}
)

func fileNames(list []cop.Cop) []string {
	var names []string
	for _, c := range list {
		names = append(names, c.Name())
	}
	return names
}

func programNames(list []prog.Cop) []string {
	var names []string
	for _, c := range list {
		names = append(names, c.Name())
	}
	return names
}

func TestAllKeepsDefaultCops(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultFileCops, fileNames(cops.All()))
	assert.Equal(t, defaultProgramCops, programNames(cops.AllProgram()))

	first, second := cops.All(), cops.All()
	for i := range first {
		assert.NotSame(t, first[i], second[i], "All must return fresh instances")
	}
	firstProgram, secondProgram := cops.AllProgram(), cops.AllProgram()
	for i := range firstProgram {
		assert.NotSame(t, firstProgram[i], secondProgram[i], "AllProgram must return fresh instances")
	}

	first[0] = nil
	firstProgram[0] = nil
	assert.Equal(t, defaultFileCops, fileNames(cops.All()), "callers may modify the returned slice")
	assert.Equal(t, defaultProgramCops, programNames(cops.AllProgram()))
}

func TestCatalogEntries(t *testing.T) {
	t.Parallel()
	catalog := cops.Catalog()
	require.Len(t, catalog, 48)

	var names, defaultFiles, defaultPrograms, optIn []string
	for _, e := range catalog {
		names = append(names, e.Name)
		require.NotEqual(t, e.File == nil, e.Program == nil, "%s must have exactly one constructor", e.Name)
		if e.File != nil {
			first, second := e.File(), e.File()
			assert.Equal(t, e.Name, first.Name())
			assert.NotSame(t, first, second, "%s must construct fresh instances", e.Name)
			if e.Default {
				defaultFiles = append(defaultFiles, e.Name)
			}
		} else {
			first, second := e.Program(), e.Program()
			assert.Equal(t, e.Name, first.Name())
			assert.NotSame(t, first, second, "%s must construct fresh instances", e.Name)
			if e.Default {
				defaultPrograms = append(defaultPrograms, e.Name)
			}
		}
		if !e.Default {
			optIn = append(optIn, e.Name)
		}
	}
	for i := 1; i < len(names); i++ {
		assert.Less(t, names[i-1], names[i], "catalog names must be unique and in name order")
	}
	assert.Equal(t, defaultFileCops, defaultFiles)
	assert.Equal(t, defaultProgramCops, defaultPrograms)

	doc, err := os.ReadFile("../docs/shared-cops.md")
	require.NoError(t, err)
	var documented []string
	for _, m := range regexp.MustCompile("(?m)^\\| `([^`]+)` \\|").FindAllStringSubmatch(string(doc), -1) {
		documented = append(documented, m[1])
	}
	slices.Sort(documented)
	assert.Equal(t, documented, optIn, "every shared opt-in cop must be selectable, and only those")

	catalog[0].Name = "Changed"
	assert.Equal(t, names[0], cops.Catalog()[0].Name, "Catalog must return a fresh slice")
}

// TestCatalogUsesCanonicalConstructors inspects the package source so a new
// exported constructor cannot ship without a catalog entry. Each canonical
// constructor must appear once, under the field matching its result type.
// Alternate *File constructors of program cops must not appear.
func TestCatalogUsesCanonicalConstructors(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	modes := map[string]string{}
	var catalog *ast.FuncDecl
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if fn.Name.Name == "Catalog" {
				catalog = fn
			}
			if !fn.Name.IsExported() || !strings.HasPrefix(fn.Name.Name, "New") {
				continue
			}
			require.Len(t, fn.Type.Results.List, 1, fn.Name.Name)
			switch result := resultType(fn.Type.Results.List[0].Type); result {
			case "*cop.Func":
				modes[fn.Name.Name] = "File"
			case "*prog.Func":
				modes[fn.Name.Name] = "Program"
			default:
				t.Fatalf("%s returns %s, want *cop.Func or *prog.Func", fn.Name.Name, result)
			}
		}
	}
	require.NotNil(t, catalog)

	used := map[string][]string{}
	ast.Inspect(catalog.Body, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		field, ok := kv.Key.(*ast.Ident)
		if !ok || (field.Name != "File" && field.Name != "Program") {
			return true
		}
		ast.Inspect(kv.Value, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && modes[id.Name] != "" {
				used[id.Name] = append(used[id.Name], field.Name)
			}
			return true
		})
		return false
	})

	canonical := 0
	for name, mode := range modes {
		if base, ok := strings.CutSuffix(name, "File"); ok && modes[base] == "Program" {
			assert.Empty(t, used[name], "alternate constructor %s must not be a catalog entry", name)
			continue
		}
		canonical++
		assert.Equal(t, []string{mode}, used[name], "%s must appear once as a %s entry", name, mode)
	}
	assert.Equal(t, 48, canonical)
	assert.Len(t, used, 48)
}

// resultType formats a *pkg.Name result type, or returns "?" for other shapes.
func resultType(expr ast.Expr) string {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return "?"
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return "?"
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "?"
	}
	return "*" + pkg.Name + "." + sel.Sel.Name
}
