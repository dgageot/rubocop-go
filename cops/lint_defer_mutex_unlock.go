package cops

import (
	"go/ast"
	"go/token"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintDeferMutexUnlock catches manual mutex unlocks that are safe and clearer as a
// defer immediately after the corresponding lock.
//
// The cop is deliberately conservative. It ignores intentionally short critical
// sections (work after unlock), unlock/relock patterns, nested lock scopes, and
// terminal returns with side-effectful result expressions. Those patterns would
// change behavior if rewritten to defer because Go evaluates return expressions
// before running deferred calls.
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
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return "", "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	return sel.Sel.Name, selectorReceiver(sel.X), true
}

func selectorReceiver(expr ast.Expr) string {
	switch v := expr.(type) {
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
	sel, ok := deferStmt.Call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == lock.unlockMethod() && selectorReceiver(sel.X) == lock.recv
}

func isSafeManualUnlockAtEnd(stmts []ast.Stmt, lockIdx int, lock mutexCall) bool {
	unlockIdx := -1
	for i := lockIdx + 1; i < len(stmts); i++ {
		if stmtContainsDefer(stmts[i]) {
			return false
		}
		name, recv, ok := selectorCallStmt(stmts[i])
		if !ok {
			// Compound statement (if/for/switch/...): a nested lock or unlock of
			// the same mutex means the terminal unlock is not the sole release,
			// so deferring it would change behavior.
			if stmtContainsLockOp(stmts[i], lock) {
				return false
			}
			continue
		}
		if recv != lock.recv {
			continue
		}
		switch name {
		case lock.method:
			return false // release/reacquire or nested critical section
		case lock.unlockMethod():
			if unlockIdx >= 0 {
				return false
			}
			unlockIdx = i
		}
	}
	if unlockIdx < 0 {
		return false
	}
	if unlockIdx == len(stmts)-1 {
		return true
	}
	if unlockIdx+1 != len(stmts)-1 {
		return false
	}
	ret, ok := stmts[unlockIdx+1].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	return returnResultsAreSideEffectFree(ret)
}

func stmtContainsLockOp(stmt ast.Stmt, lock mutexCall) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selectorReceiver(sel.X) != lock.recv {
			return true
		}
		switch sel.Sel.Name {
		case lock.method, lock.unlockMethod():
			found = true
		}
		return !found
	})
	return found
}

func stmtContainsDefer(stmt ast.Stmt) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		if found {
			return false
		}
		_, found = n.(*ast.DeferStmt)
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
	switch v := expr.(type) {
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
