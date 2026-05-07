package cop

import "go/ast"

// IsContextType reports whether expr is the syntactic type context.Context.
//
// The check is name-based and does not consult type information; an alias
// or dot-import that re-exports context.Context under a different name
// will not match. In practice the standard import path is always used.
func IsContextType(expr ast.Expr) bool {
	pkg, sel, ok := MatchSelector(expr)
	return ok && pkg == "context" && sel == "Context"
}

// IsContextProducer reports whether expr is a call whose shape commonly
// yields a context.Context: any call into the `context` package, or any
// zero-arg method named `Context` (the cobra / http.Request convention).
//
// Like [IsContextType] this is syntactic — false positives are possible
// for unrelated zero-arg `.Context()` methods, but they are rare in
// practice and the cost of being wrong is one spurious "you have a ctx"
// hint, never a missed offense in a context-aware rule.
func IsContextProducer(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	pkg, sel, ok := MatchSelector(call.Fun)
	return ok && (pkg == "context" || (sel == "Context" && len(call.Args) == 0))
}

// SignatureHasContext reports whether typ declares any parameter or named
// result of type context.Context. An anonymous parameter (e.g.
// `func(context.Context)`) still counts as declaring a context in scope —
// callers that pass a value satisfy the signature regardless of the name.
func SignatureHasContext(typ *ast.FuncType) bool {
	for _, fl := range []*ast.FieldList{typ.Params, typ.Results} {
		if fl == nil {
			continue
		}
		for _, f := range fl.List {
			if !IsContextType(f.Type) {
				continue
			}
			if len(f.Names) == 0 {
				return true
			}
			for _, n := range f.Names {
				if isNamedIdent(n) {
					return true
				}
			}
		}
	}
	return false
}

// BodyDeclaresContext reports whether body locally binds an identifier to
// a context.Context value. Nested function literals are intentionally
// skipped — their bindings are scoped to the closure and are inspected
// separately by [WalkFuncWithContextScope].
func BodyDeclaresContext(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch s := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			found = bindsContext(s.Lhs, s.Rhs)
		case *ast.ValueSpec:
			found = valueSpecDeclaresContext(s)
		}
		return !found
	})
	return found
}

// WalkFuncWithContextScope walks body invoking visit at every AST node,
// with hasContext=true when a context.Context is syntactically reachable
// from the enclosing function. The walk recurses into nested function
// literals, propagating the outer context-visibility (closures capture by
// name, so a nested literal sees the parent's ctx).
//
// outerHasContext seeds the recursion; pass false at the top level. visit
// receives the standard ast.Inspect contract: return false to stop
// descending into the current subtree.
//
// Use it for context-aware rules — "prefer the *Context variant" being the
// archetype:
//
//	p.ForEachFunc(func(fn *ast.FuncDecl) {
//	    if fn.Body == nil { return }
//	    cop.WalkFuncWithContextScope(fn.Type, fn.Body, false,
//	        func(n ast.Node, hasContext bool) bool {
//	            if !hasContext { return true }
//	            // ... inspect call expressions ...
//	            return true
//	        })
//	})
func WalkFuncWithContextScope(typ *ast.FuncType, body *ast.BlockStmt, outerHasContext bool, visit func(n ast.Node, hasContext bool) bool) {
	if body == nil {
		return
	}
	hasContext := outerHasContext || SignatureHasContext(typ) || BodyDeclaresContext(body)
	ast.Inspect(body, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			WalkFuncWithContextScope(fl.Type, fl.Body, hasContext, visit)
			return false
		}
		return visit(n, hasContext)
	})
}

// bindsContext reports whether the assignment binds at least one LHS
// identifier to a context.Context value. A single RHS covers both
// single-value forms (`ctx := f()`) and multi-value returns
// (`ctx, cancel := context.WithCancel(...)`); in the latter case the
// context is always at the first return position — `_, cancel := ...`
// discards the context and intentionally yields no in-scope name.
func bindsContext(lhs, rhs []ast.Expr) bool {
	if len(rhs) == 1 {
		return len(lhs) >= 1 && IsContextProducer(rhs[0]) && isNamedIdent(lhs[0])
	}
	for i := 0; i < len(lhs) && i < len(rhs); i++ {
		if IsContextProducer(rhs[i]) && isNamedIdent(lhs[i]) {
			return true
		}
	}
	return false
}

// valueSpecDeclaresContext reports whether s declares a context.Context,
// either explicitly via its declared type (`var ctx context.Context`) or
// implicitly via its initializer (`var ctx = context.Background()`).
func valueSpecDeclaresContext(s *ast.ValueSpec) bool {
	if IsContextType(s.Type) {
		for _, n := range s.Names {
			if isNamedIdent(n) {
				return true
			}
		}
	}
	if len(s.Values) == 0 {
		return false
	}
	lhs := make([]ast.Expr, len(s.Names))
	for i, n := range s.Names {
		lhs[i] = n
	}
	return bindsContext(lhs, s.Values)
}

// isNamedIdent reports whether e is a named identifier — anonymous (`_`)
// or unset names produce no usable scope binding.
func isNamedIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name != "" && id.Name != "_"
}
