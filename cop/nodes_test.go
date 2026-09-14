package cop_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestNodes(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "sample.go", `package x
func f() { outer(inner()); func() { nested() }() }
`, parser.SkipObjectResolution)
	require.NoError(t, err)

	var want []*ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			want = append(want, call)
		}
		return true
	})
	assert.Equal(t, want, slices.Collect(cop.Nodes[*ast.CallExpr](file)))
	assert.Equal(t, []*ast.CallExpr{want[1]}, slices.Collect(cop.Nodes[*ast.CallExpr](want[1])))
	assert.Empty(t, slices.Collect(cop.Nodes[*ast.CallExpr](nil)))
	var body *ast.BlockStmt
	assert.Empty(t, slices.Collect(cop.Nodes[*ast.CallExpr](body)))
	assert.Empty(t, slices.Collect(cop.Nodes[*ast.GoStmt](file)))

	var first []*ast.CallExpr
	for call := range cop.Nodes[*ast.CallExpr](file) {
		first = append(first, call)
		break
	}
	assert.Equal(t, want[:1], first)
	assert.Equal(t, want, slices.Collect(cop.Nodes[*ast.CallExpr](file)))
}

func TestOn(t *testing.T) {
	c := cop.On(cop.Meta{Name: "Test/Calls", Severity: cop.Warning}, func(p *cop.Pass, call *ast.CallExpr) {
		p.Report(call, "call")
	}, cop.WithScope(cop.OnlyFile("target.go")), cop.WithTypes())
	assert.True(t, c.NeedsTypes())

	src := "package x; func f() { g(h()) }"
	assert.Empty(t, coptest.RunNamed(t, c, "other.go", src))
	offenses := coptest.RunNamed(t, c, "target.go", src)
	require.Len(t, offenses, 2)
	assert.Equal(t, "Test/Calls", offenses[0].CopName)
	assert.Equal(t, cop.Warning, offenses[0].Severity)
	assert.Less(t, offenses[0].Pos.Column, offenses[1].Pos.Column)
}

func TestOnTyped(t *testing.T) {
	c := cop.On(cop.Meta{Name: "Test/Typed"}, func(p *cop.Pass, call *ast.CallExpr) {
		require.NotNil(t, p.CalleeObject(call))
		value, ok := p.StringValue(call.Args[0])
		require.True(t, ok)
		p.Report(call, value)
	}, cop.WithTypes())
	offenses := coptest.RunTyped(t, c, `package x
const prefix = "hello"
func use(string) {}
func f() { use(prefix + " world") }
`)
	require.Len(t, offenses, 1)
	assert.Equal(t, "hello world", offenses[0].Message)
}
