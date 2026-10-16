package cops

import (
	"go/ast"
	"go/token"
	"slices"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintDeferMutexUnlock catches manual mutex unlocks that are safe and clearer as a
// defer immediately after the corresponding lock.
//
// The cop is deliberately conservative. It ignores short critical sections,
// early exits and jumps, intervening defers, and nested lock operations. Terminal
// return expressions must be side-effect-free because Go evaluates them before
// running deferred calls.
func NewLintDeferMutexUnlock(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/DeferMutexUnlock",
			Description: "Prefer deferred unlocking for terminal critical sections.",
			Severity:    cop.Warning,
		},
		Run: func(p *cop.Pass) {
			bindings := make(mutexClosureBindings)
			ast.Walk(&mutexClosureVisitor{scope: &mutexClosureScope{}, bindings: bindings}, p.File)
			ast.Inspect(p.File, func(n ast.Node) bool {
				var body *ast.BlockStmt
				switch fn := n.(type) {
				case *ast.FuncDecl:
					body = fn.Body
				case *ast.FuncLit:
					body = fn.Body
				default:
					return true
				}
				checkDeferMutexUnlockBody(p, body, bindings)
				return true
			})
		},
	}, opts...)
}

func checkDeferMutexUnlockBody(p *cop.Pass, body *ast.BlockStmt, bindings mutexClosureBindings) {
	if body == nil {
		return
	}
	for i, stmt := range body.List {
		lock, ok := lockCall(stmt)
		if !ok || lock.recv == "" {
			continue
		}
		if i+1 < len(body.List) && isDeferUnlock(body.List[i+1], lock) {
			continue
		}
		if isSafeManualUnlockAtEnd(body.List, i, lock, bindings) {
			p.Reportf(stmt,
				"%s.%s() is only released at the end of this scope; use `defer %s.%s()` immediately after locking",
				lock.recv, lock.method, lock.recv, lock.unlockMethod())
		}
	}
}

type mutexCall struct {
	recv   string
	method string
}

func (c mutexCall) unlockMethod() string {
	if c.method == "RLock" {
		return "RUnlock"
	}
	return "Unlock"
}

func lockCall(stmt ast.Stmt) (mutexCall, bool) {
	name, recv, ok := selectorCallStmt(stmt)
	if !ok || (name != "Lock" && name != "RLock") {
		return mutexCall{}, false
	}
	return mutexCall{recv: recv, method: name}, true
}

func selectorCallStmt(stmt ast.Stmt) (name, recv string, ok bool) {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return "", "", false
	}
	call, ok := ast.Unparen(exprStmt.X).(*ast.CallExpr)
	if !ok {
		return "", "", false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	return sel.Sel.Name, selectorReceiver(sel.X), true
}

func selectorReceiver(expr ast.Expr) string {
	switch v := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		x := selectorReceiver(v.X)
		if x == "" {
			return ""
		}
		return x + "." + v.Sel.Name
	default:
		return ""
	}
}

func isDeferUnlock(stmt ast.Stmt, lock mutexCall) bool {
	deferStmt, ok := stmt.(*ast.DeferStmt)
	if !ok {
		return false
	}
	sel, ok := ast.Unparen(deferStmt.Call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == lock.unlockMethod() && selectorReceiver(sel.X) == lock.recv
}

func isSafeManualUnlockAtEnd(stmts []ast.Stmt, lockIdx int, lock mutexCall, bindings mutexClosureBindings) bool {
	unlockIdx := len(stmts) - 1
	if ret, ok := stmts[unlockIdx].(*ast.ReturnStmt); ok {
		if !returnResultsAreSideEffectFree(ret) {
			return false
		}
		unlockIdx--
	}
	if unlockIdx <= lockIdx {
		return false
	}
	name, recv, ok := selectorCallStmt(stmts[unlockIdx])
	if !ok || recv != lock.recv || name != lock.unlockMethod() {
		return false
	}
	scan := mutexLockScanner{lock: lock, bindings: bindings, seen: make(map[*mutexClosureBinding]bool)}
	for _, stmt := range stmts[lockIdx+1 : unlockIdx] {
		if stmtPreventsDeferredUnlock(stmt) || scan.containsLockOp(stmt) {
			return false
		}
	}
	// Even a selector call can evaluate callbacks in its arguments.
	call := ast.Unparen(stmts[unlockIdx].(*ast.ExprStmt).X).(*ast.CallExpr)
	for _, arg := range call.Args {
		if scan.containsLockOp(arg) || scan.valueContainsLockOp(arg) {
			return false
		}
	}
	return true
}

// Bindings retain every syntactic assignment, not just the last one: branches,
// aliases, and captured reassignments can select any of those closure values.
// Only local identifier aliases are followed; this is not call-graph analysis.
type mutexClosureBindings map[*ast.Ident]*mutexClosureBinding

type mutexClosureBinding struct {
	values []ast.Expr
}

type mutexClosureScope struct {
	parent *mutexClosureScope
	names  map[string]*mutexClosureBinding
}

func (s *mutexClosureScope) lookup(name string) *mutexClosureBinding {
	for ; s != nil; s = s.parent {
		if binding := s.names[name]; binding != nil {
			return binding
		}
	}
	return nil
}

type mutexClosureVisitor struct {
	node     ast.Node
	scope    *mutexClosureScope
	bindings mutexClosureBindings
}

func (v *mutexClosureVisitor) bind(id *ast.Ident, declare bool, value ast.Expr) {
	if id.Name == "_" {
		return
	}
	binding := v.scope.names[id.Name]
	if !declare {
		binding = v.scope.lookup(id.Name)
	}
	if binding == nil {
		binding = &mutexClosureBinding{}
		if v.scope.names == nil {
			v.scope.names = make(map[string]*mutexClosureBinding)
		}
		v.scope.names[id.Name] = binding
	}
	v.bindings[id] = binding
	if value != nil {
		binding.values = append(binding.values, value)
	}
}

func (v *mutexClosureVisitor) signature(fields *ast.FieldList) {
	if fields != nil {
		for _, field := range fields.List {
			for _, name := range field.Names {
				v.bind(name, true, nil)
			}
		}
	}
}

func (v *mutexClosureVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		// A local declaration starts after its initializer has been visited.
		switch n := v.node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					var value ast.Expr
					if len(n.Lhs) == len(n.Rhs) {
						value = n.Rhs[i]
					}
					v.bind(id, n.Tok == token.DEFINE, value)
				}
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				var value ast.Expr
				if len(n.Names) == len(n.Values) {
					value = n.Values[i]
				}
				v.bind(name, true, value)
			}
		}
		return nil
	}
	child := &mutexClosureVisitor{node: node, scope: v.scope, bindings: v.bindings}
	switch n := node.(type) {
	case *ast.FuncDecl:
		child.scope = &mutexClosureScope{parent: v.scope}
		child.signature(n.Recv)
		child.signature(n.Type.Params)
		child.signature(n.Type.Results)
	case *ast.FuncLit:
		child.scope = &mutexClosureScope{parent: v.scope}
		child.signature(n.Type.Params)
		child.signature(n.Type.Results)
	case *ast.BlockStmt:
		switch v.node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
		default:
			child.scope = &mutexClosureScope{parent: v.scope}
		}
		if loop, ok := v.node.(*ast.RangeStmt); ok && loop.Tok == token.DEFINE {
			for _, expr := range []ast.Expr{loop.Key, loop.Value} {
				if id, ok := expr.(*ast.Ident); ok {
					child.bind(id, true, nil)
				}
			}
		}
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt,
		*ast.SelectStmt, *ast.CaseClause, *ast.CommClause:
		child.scope = &mutexClosureScope{parent: v.scope}
	case *ast.Ident:
		if sel, ok := v.node.(*ast.SelectorExpr); !ok || sel.Sel != n {
			if binding := child.scope.lookup(n.Name); binding != nil {
				child.bindings[n] = binding
			}
		}
	}
	return child
}

type mutexLockScanner struct {
	lock     mutexCall
	bindings mutexClosureBindings
	seen     map[*mutexClosureBinding]bool
}

func (s *mutexLockScanner) containsLockOp(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // Stored closures are inspected only when used below.
		case *ast.CallExpr:
			if sel, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr); ok && selectorReceiver(sel.X) == s.lock.recv {
				switch sel.Sel.Name {
				case "Lock", "Unlock", "RLock", "RUnlock":
					found = true
				}
			}
			if !found {
				found = s.valueContainsLockOp(n.Fun)
			}
			// Arguments may be callbacks, including aliases and containers.
			for _, arg := range n.Args {
				if found {
					break
				}
				found = s.valueContainsLockOp(arg)
			}
		}
		return !found
	})
	return found
}

func (s *mutexLockScanner) valueContainsLockOp(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			found = s.containsLockOp(n.Body)
			return false
		case *ast.Ident:
			binding := s.bindings[n]
			if binding != nil && !s.seen[binding] {
				s.seen[binding] = true
				found = slices.ContainsFunc(binding.values, s.valueContainsLockOp)
			}
		}
		return !found
	})
	return found
}

func stmtPreventsDeferredUnlock(stmt ast.Stmt) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // Returns and defers belong to the closure's scope.
		case *ast.DeferStmt, *ast.ReturnStmt:
			found = true
		case *ast.LabeledStmt:
			// A jump to this label could bypass registering the defer.
			found = true
		case *ast.BranchStmt:
			found = n.Tok == token.GOTO
		}
		return !found
	})
	return found
}

func returnResultsAreSideEffectFree(ret *ast.ReturnStmt) bool {
	for _, result := range ret.Results {
		if !exprIsSideEffectFree(result) {
			return false
		}
	}
	return true
}

func exprIsSideEffectFree(expr ast.Expr) bool {
	switch v := ast.Unparen(expr).(type) {
	case nil:
		return true
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return exprIsSideEffectFree(v.X)
	case *ast.UnaryExpr:
		return v.Op != token.ARROW && exprIsSideEffectFree(v.X)
	case *ast.CompositeLit:
		for _, elt := range v.Elts {
			if !exprIsSideEffectFree(elt) {
				return false
			}
		}
		return true
	case *ast.KeyValueExpr:
		return exprIsSideEffectFree(v.Key) && exprIsSideEffectFree(v.Value)
	default:
		return false
	}
}
