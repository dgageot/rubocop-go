package cop_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
)

func TestCandidateSkipsCheck(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "sample.go", "package p", 0)
	require.NoError(t, err)
	called := false
	c := cop.New(cop.Meta{Name: "Test/Candidate"}, func(*cop.Pass) { called = true },
		cop.WithCandidate(func(candidate *ast.File) bool {
			assert.Same(t, file, candidate)
			return false
		}))
	c.Check(&cop.Pass{Cop: c, File: file})
	assert.False(t, called)
	c.Candidate = func(*ast.File) bool { return true }
	c.Check(&cop.Pass{Cop: c, File: file})
	assert.True(t, called)
}

func TestReportOffensePreservesPositions(t *testing.T) {
	t.Parallel()
	c := cop.New(cop.Meta{Name: "Test/Receiver", Severity: cop.Warning}, nil)
	original := cop.Offense{
		Pos:     token.Position{Filename: "first.go", Offset: 12, Line: 2, Column: 3},
		End:     token.Position{Filename: "second.go", Offset: 40, Line: 5, Column: 2},
		Message: "keep 100% exactly", CopName: "Test/Original", Severity: cop.Convention,
	}
	for _, override := range []*cop.Severity{nil, new(cop.Error)} {
		pass := &cop.Pass{Cop: c, SeverityOverride: override}
		pass.ReportOffense(original)
		pass.ReportOffense(cop.Offense{Message: "no position"})
		want := original
		want.CopName = c.Name()
		want.Severity = c.Severity()
		if override != nil {
			want.Severity = *override
		}
		assert.Equal(t, []cop.Offense{want, {CopName: c.Name(), Severity: want.Severity, Message: "no position"}}, pass.Offenses())
	}
	assert.Equal(t, "Test/Original", original.CopName)
	assert.Equal(t, cop.Convention, original.Severity)
}
