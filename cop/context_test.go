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

func contextFunction(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "context.go", "package p\n"+src, parser.SkipObjectResolution)
	require.NoError(t, err)
	return file.Decls[0].(*ast.FuncDecl)
}

func TestWalkFuncWithContextScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want []bool
	}{
		{"parameter", `func f(ctx context.Context) { mark(); }`, []bool{true}},
		{"parenthesized parameter", `func f(ctx (context.Context)) { mark(); }`, []bool{true}},
		{"parenthesized local", `func f() { var ctx (context.Context); mark(); _ = ctx }`, []bool{true}},
		{"parenthesized producer", `func f() { ctx := ((context.Background())); mark(); _ = ctx }`, []bool{true}},
		{"parenthesized callee", `func f() { ctx := (context.Background)(); mark(); _ = ctx }`, []bool{true}},
		{"cause is not a context", `func f() { err := context.Cause(context.Background()); mark(); _ = err }`, []bool{false}},
		{"after func is not a context", `func f() { stop := context.AfterFunc(context.Background(), func() { mark() }); mark(); _ = stop }`, []bool{false, false}},
		{"nonidentifier receiver", `func f() { ctx := requests[0].Context(); mark(); _ = ctx }`, []bool{true}},
		{"unnamed closure captures context", `func f(ctx context.Context) { _ = func(context.Context) { mark() } }`, []bool{true}},
		{"blank closure captures context", `func f(ctx context.Context) { _ = func(_ context.Context) { mark() } }`, []bool{true}},
		{"blank parameter", `func f(_ context.Context) { mark(); }`, []bool{false}},
		{"anonymous parameter", `func f(context.Context) { mark(); }`, []bool{false}},
		{"named result", `func f() (ctx context.Context) { mark(); return }`, []bool{true}},
		{"unnamed result", `func f() context.Context { mark(); return nil }`, []bool{false}},
		{"closure unnamed result", `func f() { _ = func() context.Context { mark(); return nil } }`, []bool{false}},
		{"before declaration", `func f() { mark(); ctx := context.Background(); mark(); _ = ctx }`, []bool{false, true}},
		{"initializer", `func f() { ctx := context.WithValue(mark(), 1, 2); mark(); _ = ctx }`, []bool{false, true}},
		{"typed local", `func f() { var ctx context.Context; mark(); _ = ctx }`, []bool{true}},
		{"var initializer", `func f() { var ctx = context.WithValue(mark(), 1, 2); mark(); _ = ctx }`, []bool{false, true}},
		{"multi return", `func f() { ctx, cancel := context.WithCancel(nil); mark(); _, _ = ctx, cancel }`, []bool{true}},
		{"discarded context", `func f() { _, cancel := context.WithCancel(nil); mark(); _ = cancel }`, []bool{false}},
		{"multiple initializers", `func f() { x, ctx := 0, context.Background(); mark(); _, _ = x, ctx }`, []bool{true}},
		{"expired block", `func f() { { ctx := context.Background(); mark(); _ = ctx }; mark() }`, []bool{true, false}},
		{"shadowed parameter", `func f(ctx context.Context) { { ctx := 1; mark(); _ = ctx }; mark() }`, []bool{false, true}},
		{"shadow initializer", `func f(ctx context.Context) { { ctx := mark(); mark(); _ = ctx } }`, []bool{true, false}},
		{"one of two shadowed", `func f(ctx, other context.Context) { { ctx := 1; mark(); _ = ctx } }`, []bool{true}},
		{"type shadows parameter", `func f(ctx context.Context) { { type ctx int; mark() }; mark() }`, []bool{false, true}},
		{"if initializer", `func f() { if ctx := context.Background(); mark() { mark(); _ = ctx } else { mark() }; mark() }`, []bool{true, true, true, false}},
		{"if body only", `func f() { if mark() { ctx := context.Background(); mark(); _ = ctx } else { mark() }; mark() }`, []bool{false, true, false, false}},
		{"for initializer", `func f() { for ctx := context.Background(); mark(); mark() { mark(); _ = ctx }; mark() }`, []bool{true, true, true, false}},
		{"range shadow", `func f(ctx context.Context) { for ctx := range mark() { mark(); _ = ctx }; mark() }`, []bool{true, false, true}},
		{"range assignment", `func f(ctx context.Context) { for ctx = range mark() { mark() }; mark() }`, []bool{true, true, true}},
		{"switch initializer", `func f() { switch ctx := context.Background(); mark() { case 1: mark(); _ = ctx; default: mark() }; mark() }`, []bool{true, true, true, false}},
		{"case isolation", `func f() { switch { case true: ctx := context.Background(); mark(); _ = ctx; default: mark() }; mark() }`, []bool{true, false, false}},
		{"type switch", `func f(ctx context.Context, value any) { switch ctx := value.(type) { case int: mark(); case context.Context: mark(); default: mark() }; mark() }`, []bool{false, true, false, true}},
		{"select shadow", `func f(ctx context.Context, ch chan int) { select { case ctx := <-ch: mark(); _ = ctx; default: mark() }; mark() }`, []bool{false, true, true}},
		{"select receive scope", `func f(ctx context.Context) { select { case ctx := <-mark(): mark(); _ = ctx }; mark() }`, []bool{true, false, true}},
		{"select body context", `func f() { select { case <-ch: ctx := context.Background(); mark(); _ = ctx; default: mark() }; mark() }`, []bool{true, false, false}},
		{"closure capture", `func f(ctx context.Context) { _ = func() { mark() }; mark() }`, []bool{true, true}},
		{"closure before declaration", `func f() { _ = func() { mark() }; ctx := context.Background(); _ = func() { mark() }; _ = ctx }`, []bool{false, true}},
		{"closure shadow parameter", `func f(ctx context.Context) { _ = func(ctx int) { mark() }; mark() }`, []bool{false, true}},
		{"closure shadow local", `func f(ctx context.Context) { _ = func() { mark(); ctx := 0; mark(); _ = ctx }; mark() }`, []bool{true, false, true}},
		{"closure local does not leak", `func f() { _ = func() { ctx := context.Background(); mark(); _ = ctx }; mark(); _ = func() { mark() } }`, []bool{true, false, false}},
		{"closure short redeclaration", `func f(ctx context.Context) { _ = func(ctx context.Context) { ctx, cancel := context.WithCancel(nil); mark(); _, _ = ctx, cancel } }`, []bool{true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := contextFunction(t, tc.src)
			var got []bool
			cop.WalkFuncWithContextScope(fn.Type, fn.Body, false, func(n ast.Node, hasContext bool) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "mark" {
						got = append(got, hasContext)
					}
				}
				return true
			})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWalkFuncWithContextScopePruning(t *testing.T) {
	t.Parallel()
	fn := contextFunction(t, `func f() { ctx := context.Background(); _ = func() { hidden() }; visible(); _ = ctx }`)
	var calls []string
	cop.WalkFuncWithContextScope(fn.Type, fn.Body, false, func(n ast.Node, hasContext bool) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			return false
		case *ast.CallExpr:
			calls = append(calls, node.Fun.(*ast.Ident).Name)
			assert.True(t, hasContext, "pruning must not hide a preceding declaration")
		}
		return true
	})
	assert.Equal(t, []string{"visible"}, calls)

	calls = nil
	cop.WalkFuncWithContextScope(fn.Type, fn.Body, false, func(n ast.Node, _ bool) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok {
				calls = append(calls, id.Name)
			}
		}
		return true
	})
	assert.Equal(t, []string{"visible"}, calls)
}

func TestContextSyntacticHelpers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`func f() { if true { ctx := context.Background(); _ = ctx } }`, true},
		{`func f() { _ = func() { ctx := context.Background(); _ = ctx } }`, false},
		{`func f() { _, cancel := context.WithCancel(nil); _ = cancel }`, false},
		{`func f() { ctx = context.Background() }`, true},
	} {
		fn := contextFunction(t, tc.src)
		assert.Equal(t, tc.want, cop.BodyDeclaresContext(fn.Body), tc.src)
	}
	assert.False(t, cop.BodyDeclaresContext(nil))
	fn := contextFunction(t, `func f(context.Context) {}`)
	assert.False(t, cop.SignatureHasContext(fn.Type))
	unnamedResult := contextFunction(t, `func f() context.Context { return nil }`)
	assert.False(t, cop.SignatureHasContext(unnamedResult.Type))
	cop.WalkFuncWithContextScope(fn.Type, nil, false, func(ast.Node, bool) bool {
		t.Fatal("nil body must not be visited")
		return true
	})
	cop.WalkFuncWithContextScope(nil, fn.Body, true, func(_ ast.Node, hasContext bool) bool {
		assert.True(t, hasContext)
		return true
	})
}

func TestContextProducers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"context.Background()", true},
		{"context.TODO()", true},
		{"context.WithCancel(ctx)", true},
		{"context.WithCancelCause(ctx)", true},
		{"context.WithDeadline(ctx, deadline)", true},
		{"context.WithDeadlineCause(ctx, deadline, cause)", true},
		{"context.WithTimeout(ctx, timeout)", true},
		{"context.WithTimeoutCause(ctx, timeout, cause)", true},
		{"context.WithValue(ctx, key, value)", true},
		{"context.WithoutCancel(ctx)", true},
		{"context.Cause(ctx)", false},
		{"context.AfterFunc(ctx, callback)", false},
		{"context.Canceled", false},
		{"context.Custom()", false},
		{"request.Context()", true},
		{"server.request.Context()", true},
		{"requests[0].Context()", true},
		{"getRequest().Context()", true},
		{"(request).Context()", true},
		{"(getRequest().Context)()", true},
		{"((context.Background()))", true},
		{"(context.Background)()", true},
		{"request.Context(value)", false},
		{"request", false},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			expr, err := parser.ParseExpr(tc.expr)
			require.NoError(t, err)
			assert.Equal(t, tc.want, cop.IsContextProducer(expr))
		})
	}
}

func TestContextSignatureBindings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`func f(ctx context.Context) {}`, true},
		{`func f(ctx (context.Context)) {}`, true},
		{`func f(_ context.Context) {}`, false},
		{`func f(context.Context) {}`, false},
		{`func f() (ctx context.Context) { return }`, true},
		{`func f() (_ context.Context) { return nil }`, false},
		{`func f() context.Context { return nil }`, false},
	} {
		fn := contextFunction(t, tc.src)
		assert.Equal(t, tc.want, cop.SignatureHasContext(fn.Type), tc.src)
	}
	assert.False(t, cop.SignatureHasContext(nil))
}
