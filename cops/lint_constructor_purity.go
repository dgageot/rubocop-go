package cops

import (
	"go/ast"

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
// Detection is body-local and syntactic: only `go` statements that execute
// as part of construction are flagged. A `go` nested inside a function
// literal that the constructor merely returns or stores runs when that
// closure is later invoked, not at construction time, so it is intentionally
// ignored. Immediately-invoked function literals are inspected because they
// do run during construction. Goroutines started indirectly through a helper
// call are out of scope — catching those would require call-graph analysis.
//
// Annotate an intentional case with //rubocop:disable Lint/ConstructorPurity.
func NewLintConstructorPurity(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ConstructorPurity",
			Description: "Avoid starting goroutines in constructors.",
			Severity:    cop.Error,
		},
		Run: func(p *cop.Pass) {
			p.ForEachFunc(func(fn *ast.FuncDecl) {
				if !isConstructor(fn) || fn.Body == nil {
					return
				}
				forEachConstructionGoStmt(fn.Body, func(g *ast.GoStmt) {
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
	return name == "New" || (len(name) > 3 && name[:3] == "New" && name[3] >= 'A' && name[3] <= 'Z')
}

// inspectConstructionBody walks body and visits nodes that run as part of
// executing the constructor. Nested function literals are skipped unless they
// are immediately invoked (func() { ... })(), in which case their body runs
// during construction and is inspected.
func inspectConstructionBody(body *ast.BlockStmt, visit func(ast.Node)) {
	if body == nil {
		return
	}
	inspectConstructionNode(body, visit)
}

func inspectConstructionNode(root ast.Node, visit func(ast.Node)) {
	ast.Inspect(root, func(n ast.Node) bool {
		switch s := n.(type) {
		case nil:
			return true
		case *ast.FuncLit:
			return false
		case *ast.GoStmt:
			visit(s)
			return false
		case *ast.CallExpr:
			visit(s)
			if fl := calledFuncLit(s.Fun); fl != nil {
				for _, arg := range s.Args {
					inspectConstructionNode(arg, visit)
				}
				inspectConstructionNode(fl.Body, visit)
				return false
			}
		default:
			visit(s)
		}
		return true
	})
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
func forEachConstructionGoStmt(body *ast.BlockStmt, fn func(*ast.GoStmt)) {
	inspectConstructionBody(body, func(n ast.Node) {
		if g, ok := n.(*ast.GoStmt); ok {
			fn(g)
		}
	})
}
