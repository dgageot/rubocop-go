package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintWaitGroupGo suggests WaitGroup.Go for adjacent Add/go/Done patterns.
// This is advisory: the callback's panic contract and capture timing need review.
func NewLintWaitGroupGo(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newWaitGroupGoFile(opts...))
}

func newWaitGroupGoFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/WaitGroupGo",
			Description: "Consider WaitGroup.Go for simple Add/go/Done patterns.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.25",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BlockStmt:
					checkWaitGroupGo(p, node.List)
				case *ast.CaseClause:
					checkWaitGroupGo(p, node.Body)
				case *ast.CommClause:
					checkWaitGroupGo(p, node.Body)
				}
				return true
			})
		},
	}, opts...)
}

func checkWaitGroupGo(p *cop.Pass, stmts []ast.Stmt) {
	for i := 0; i+1 < len(stmts); i++ {
		stmt, ok := stmts[i].(*ast.ExprStmt)
		if !ok {
			continue
		}
		add, ok := ast.Unparen(stmt.X).(*ast.CallExpr)
		if !ok || len(add.Args) != 1 || add.Ellipsis.IsValid() {
			continue
		}
		value := p.Info.Types[add.Args[0]].Value
		if value == nil || value.Kind() != constant.Int || !constant.Compare(value, token.EQL, constant.MakeInt64(1)) {
			continue
		}
		recv := waitGroupGoReceiver(p.Info, add, "Add")
		if recv == nil {
			continue
		}
		launch, ok := stmts[i+1].(*ast.GoStmt)
		if !ok || len(launch.Call.Args) != 0 || launch.Call.Ellipsis.IsValid() {
			continue
		}
		fn, ok := ast.Unparen(launch.Call.Fun).(*ast.FuncLit)
		if !ok || fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 0 || len(fn.Body.List) == 0 {
			continue
		}
		done, ok := fn.Body.List[0].(*ast.DeferStmt)
		if !ok || len(done.Call.Args) != 0 || waitGroupGoReceiver(p.Info, done.Call, "Done") != recv {
			continue
		}
		if !waitGroupGoStable(p.Info, p.File, recv) || !waitGroupGoBody(p.Info, fn.Body, recv) {
			continue
		}
		p.ReportAt(stmt.Pos(), launch.End(), "consider wg.Go for this WaitGroup pattern; the callback must not let a panic escape; verify receiver stability, captures, and timing before changing it")
	}
}

func waitGroupGoReceiver(info *types.Info, call *ast.CallExpr, method string) *types.Var {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	selection := info.Selections[sel]
	if selection == nil || selection.Kind() != types.MethodVal || len(selection.Index()) != 1 {
		return nil
	}
	fn, ok := selection.Obj().(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "sync" || fn.Name() != method {
		return nil
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return nil
	}
	recv, ok := info.ObjectOf(id).(*types.Var)
	if !ok || recv.IsField() || recv.Parent() == nil || recv.Pkg() == nil || recv.Parent() == recv.Pkg().Scope() {
		return nil
	}
	typ := types.Unalias(recv.Type())
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = types.Unalias(ptr.Elem())
	} else if !info.Types[sel.X].Addressable() {
		return nil
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "sync" || named.Obj().Name() != "WaitGroup" {
		return nil
	}
	return recv
}

// Assignments anywhere in the file include writes by other capturing closures.
// Taking the variable's address can hide such writes behind an alias.
func waitGroupGoStable(info *types.Info, file *ast.File, recv *types.Var) bool {
	stable := true
	ast.Inspect(file, func(n ast.Node) bool {
		if !stable {
			return false
		}
		switch node := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if waitGroupGoUses(info, lhs, recv) {
					stable = false
					break
				}
			}
		case *ast.RangeStmt:
			// Range bindings can be reused in files targeting older Go syntax.
			for _, expr := range []ast.Expr{node.Key, node.Value} {
				if waitGroupGoUses(info, expr, recv) {
					stable = false
					break
				}
				if id, ok := expr.(*ast.Ident); ok && info.ObjectOf(id) == recv {
					stable = false
					break
				}
			}
		case *ast.UnaryExpr:
			if node.Op == token.AND && waitGroupGoUses(info, node.X, recv) {
				stable = false
			}
		}
		return stable
	})
	return stable
}

func waitGroupGoBody(info *types.Info, body *ast.BlockStmt, recv *types.Var) bool {
	// Restrict receiver use to the first defer, including in nested closures.
	for _, stmt := range body.List[1:] {
		if waitGroupGoUses(info, stmt, recv) {
			return false
		}
	}
	safe := true
	ast.Inspect(body, func(n ast.Node) bool {
		if !safe {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // A nested deferred recovery keeps its own call frame.
		}
		if call, ok := n.(*ast.CallExpr); ok {
			id, ok := ast.Unparen(call.Fun).(*ast.Ident)
			if ok && info.Uses[id] == types.Universe.Lookup("recover") {
				safe = false
			}
		}
		return safe
	})
	return safe
}

func waitGroupGoUses(info *types.Info, node ast.Node, recv *types.Var) bool {
	if node == nil {
		return false
	}
	used := false
	ast.Inspect(node, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && info.Uses[id] == recv {
			used = true
		}
		return !used
	})
	return used
}
