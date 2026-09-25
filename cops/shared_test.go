package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"go/version"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

func TestSharedCopMetadata(t *testing.T) {
	t.Parallel()
	fileCops := []cop.Cop{
		NewLintSlogContextual(), NewLintConstructorPurity(), NewLintConstructorNetworkIO(),
		NewLintWrapErrors(), NewLintErrorStringMatching(), NewLintDeferMutexUnlock(), NewLintNewExpr(),
		NewLintContextFirstParameter(), NewLintNoContextField(), NewLintHTTPRequestWithContext(), NewLintNoFatalOutsideMain(),
		NewLintNoStdoutInLibraries(),
	}
	programCops := []prog.Cop{
		NewLintPointerHelper(), NewLintReflectFields(), NewLintStdlibUUID(), NewLintURLClone(),
		NewLintJSONMarshalWrite(), NewLintBenchmarkLoop(), NewLintSplitTrimJoin(), NewLintFieldsSeq(), NewLintStreamCloseSafety(),
		NewLintConstructorCommandExec(), NewLintCutPrefix(), NewLintCutSuffix(), NewLintFieldsSeqLookup(), NewLintSlicesClone(), NewLintSortStableFunc(),
	}
	doc, err := os.ReadFile("../docs/shared-cops.md")
	require.NoError(t, err)
	names := make(map[string]bool)
	check := func(name, description string) {
		t.Helper()
		assert.False(t, names[name], "duplicate cop ID: %s", name)
		names[name] = true
		assert.True(t, strings.HasPrefix(name, "Lint/"))
		assert.NotEmpty(t, description)
		assert.LessOrEqual(t, len(description), 100)
		assert.Contains(t, string(doc), "| `"+name+"` | "+description+" |")
	}
	for _, c := range fileCops {
		check(c.Name(), c.Description())
	}
	for _, c := range programCops {
		check(c.Name(), c.Description())
	}
	assert.Len(t, names, 27)
	for _, c := range All() {
		assert.False(t, names[c.Name()], "shared cops are explicitly selected, not enabled by default")
	}
	for _, c := range AllProgram() {
		assert.False(t, names[c.Name()])
	}
}

func TestModernizationMinimumVersions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, minimum, filename, src string
		new                          func(...cop.FuncOption) *cop.Func
	}{
		{"new", "go1.26", "p.go", "package p\nfunc f() *int { n := 1; return &n }", NewLintNewExpr},
		{"pointer", "go1.26", "p.go", "", newPointerHelperFile},
		{"reflect", "go1.26", "p.go", reflectFieldsFixture, newReflectFieldsFile},
		{"uuid", "go1.27", "p.go", "", newStdlibUUIDFile},
		{"url", "go1.27", "p.go", urlCloneFixture, newURLCloneFile},
		{"json", "go1.27", "p.go", jsonMarshalWriteFixture, newJSONMarshalWriteFile},
		{"benchmark", "go1.24", "p_test.go", benchmarkLoopFixture, newBenchmarkLoopFile},
		{"split", "go1.27", "p.go", splitTrimFixture, newSplitTrimJoinFile},
		{"fields", "go1.24", "p.go", fieldsSeqFixture, newFieldsSeqFile},
		{"prefix", "go1.20", "p.go", cutPrefixFixture, newCutPrefixFile},
		{"suffix", "go1.20", "p.go", cutSuffixFixture, newCutSuffixFile},
		{"lookup", "go1.24", "p.go", fieldsSeqLookupFixture, newFieldsSeqLookupFile},
		{"clone", "go1.21", "p.go", slicesCloneFixture, newSlicesCloneFile},
		{"sort", "go1.21", "p.go", sortStableFuncFixture, newSortStableFuncFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.new()
			minimum := c.MinStdlibVersion
			if minimum == "" {
				minimum = c.MinGoVersion
			}
			assert.Equal(t, tc.minimum, minimum)
			lowered := tc.new(cop.WithMinGoVersion("go1.20"), cop.WithMinStdlibVersion("go1.20"), cop.WithScope(cop.And()))
			assert.GreaterOrEqual(t, version.Compare(lowered.MinGoVersion, c.MinGoVersion), 0)
			assert.GreaterOrEqual(t, version.Compare(lowered.MinStdlibVersion, c.MinStdlibVersion), 0)
			assert.Equal(t, "go1.30", tc.new(cop.WithMinGoVersion("go1.30")).MinGoVersion)
			if tc.src == "" {
				return // Imported dependency cases are covered by program tests.
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.filename, tc.src, parser.ParseComments)
			require.NoError(t, err)
			info := &types.Info{
				Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
				Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
				FileVersions: make(map[*ast.File]string),
			}
			cfg := types.Config{Importer: importer.Default()}
			pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
			require.NoError(t, err)
			for _, target := range []string{"", "go1.19", "go1.20", "go1.21", "go1.23", tc.minimum, "go1.28"} {
				info.FileVersions[file] = target
				pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
				c.Check(pass)
				if target == "" || version.Compare(target, tc.minimum) < 0 {
					assert.Empty(t, pass.Offenses(), target)
				} else {
					assert.Len(t, pass.Offenses(), 1, target)
				}
			}
		})
	}
}

func requireGo127(t *testing.T) {
	t.Helper()
	if version.Compare(runtime.Version(), "go1.27") < 0 {
		t.Skip("fixture requires Go 1.27")
	}
}
