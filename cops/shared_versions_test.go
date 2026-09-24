package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
)

func TestModernizationLanguageAndAPIVersions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, language, src string
		cop                 *cop.Func
		want                int
	}{
		{"new needs syntax", "go1.25", "package p\nfunc f() *int { n := 1; return &n }", NewLintNewExpr(), 0},
		{"new supports syntax", "go1.26", "package p\nfunc f() *int { n := 1; return &n }", NewLintNewExpr(), 1},
		{"reflect uses module APIs", "go1.23", reflectFieldsFixture, newReflectFieldsFile(), 1},
		{"reflect needs range syntax", "go1.22", reflectFieldsFixture, newReflectFieldsFile(), 0},
		{"fields uses module APIs", "go1.23", fieldsSeqFixture, newFieldsSeqFile(), 1},
		{"fields needs range syntax", "go1.22", fieldsSeqFixture, newFieldsSeqFile(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "p.go", "//go:build "+tc.language+"\n\n"+tc.src, parser.ParseComments)
			require.NoError(t, err)
			info := &types.Info{
				Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
				Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
				FileVersions: make(map[*ast.File]string),
			}
			cfg := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
			pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
			require.NoError(t, err)
			pass := &cop.Pass{Cop: tc.cop, FileSet: fset, File: file, Info: info, Package: pkg}
			assert.Equal(t, tc.language, pass.GoVersion())
			assert.Equal(t, "go1.26", pass.StdlibVersion())
			tc.cop.Check(pass)
			assert.Len(t, pass.Offenses(), tc.want)
		})
	}
}
