package prog_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

func TestFromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":      "module fixture\n\ngo 1.26\n",
		"p.go":        "package p\nimport text \"strings\"\nfunc f(s string) []string { return text.Fields(s) }\n",
		"excluded.go": "package p\nimport text \"strings\"\nfunc g(s string) []string { return text.Fields(s) }\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	program, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	require.False(t, program.HasErrors())
	c := cop.New(cop.Meta{Name: "Test/TypedFile", Description: "Check resolved imports.", Severity: cop.Warning}, func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return
			}
			fn, ok := p.Info.Uses[selector.Sel].(*types.Func)
			require.True(t, ok)
			assert.Equal(t, "strings", fn.Pkg().Path())
			p.Report(call, "resolved")
		})
	}, cop.WithTypes(), cop.WithMinGoVersion("go1.26"), cop.WithScope(cop.OnlyFile("p.go")))
	whole := prog.FromFile(c)
	assert.Equal(t, c.Name(), whole.Name())
	assert.Equal(t, c.Description(), whole.Description())
	assert.Equal(t, c.Severity(), whole.Severity())
	severity := cop.Error
	pass := &prog.Pass{Cop: whole, Program: program, SeverityOverride: &severity}
	whole.Check(pass)
	offenses := pass.Offenses()
	require.Len(t, offenses, 1)
	assert.Equal(t, c.Name(), offenses[0].CopName)
	assert.Equal(t, severity, offenses[0].Severity)
	assert.Equal(t, filepath.Join(dir, "p.go"), offenses[0].Pos.Filename)
	assert.Equal(t, 3, offenses[0].Pos.Line)
	assert.Greater(t, offenses[0].End.Offset, offenses[0].Pos.Offset)
	assert.Len(t, program.Packages[0].Syntax, 2, "scope must not discard analysis inputs")

	c.MinGoVersion = "go1.27"
	pass = &prog.Pass{Cop: whole, Program: program}
	whole.Check(pass)
	assert.Empty(t, pass.Offenses())
}

func TestFromFilePreservesPositions(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", "package p\n", parser.SkipObjectResolution)
	require.NoError(t, err)
	other, err := parser.ParseFile(fset, "other.go", "package p\n//line generated.go:42:7\nvar value = 1\n", parser.ParseComments|parser.SkipObjectResolution)
	require.NoError(t, err)
	program := &prog.Program{
		Fset: fset,
		Packages: []*packages.Package{{
			Syntax: []*ast.File{file, other},
		}},
	}
	var original []cop.Offense
	c := cop.New(cop.Meta{Name: "Test/Inner", Severity: cop.Warning}, func(p *cop.Pass) {
		p.Report(other.Decls[0], "cross-file 100% exact")
		p.ReportAt(file.Pos(), other.End(), "cross-file endpoints")
		p.ReportAt(token.NoPos, token.NoPos, "no position")
		p.ReportAt(file.Pos(), token.NoPos, "no end")
		p.ReportAt(token.NoPos, other.End(), "no start")
		original = p.Offenses()
	}, cop.WithScope(cop.OnlyFile("p.go")))
	whole := prog.FromFile(c)
	outer := prog.New(cop.Meta{Name: "Test/Outer", Severity: cop.Error}, whole.Check)
	for _, severity := range []*cop.Severity{nil, new(cop.Convention)} {
		pass := &prog.Pass{Cop: outer, Program: program, SeverityOverride: severity}
		outer.Check(pass)
		offenses := pass.Offenses()
		require.Len(t, offenses, 5)
		require.Len(t, original, 5)
		for i, want := range original {
			want.CopName = outer.Name()
			want.Severity = outer.Severity()
			if severity != nil {
				want.Severity = *severity
			}
			assert.Equal(t, want, offenses[i])
		}
		assert.Equal(t, "generated.go", offenses[0].Pos.Filename)
		assert.Equal(t, 42, offenses[0].Pos.Line)
		assert.Equal(t, 7, offenses[0].Pos.Column)
		assert.Equal(t, token.Position{}, offenses[2].Pos)
		assert.Equal(t, token.Position{}, offenses[2].End)
	}
}

func TestPassReportOffense(t *testing.T) {
	t.Parallel()
	c := prog.New(cop.Meta{Name: "Test/Outer", Severity: cop.Warning}, nil)
	original := cop.Offense{
		Pos:      token.Position{Filename: "generated.go", Offset: 123, Line: 40, Column: 2},
		End:      token.Position{Filename: "other.go", Offset: 456, Line: 90, Column: 7},
		Message:  "keep 100% exactly",
		CopName:  "Test/Inner",
		Severity: cop.Error,
	}
	for _, severity := range []*cop.Severity{nil, new(cop.Convention)} {
		pass := &prog.Pass{Cop: c, SeverityOverride: severity}
		pass.ReportOffense(original)
		pass.ReportOffense(cop.Offense{Message: "no position"})
		wantSeverity := c.Severity()
		if severity != nil {
			wantSeverity = *severity
		}
		want := original
		want.CopName = c.Name()
		want.Severity = wantSeverity
		assert.Equal(t, []cop.Offense{
			want,
			{Message: "no position", CopName: c.Name(), Severity: wantSeverity},
		}, pass.Offenses())
	}
	assert.Equal(t, "Test/Inner", original.CopName)
	assert.Equal(t, cop.Error, original.Severity)
}
