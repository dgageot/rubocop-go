package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
)

const constructorCommandExecFixture = `package p
import "os/exec"
func NewCmd() *exec.Cmd { return exec.Command("echo") }`

const fieldsSeqLookupFixture = `package p
import "strings"
func f(s string) string {
 words := strings.Fields(s)
 if len(words) == 0 { return "empty" }
 return words[0]
}`

const slicesCloneFixture = `package p
func f(s []int) []int { return append([]int(nil), s...) }`

const sortStableFuncFixture = `package p
import "sort"
func f(xs []int) { sort.SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] }) }`

func TestExtractedCopFactories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		file func(...cop.FuncOption) *cop.Func
		new  func(...cop.FuncOption) *prog.Func
		src  string
	}{
		{"command", newConstructorCommandExecFile, NewLintConstructorCommandExec, constructorCommandExecFixture},
		{"prefix", newCutPrefixFile, NewLintCutPrefix, cutPrefixFixture},
		{"suffix", newCutSuffixFile, NewLintCutSuffix, cutSuffixFixture},
		{"lookup", newFieldsSeqLookupFile, NewLintFieldsSeqLookup, fieldsSeqLookupFixture},
		{"clone", newSlicesCloneFile, NewLintSlicesClone, slicesCloneFixture},
		{"sort", newSortStableFuncFile, NewLintSortStableFunc, sortStableFuncFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scope := cop.WithScope(cop.Not(cop.UnderDir("legacy")))
			fresh, scoped := tc.file(), tc.file(scope)
			require.NotSame(t, fresh, scoped)
			assert.Nil(t, fresh.Scope)
			assert.True(t, fresh.NeedsTypes())
			assert.Equal(t, fresh.Meta, tc.new().Meta)
			first, second := tc.new(), tc.new()
			require.NotSame(t, first, second)
			if fresh.MinStdlibVersion != "" {
				assert.Empty(t, coptest.Run(t, fresh, tc.src), "unknown types and version")
			}
			files := coptest.ProgramFiles{
				"go.mod":                  "module example.test\n\ngo 1.26\n",
				"sample.go":               tc.src,
				"legacy/sample.go":        tc.src,
				"pkg/config/v0/sample.go": tc.src,
			}
			offenses := coptest.RunProgram(t, tc.new(scope), files)
			require.Len(t, offenses, 2)
			for _, offense := range offenses {
				assert.NotContains(t, filepath.ToSlash(offense.Pos.Filename), "/legacy/")
				assert.Greater(t, offense.End.Offset, offense.Pos.Offset)
			}
		})
	}
}

func TestExtractedPolicyFactories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		new  func(...cop.FuncOption) *cop.Func
		src  string
	}{
		{"command", newConstructorCommandExecFile, `package p; func New() any { return exec.Command("echo") }`},
		{"stdout", NewLintNoStdoutInLibraries, `package p; func f() { fmt.Println("hello") }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scoped := tc.new(cop.WithScope(cop.UnderDir("internal")))
			fresh := tc.new()
			require.NotSame(t, fresh, scoped)
			assert.Nil(t, fresh.Scope)
			assert.Len(t, coptest.RunNamed(t, scoped, "internal/p.go", tc.src), 1)
			assert.Empty(t, coptest.RunNamed(t, scoped, "other/p.go", tc.src))
			assert.Len(t, coptest.RunNamed(t, fresh, "other/p.go", tc.src), 1)
		})
	}
}

func runExtractedTypedCop(t *testing.T, c *cop.Func, src string) []cop.Offense {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	cfg := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
	pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
	c.Check(pass)
	return pass.Offenses()
}
