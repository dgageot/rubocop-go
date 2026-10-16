package cops

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintConstructorPurity enforces that constructors do not start goroutines.
//
// A constructor here is a top-level function (no receiver) named New or
// New<Something> that returns at least one value — the standard Go naming
// convention. Such functions should wire up and return state; they should
// not own a background lifecycle. A `go …` in a constructor means the
// goroutine starts the instant the value is built, before the caller can
// configure it, decide not to use it, or arrange for it to be stopped. The
// caller has no handle to that goroutine and no way to wait for it or shut
// it down, which leaks goroutines in tests and makes shutdown racy.
//
// The fix is to defer the spawn to an explicit Start/Run method (or to
// accept a context the goroutine honours), so that starting background work
// is a deliberate, observable step rather than a hidden effect of New.
//
// Detection is body-local: only `go` statements that execute
// as part of construction are flagged. A `go` nested inside a function
// literal that the constructor merely returns or stores runs when that
// closure is later invoked, not at construction time, so it is intentionally
// ignored. Immediately-invoked function literals and inline range functions
// are inspected because they run during construction. Goroutines started
// indirectly through a helper call are out of scope — catching those would
// require call-graph analysis.
//
// Annotate an intentional case with //rubocop:disable Lint/ConstructorPurity.
func NewLintConstructorPurity(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ConstructorPurity",
			Description: "Avoid starting goroutines in constructors.",
			Severity:    cop.Error,
		},
		Types: true,
		Run: func(p *cop.Pass) {
			p.ForEachFunc(func(fn *ast.FuncDecl) {
				if !isConstructor(fn) || fn.Body == nil {
					return
				}
				forEachConstructionGoStmt(p.Info, fn.Body, func(g *ast.GoStmt) {
					p.Reportf(g,
						"constructor %s starts a goroutine; defer this to a Start/Run method"+
							" so background work is started deliberately and can be stopped",
						fn.Name.Name)
				})
			})
		},
	}, opts...)
}

// isConstructor reports whether fn is a plain top-level function named New or
// New<Something> (the next rune after "New" is upper-case) that returns at
// least one result.
func isConstructor(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Name == nil {
		return false
	}
	name := fn.Name.Name
	if !isConstructorName(name) {
		return false
	}
	return fn.Type.Results != nil && len(fn.Type.Results.List) > 0
}

func isConstructorName(name string) bool {
	if name == "New" {
		return true
	}
	if !strings.HasPrefix(name, "New") {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name[len("New"):])
	return unicode.IsUpper(r)
}

// inspectConstructionBody walks body and visits nodes that run as part of
// executing the constructor. Nested function literals are skipped unless
// invoked directly or used as an inline range function.
func inspectConstructionBody(body *ast.BlockStmt, visit func(ast.Node)) {
	if body == nil {
		return
	}
	inspectConstructionNode(nil, body, visit)
}

func inspectConstructionNode(info *types.Info, root ast.Node, visit func(ast.Node)) {
	if root == nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if expr, ok := n.(ast.Expr); ok && info != nil && info.Types[expr].Value != nil {
			return false // Constant expressions have no evaluated operands.
		}
		switch s := n.(type) {
		case nil:
			return true
		case *ast.FuncLit:
			return false
		case *ast.GoStmt:
			visit(s)
			// The invocation is asynchronous, but its receiver and arguments are not.
			inspectConstructionNode(info, s.Call.Fun, visit)
			for _, arg := range s.Call.Args {
				inspectConstructionNode(info, arg, visit)
			}
			return false
		case *ast.RangeStmt:
			visit(s)
			inspectConstructionNode(info, s.X, visit)
			if fl := constructionFuncLit(info, s.X); fl != nil {
				inspectConstructionNode(info, fl.Body, visit)
			}
			inspectConstructionNode(info, s.Key, visit)
			inspectConstructionNode(info, s.Value, visit)
			inspectConstructionNode(info, s.Body, visit)
			return false
		case *ast.CallExpr:
			if unevaluatedConstructionCall(info, s) {
				return false
			}
			visit(s)
			if fl := constructionFuncLit(info, s.Fun); fl != nil {
				for _, arg := range s.Args {
					inspectConstructionNode(info, arg, visit)
				}
				inspectConstructionNode(info, fl.Body, visit)
				return false
			}
		default:
			visit(s)
		}
		return true
	})
}

// constructionFuncLit unwraps conversions, but not calls that accept callbacks.
// Without type information, only explicit function types are unambiguous.
func constructionFuncLit(info *types.Info, expr ast.Expr) *ast.FuncLit {
	for {
		expr = ast.Unparen(expr)
		if fl, ok := expr.(*ast.FuncLit); ok {
			return fl
		}
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return nil
		}
		_, conversion := ast.Unparen(call.Fun).(*ast.FuncType)
		if info != nil {
			conversion = info.Types[call.Fun].IsType() || conversion
		}
		if !conversion {
			return nil
		}
		expr = call.Args[0]
	}
}

func unevaluatedConstructionCall(info *types.Info, call *ast.CallExpr) bool {
	if obj := calleeObject(info, call); obj != nil {
		if obj.Pkg() == nil || obj.Pkg().Path() != "unsafe" {
			return false
		}
		switch obj.Name() {
		case "Sizeof", "Alignof", "Offsetof":
			return true
		}
		return false
	}
	return cop.IsCallTo(call, "unsafe", "Sizeof", "Alignof", "Offsetof")
}

func calledFuncLit(expr ast.Expr) *ast.FuncLit {
	for {
		switch e := expr.(type) {
		case *ast.FuncLit:
			return e
		case *ast.ParenExpr:
			expr = e.X
		default:
			return nil
		}
	}
}

// forEachConstructionGoStmt invokes fn for every `go` statement that runs as
// part of executing body.
func forEachConstructionGoStmt(info *types.Info, body *ast.BlockStmt, fn func(*ast.GoStmt)) {
	inspectConstructionNode(info, body, func(n ast.Node) {
		if g, ok := n.(*ast.GoStmt); ok {
			fn(g)
		}
	})
}

func forEachTypedConstructionCall(info *types.Info, body *ast.BlockStmt, fn func(*ast.CallExpr)) {
	if info == nil {
		forEachConstructionCallExpr(body, fn)
		return
	}
	inspectConstructionNode(info, body, func(n ast.Node) {
		if call, ok := n.(*ast.CallExpr); ok {
			fn(call)
		}
	})
}
