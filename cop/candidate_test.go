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
