package cops

import (
	"go/ast"
	"go/token"

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
				checkDeferMutexUnlockBody(p, body)
				return true
			})
		},
	}, opts...)
}

func checkDeferMutexUnlockBody(p *cop.Pass, body *ast.BlockStmt) {
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
		if isSafeManualUnlockAtEnd(body.List, i, lock) {
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

func isSafeManualUnlockAtEnd(stmts []ast.Stmt, lockIdx int, lock mutexCall) bool {
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
	for _, stmt := range stmts[lockIdx+1 : unlockIdx] {
		if stmtPreventsDeferredUnlock(stmt) || nodeContainsLockOp(stmt, lock) {
			return false
		}
	}
	// Even a selector call can evaluate callbacks in its arguments.
	call := ast.Unparen(stmts[unlockIdx].(*ast.ExprStmt).X).(*ast.CallExpr)
	for _, arg := range call.Args {
		if nodeContainsLockOp(arg, lock) {
			return false
		}
	}
	return true
}

func nodeContainsLockOp(node ast.Node, lock mutexCall) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // A stored closure does not execute here.
		case *ast.CallExpr:
			if sel, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr); ok && selectorReceiver(sel.X) == lock.recv {
				switch sel.Sel.Name {
				case "Lock", "Unlock", "RLock", "RUnlock":
					found = true
				}
			}
			if literal := calledFuncLit(n.Fun); !found && literal != nil {
				found = nodeContainsLockOp(literal.Body, lock)
			}
			// Inline callbacks may execute even when the callee is a selector.
			for _, arg := range n.Args {
				if found {
					break
				}
				ast.Inspect(arg, func(node ast.Node) bool {
					if found {
						return false
					}
					if literal, ok := node.(*ast.FuncLit); ok {
						found = nodeContainsLockOp(literal.Body, lock)
						return false
					}
					return true
				})
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
