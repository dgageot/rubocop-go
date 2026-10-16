package cops

import (
	"go/ast"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintHTTPTestRequestWithContext reports immediate test-request context attachment.
// It complements HTTPRequestWithContext without changing its production policy.
func NewLintHTTPTestRequestWithContext(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newHTTPTestRequestWithContextFile(opts...))
}

func newHTTPTestRequestWithContextFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/HTTPTestRequestWithContext",
			Description: "Use httptest.NewRequestWithContext for immediate test-request context attachment.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.23",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.PreorderStack(p.File, nil, func(node ast.Node, stack []ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				// Only direct calls are delayed; calls within arguments still run now.
				if len(stack) > 0 {
					switch stack[len(stack)-1].(type) {
					case *ast.GoStmt, *ast.DeferStmt:
						return true
					}
				}
				if len(call.Args) != 1 || call.Ellipsis.IsValid() {
					return true
				}
				method, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
				if !ok {
					return true
				}
				selection := p.Info.Selections[method]
				if selection == nil || selection.Kind() != types.MethodVal || len(selection.Index()) != 1 {
					return true
				}
				fn, ok := selection.Obj().(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "net/http" || fn.Name() != "WithContext" {
					return true
				}
				request, ok := ast.Unparen(method.X).(*ast.CallExpr)
				if !ok || len(request.Args) != 3 || request.Ellipsis.IsValid() {
					return true
				}
				ctor, ok := p.CalleeObject(request).(*types.Func)
				if !ok || ctor.Pkg() == nil || ctor.Pkg().Path() != "net/http/httptest" || ctor.Name() != "NewRequest" {
					return true
				}
				// Moving context before construction is safe only for inert
				// method/target strings and a nil body.
				if p.Info.Types[request.Args[0]].Value == nil || p.Info.Types[request.Args[1]].Value == nil || !httpTestNil(p.Info, request.Args[2]) {
					return true
				}
				if !httpTestContext(p.Info, call.Args[0]) {
					return true
				}
				p.Report(call, "use httptest.NewRequestWithContext(ctx, method, target, nil); keep context evaluation and request validation in their original order")
				return true
			})
		},
	}, opts...)
}

func httpTestNil(info *types.Info, expr ast.Expr) bool {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	return ok && info.Uses[id] == types.Universe.Lookup("nil")
}

func httpTestContext(info *types.Info, expr ast.Expr) bool {
	if id, ok := ast.Unparen(expr).(*ast.Ident); ok {
		v, ok := info.Uses[id].(*types.Var)
		return ok && !v.IsField() && v.Parent() != nil && v.Pkg() != nil && v.Parent() != v.Pkg().Scope()
	}
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	fn, ok := calleeObject(info, call).(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}
	return fn.Pkg().Path() == "context" && (fn.Name() == "Background" || fn.Name() == "TODO")
}
