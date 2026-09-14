package cop_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestStringLiteral(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
		ok   bool
	}{
		{`"hello\n"`, "hello\n", true},
		{"`hello`", "hello", true},
		{`""`, "", true},
		{"42", "", false},
		{"name", "", false},
		{`"a" + "b"`, "", false},
	} {
		t.Run(tc.src, func(t *testing.T) {
			expr, err := parser.ParseExpr(tc.src)
			require.NoError(t, err)
			value, ok := cop.StringLiteral(expr)
			assert.Equal(t, tc.want, value)
			assert.Equal(t, tc.ok, ok)
			p := &cop.Pass{}
			fallback, fallbackOK := p.StringValue(expr)
			assert.Equal(t, value, fallback)
			assert.Equal(t, ok, fallbackOK)
		})
	}
	_, ok := cop.StringLiteral(&ast.BasicLit{Kind: token.STRING, Value: `"broken`})
	assert.False(t, ok)
	_, ok = cop.StringLiteral(nil)
	assert.False(t, ok)
}

func TestStringValue(t *testing.T) {
	var values []string
	var matched []bool
	probe := newProbe(func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			if !cop.IsCallTo(call, "sink", "Use") {
				return
			}
			value, ok := p.StringValue(call.Args[0])
			values = append(values, value)
			matched = append(matched, ok)
		})
	})
	coptest.RunTyped(t, probe, `package x
const prefix = "hello"
var dynamic = "value"
var sink struct { Use func(any) }
func f() {
 sink.Use(prefix)
 sink.Use(prefix + " world")
 sink.Use(dynamic)
 sink.Use(42)
 sink.Use(missing)
}
`)
	assert.Equal(t, []string{"hello", "hello world", "", "", ""}, values)
	assert.Equal(t, []bool{true, true, false, false, false}, matched)
}

func TestCalleeObject(t *testing.T) {
	var names []string
	probe := newProbe(func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			obj := p.CalleeObject(call)
			if obj == nil {
				names = append(names, "")
				return
			}
			names = append(names, obj.Name())
		})
		assert.Nil(t, p.CalleeObject(nil))
	})
	coptest.RunTyped(t, probe, `package x
func plain() {}
func generic[T any](T) {}
func pair[A, B any](A, B) {}
type T struct{}
func (T) Method() {}
func f(t T) {
 plain()
 (plain)()
 generic[int](1)
 pair[int, string](1, "x")
 t.Method()
 func() {}()
 missing()
}
`)
	assert.Equal(t, []string{"plain", "plain", "generic", "pair", "Method", "", ""}, names)
	assert.Nil(t, (&cop.Pass{}).CalleeObject(&ast.CallExpr{Fun: ast.NewIdent("f")}))
}
