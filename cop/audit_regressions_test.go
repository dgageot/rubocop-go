package cop_test

import (
	"go/ast"
	"go/parser"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestParenthesizedSelectors(t *testing.T) {
	for _, source := range []string{"fmt.Println", "(fmt.Println)", "((fmt).Println)"} {
		t.Run(source, func(t *testing.T) {
			expr, err := parser.ParseExpr(source)
			require.NoError(t, err)
			assert.True(t, cop.IsSelector(expr, "fmt", "Println"))
			assert.True(t, cop.IsCallTo(&ast.CallExpr{Fun: expr}, "fmt", "Println"))
		})
	}
}

func TestParenthesizedStructDeclarations(t *testing.T) {
	var names []string
	probe := newProbe(func(p *cop.Pass) {
		p.ForEachStruct(func(spec *ast.TypeSpec, _ *ast.StructType) {
			names = append(names, spec.Name.Name)
		})
	})
	coptest.Run(t, probe, `package p
	type S (struct { Value int })
	type Alias = (struct { Value int })
	type Pointer *(struct { Value int })
	func f() { type Local struct { Value int } }
	`)
	assert.Equal(t, []string{"S", "Alias"}, names)
}

func TestFilenameIgnoresLineDirectives(t *testing.T) {
	for _, tc := range []struct {
		physical, adjusted string
		isTest             bool
	}{
		{"real.go", "virtual_test.go", false},
		{"real_test.go", "virtual.go", true},
	} {
		t.Run(tc.physical, func(t *testing.T) {
			probe := newProbe(func(p *cop.Pass) {
				assert.Equal(t, tc.physical, p.Filename())
				assert.Equal(t, tc.isTest, p.IsTestFile())
				assert.Equal(t, tc.adjusted, p.FileSet.Position(p.File.Package).Filename)
			})
			coptest.RunNamed(t, probe, tc.physical, "//line "+tc.adjusted+":1\npackage p")
		})
	}
}

func TestStdlibVersionDoesNotUseLanguageFloor(t *testing.T) {
	for _, tc := range []struct {
		module, constraint, language, stdlib string
	}{
		{"go1.19", "go1.19", "go1.21", "go1.19"},
		{"go1.20", "go1.20", "go1.21", "go1.20"},
		{"go1.20", "go1.21", "go1.21", "go1.21"},
		{"go1.21", "go1.20", "go1.21", "go1.21"},
		{"go1.26", "go1.20", "go1.21", "go1.26"},
	} {
		t.Run(tc.module+"/"+tc.constraint, func(t *testing.T) {
			p := passWithVersion(t, tc.module, tc.constraint)
			assert.Equal(t, tc.language, p.GoVersion())
			assert.Equal(t, tc.stdlib, p.StdlibVersion())
		})
	}
}
