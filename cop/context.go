package cop

import (
	"go/ast"
	"go/token"
)

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
			if len(f.Names) == 0 && fl == typ.Params {
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

// WalkFuncWithContextScope walks body with the standard ast.Inspect visit
// contract. hasContext reports whether a syntactically recognized context
// binding is visible at that node, respecting declaration order, block scopes,
// shadowing, and closure captures. Like IsContextType, this is not type analysis.
//
// outerHasContext represents a context supplied by a caller outside this walk.
// Its name is unknown, so local declarations cannot shadow that seed.
func WalkFuncWithContextScope(typ *ast.FuncType, body *ast.BlockStmt, outerHasContext bool, visit func(n ast.Node, hasContext bool) bool) {
	if body == nil {
		return
	}
	scope := &contextScope{anonymous: outerHasContext}
	scope.signature(typ)
	ast.Walk(&contextVisitor{scope: scope, visit: visit}, body)
}

type contextScope struct {
	parent    *contextScope
	names     map[string]bool
	anonymous bool
}

func (s *contextScope) bind(name string, context bool) {
	if name == "" || name == "_" {
		return
	}
	if s.names == nil {
		s.names = make(map[string]bool)
	}
	s.names[name] = context
}

func (s *contextScope) hasContext() bool {
	seen := make(map[string]bool)
	for ; s != nil; s = s.parent {
		if s.anonymous {
			return true
		}
		for name, context := range s.names {
			if !seen[name] && context {
				return true
			}
			seen[name] = true
		}
	}
	return false
}

func (s *contextScope) signature(typ *ast.FuncType) {
	if typ == nil {
		return
	}
	for _, fields := range []*ast.FieldList{typ.Params, typ.Results} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			context := IsContextType(field.Type)
			if len(field.Names) == 0 && context && fields == typ.Params {
				s.anonymous = true
			}
			for _, name := range field.Names {
				s.bind(name.Name, context)
			}
		}
	}
}

type contextVisitor struct {
	node, parent ast.Node
	scope        *contextScope
	visit        func(ast.Node, bool) bool
}

func (v *contextVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		v.bindDeclaration()
		if v.visit != nil {
			v.visit(nil, v.scope.hasContext())
		}
		return nil
	}
	child := &contextVisitor{node: n, parent: v.node, scope: v.scope, visit: v.visit}
	switch n := n.(type) {
	case *ast.FuncLit:
		child.scope = &contextScope{parent: v.scope}
		child.scope.signature(n.Type)
	case *ast.BlockStmt:
		// Parameters and the function body share a scope for := redeclarations.
		_, functionBody := v.node.(*ast.FuncLit)
		if v.node != nil && !functionBody {
			child.scope = &contextScope{parent: v.scope}
		}
		if loop, ok := v.node.(*ast.RangeStmt); ok && loop.Tok == token.DEFINE {
			for _, expr := range []ast.Expr{loop.Key, loop.Value} {
				if id, ok := expr.(*ast.Ident); ok {
					v.scope.bind(id.Name, false)
				}
			}
		}
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.CommClause:
		child.scope = &contextScope{parent: v.scope}
	case *ast.CaseClause:
		child.scope = &contextScope{parent: v.scope}
		if sw, ok := v.parent.(*ast.TypeSwitchStmt); ok {
			if assign, ok := sw.Assign.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
				if id, ok := assign.Lhs[0].(*ast.Ident); ok {
					child.scope.bind(id.Name, len(n.List) == 1 && IsContextType(n.List[0]))
				}
			}
		}
	case *ast.TypeSpec:
		child.scope.bind(n.Name.Name, false)
	}
	if child.visit != nil && !child.visit(n, child.scope.hasContext()) {
		// Still track declarations in pruned subtrees for subsequent siblings.
		child.visit = nil
	}
	return child
}

func (v *contextVisitor) bindDeclaration() {
	switch n := v.node.(type) {
	case *ast.AssignStmt:
		if n.Tok != token.DEFINE {
			return
		}
		for i, expr := range n.Lhs {
			if id, ok := expr.(*ast.Ident); ok {
				if _, exists := v.scope.names[id.Name]; !exists {
					v.scope.bind(id.Name, contextInitializer(i, n.Rhs))
				}
			}
		}
	case *ast.ValueSpec:
		for i, name := range n.Names {
			v.scope.bind(name.Name, IsContextType(n.Type) || (n.Type == nil && contextInitializer(i, n.Values)))
		}
	}
}

func contextInitializer(index int, values []ast.Expr) bool {
	if len(values) == 1 {
		return index == 0 && IsContextProducer(values[0])
	}
	return index < len(values) && IsContextProducer(values[index])
}

// bindsContext reports whether the assignment binds at least one LHS
// identifier to a context.Context value. A single RHS covers both
// single-value forms (`ctx := f()`) and multi-value returns
// (`ctx, cancel := context.WithCancel(...)`); in the latter case the
// context is always at the first return position — `_, cancel := ...`
// discards the context and intentionally yields no in-scope name.
func bindsContext(lhs, rhs []ast.Expr) bool {
	for i, expr := range lhs {
		if isNamedIdent(expr) && contextInitializer(i, rhs) {
			return true
		}
	}
	return false
}

// valueSpecDeclaresContext reports whether s declares a context.Context,
// either explicitly via its declared type (`var ctx context.Context`) or
// implicitly via its initializer (`var ctx = context.Background()`).
func valueSpecDeclaresContext(s *ast.ValueSpec) bool {
	for i, name := range s.Names {
		if isNamedIdent(name) && (IsContextType(s.Type) || contextInitializer(i, s.Values)) {
			return true
		}
	}
	return false
}

// isNamedIdent reports whether e is a named identifier — anonymous (`_`)
// or unset names produce no usable scope binding.
func isNamedIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name != "" && id.Name != "_"
}
