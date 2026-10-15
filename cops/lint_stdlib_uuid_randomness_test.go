package cops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestUUIDRandomnessEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		source     string
		configured bool
	}{
		{"assign", "reader = other", true},
		{"tuple assign", "other, (reader) = reader, other", true},
		{"range value", "for _, reader = range readers {}", true},
		{"range key", "for reader = range readersByKey {}", true},
		{"address", "retain(&(reader))", true},
		{"read", "other = reader", false},
		{"call", "reader.Read(nil)", false},
		{"same-package shadow", "reader = other; retain(&reader)", false},
		{"shadow", "reader := other; reader = other; retain(&reader)", false},
		{"configuration reference", "_ = configure", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "random.go", "package p\nfunc f() { "+tc.source+" }", 0)
			require.NoError(t, err)
			randPkg := types.NewPackage("crypto/rand", "rand")
			uuidPkg := types.NewPackage("github.com/google/uuid", "uuid")
			reader := types.NewVar(token.NoPos, randPkg, "Reader", types.Typ[types.Int])
			randPkg.Scope().Insert(reader)
			if tc.name == "same-package shadow" {
				reader = types.NewVar(token.NoPos, randPkg, "Reader", types.Typ[types.Int])
			}
			configure := types.NewFunc(token.NoPos, uuidPkg, "SetRand", types.NewSignatureType(nil, nil, nil, nil, nil, false))
			info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
			ast.Inspect(file, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if id.Name == "reader" && tc.name != "shadow" {
					info.Uses[id] = reader
				} else if id.Name == "configure" {
					info.Uses[id] = configure
				}
				return true
			})
			pkg := &packages.Package{Syntax: []*ast.File{file}, TypesInfo: info}
			assert.Equal(t, tc.configured, uuidRandomnessConfigured([]*packages.Package{pkg}))
		})
	}
}
