package cop_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
)

func TestTypedFilesProvider(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pass *cop.Pass
	}{
		{name: "unavailable", pass: &cop.Pass{}},
		{name: "untyped", pass: syntheticTypedFilesPass(t, "package p")},
		{name: "typed", pass: passWithVersion(t, "go1.23", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pass := tc.pass
			pass.Cop = newProbe(nil)
			pass.SeverityOverride = new(cop.Error)
			calls := 0
			want := []*cop.Pass{{}, {}}
			pass.SetTypedFiles(func() ([]*cop.Pass, error) {
				calls++
				return want, nil
			})
			assert.Zero(t, calls, "setting the provider must not call it")
			views, err := pass.TypedFiles()
			require.NoError(t, err)
			require.Len(t, views, len(want))
			assert.Same(t, &want[0], &views[0], "forward the provider's slice unchanged")
			assert.Nil(t, views[0].Cop)
			assert.Nil(t, views[0].SeverityOverride)
			assert.Equal(t, 1, calls)

			failure := errors.New("load failed")
			pass.SetTypedFiles(func() ([]*cop.Pass, error) { return want, failure })
			views, err = pass.TypedFiles()
			assert.Same(t, failure, err)
			require.Len(t, views, len(want))
			assert.Same(t, &want[0], &views[0])

			pass.SetTypedFiles(func() ([]*cop.Pass, error) { return nil, failure })
			views, err = pass.TypedFiles()
			assert.Same(t, failure, err)
			assert.Nil(t, views)

			pass.SetTypedFiles(func() ([]*cop.Pass, error) { return nil, nil })
			views, err = pass.TypedFiles()
			require.NoError(t, err)
			assert.Nil(t, views)

			pass.SetTypedFiles(func() ([]*cop.Pass, error) { return []*cop.Pass{}, nil })
			views, err = pass.TypedFiles()
			require.NoError(t, err)
			assert.NotNil(t, views)
			assert.Empty(t, views)

			pass.SetTypedFiles(nil)
			views, err = pass.TypedFiles()
			if tc.name == "unavailable" {
				require.EqualError(t, err, "typed file views unavailable")
				assert.Nil(t, views)
			} else {
				require.NoError(t, err)
				require.Len(t, views, 1)
				assert.Same(t, pass.File, views[0].File)
			}
		})
	}
}

func TestTypedFilesUnavailable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pass cop.Pass
	}{
		{name: "empty"},
		{name: "no file", pass: cop.Pass{FileSet: token.NewFileSet()}},
		{name: "no file set", pass: cop.Pass{File: &ast.File{}}},
		{name: "info only", pass: cop.Pass{Info: &types.Info{}}},
		{name: "package only", pass: cop.Pass{Package: types.NewPackage("p", "p")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			views, err := tc.pass.TypedFiles()
			require.EqualError(t, err, "typed file views unavailable")
			assert.Nil(t, views)
		})
	}
}

func TestTypedFilesSynthetic(t *testing.T) {
	t.Parallel()
	pass := syntheticTypedFilesPass(t, `package p
import "strings"
type Box[T any] struct { Value T }
func (b Box[T]) Get() T { return b.Value }
func Identity[T any](v T) T { return v }
var Text = strings.TrimSpace(" hi ")
var Answer = Identity(42)
func Use() int {
	box := Box[int]{Value: Answer}
	return box.Get()
}
`)
	pass.Cop = newProbe(nil)
	pass.SeverityOverride = new(cop.Error)
	views, err := pass.TypedFiles()
	require.NoError(t, err)
	require.Len(t, views, 1)
	view := views[0]
	assert.NotSame(t, pass, view)
	assert.Same(t, pass.FileSet, view.FileSet)
	assert.Same(t, pass.File, view.File)
	assert.Same(t, pass.Cop, view.Cop)
	assert.Same(t, pass.SeverityOverride, view.SeverityOverride)
	assert.Nil(t, pass.Info)
	assert.Nil(t, pass.Package)
	assert.NoDirExists(t, filepath.Dir(pass.Filename()), "the synthetic source path must not be created")
	require.NotNil(t, view.Info)
	require.NotNil(t, view.Package)
	assert.Equal(t, "p", view.Package.Name())
	assert.Empty(t, view.Package.GoVersion())
	assert.Empty(t, view.GoVersion())
	assert.Empty(t, view.StdlibVersion())
	assert.Contains(t, view.Info.FileVersions, pass.File)
	assert.NotEmpty(t, view.Info.Types)
	assert.NotEmpty(t, view.Info.Defs)
	assert.NotEmpty(t, view.Info.Uses)
	assert.Contains(t, view.Info.Scopes, pass.File)
	require.Contains(t, view.Info.Implicits, pass.File.Imports[0])
	assert.Equal(t, "strings", view.Info.Implicits[pass.File.Imports[0]].Name())
	require.Len(t, view.Info.InitOrder, 2)
	assert.Equal(t, "Text", view.Info.InitOrder[0].Lhs[0].Name())
	assert.Equal(t, "Answer", view.Info.InitOrder[1].Lhs[0].Name())

	selectors, instances, defs, uses := 0, 0, 0, 0
	for node := range ast.Preorder(pass.File) {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			selectors++
			obj := view.Info.Uses[node.Sel]
			require.NotNil(t, obj)
			assert.Equal(t, node.Sel.Name, obj.Name())
			if node.Sel.Name == "TrimSpace" {
				assert.Equal(t, "strings", obj.Pkg().Path())
			} else {
				require.Contains(t, view.Info.Selections, node)
				assert.Same(t, obj, view.Info.Selections[node].Obj())
			}
		case *ast.Ident:
			if instance, ok := view.Info.Instances[node]; ok {
				instances++
				if node.Name == "Identity" {
					assert.Equal(t, types.Typ[types.Int], instance.TypeArgs.At(0))
					assert.Equal(t, "func(v int) int", instance.Type.String())
				}
			}
			if node.Name == "Answer" {
				if obj := view.Info.Defs[node]; obj != nil {
					defs++
					assert.Same(t, view.Package.Scope().Lookup("Answer"), obj)
					assert.Equal(t, types.Typ[types.Int], obj.Type())
				} else {
					uses++
					assert.Same(t, view.Package.Scope().Lookup("Answer"), view.Info.Uses[node])
				}
			}
		}
	}
	assert.Equal(t, 3, selectors)
	assert.Equal(t, 3, instances)
	assert.Equal(t, 1, defs)
	assert.Equal(t, 1, uses)
	pass.ReportOffense(cop.NewOffense(view.Cop, view.FileSet, view.File, "typed diagnostic"))
	require.Len(t, pass.Offenses(), 1)
	assert.Equal(t, cop.Error, pass.Offenses()[0].Severity)
	assert.Nil(t, view.Offenses())
}

func TestTypedFilesReusesTypedPass(t *testing.T) {
	t.Parallel()
	pass := syntheticTypedFilesPass(t, "package p; var Answer = Sibling")
	sibling, err := parser.ParseFile(pass.FileSet, "sibling.go", "package p; var Sibling = 42", parser.SkipObjectResolution)
	require.NoError(t, err)
	pass.Info = &types.Info{Uses: make(map[*ast.Ident]types.Object)}
	cfg := types.Config{GoVersion: "go1.23"}
	pass.Package, err = cfg.Check("example.com/p", pass.FileSet, []*ast.File{pass.File, sibling}, pass.Info)
	require.NoError(t, err)
	pass.Cop = newProbe(nil)
	pass.SeverityOverride = new(cop.Error)
	pass.Report(pass.File, "existing diagnostic")

	views, err := pass.TypedFiles()
	require.NoError(t, err)
	require.Len(t, views, 1)
	view := views[0]
	assert.NotSame(t, pass, view)
	assert.Same(t, pass.FileSet, view.FileSet)
	assert.Same(t, pass.File, view.File)
	assert.Same(t, pass.Info, view.Info)
	assert.Same(t, pass.Package, view.Package)
	assert.Same(t, pass.Cop, view.Cop)
	assert.Same(t, pass.SeverityOverride, view.SeverityOverride)
	assert.Nil(t, view.Offenses())
	assert.Equal(t, "go1.23", view.GoVersion())
	assert.Equal(t, "example.com/p", view.Package.Path())

	views, err = pass.TypedFiles()
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.NotSame(t, view, views[0], "each call returns a fresh pass")
	assert.Same(t, view.Info, views[0].Info)
	assert.Len(t, pass.Offenses(), 1)
}

func TestTypedFilesPartialData(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		info *types.Info
		pkg  *types.Package
	}{
		{name: "info only", info: &types.Info{}},
		{name: "package only", pkg: types.NewPackage("original", "original")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pass := syntheticTypedFilesPass(t, "package p; var Answer = 42")
			pass.Info, pass.Package = tc.info, tc.pkg
			views, err := pass.TypedFiles()
			require.NoError(t, err)
			require.Len(t, views, 1)
			require.NotNil(t, views[0].Info)
			require.NotNil(t, views[0].Package)
			assert.NotSame(t, tc.info, views[0].Info)
			assert.NotSame(t, tc.pkg, views[0].Package)
			assert.Equal(t, tc.info, pass.Info)
			assert.Equal(t, tc.pkg, pass.Package)
			assert.Equal(t, types.Typ[types.Int], views[0].Package.Scope().Lookup("Answer").Type())
		})
	}
}

func TestTypedFilesToleratesTypeErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{name: "declaration", src: "var Broken = missing()"},
		{name: "body", src: `func Broken() int { return "bad" }`},
		{name: "import", src: `import "example.invalid/missing"; var Broken = missing.Value`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pass := syntheticTypedFilesPass(t, "package p\n"+tc.src+"\nvar Good = 42\nfunc Use() int { return Good }")
			views, err := pass.TypedFiles()
			require.NoError(t, err)
			require.Len(t, views, 1)
			view := views[0]
			require.NotNil(t, view.Info)
			require.NotNil(t, view.Package)
			good := view.Package.Scope().Lookup("Good")
			require.NotNil(t, good)
			assert.Equal(t, types.Typ[types.Int], good.Type())
			result := view.FuncDecl("Use").Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.Ident)
			assert.Same(t, good, view.Info.Uses[result])
			assert.Equal(t, types.Typ[types.Int], view.Info.TypeOf(result))
		})
	}
}

func TestTypedFilesConcurrentFallback(t *testing.T) {
	t.Parallel()
	pass := syntheticTypedFilesPass(t, `package p; import "strings"; var Text = strings.TrimSpace(" hi ")`)
	for range 8 {
		t.Run("caller", func(t *testing.T) {
			t.Parallel()
			views, err := pass.TypedFiles()
			require.NoError(t, err)
			require.Len(t, views, 1)
			require.NotNil(t, views[0].Package)
			text := views[0].Package.Scope().Lookup("Text")
			require.NotNil(t, text)
			assert.Equal(t, types.Typ[types.String], text.Type())
			assert.Nil(t, pass.Info)
			assert.Nil(t, pass.Package)
		})
	}
}

func syntheticTypedFilesPass(t *testing.T, src string) *cop.Pass {
	t.Helper()
	fset := token.NewFileSet()
	filename := filepath.Join(t.TempDir(), "missing", "sample.go")
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
	require.NoError(t, err)
	return &cop.Pass{FileSet: fset, File: file}
}
