package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintURLClone requires resolved URL types, including aliased imports and types.
func NewLintURLClone(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newURLCloneFile(opts...))
}

func newURLCloneFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/URLClone",
			Description: "Use URL.Clone for equivalent manual deep copies.",
			Severity:    cop.Warning,
		},
		MinStdlibVersion: "go1.27",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BlockStmt:
					checkURLClone(p, node.List)
				case *ast.CaseClause:
					checkURLClone(p, node.Body)
				case *ast.CommClause:
					checkURLClone(p, node.Body)
				case *ast.IfStmt:
					if node.Init != nil {
						break
					}
					field := urlCloneNilComparison(p.Info, node.Cond, token.EQL)
					selector, ok := field.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "User" {
						break
					}
					source := urlCloneObject(p.Info, selector.X)
					if source == nil || !urlClonePointer(source.Type()) {
						break
					}
					cloned, rest, pointer := urlCloneCopy(p.Info, node.Body.List, source)
					if cloned != nil && len(rest) == 1 && urlCloneReturn(p.Info, rest[0], cloned, pointer) {
						p.Report(node.Body.List[0], "use url.URL.Clone for this copy with nil User; preserve the existing nil checks")
					}
				}
				return true
			})
		},
	}, opts...)
}

func checkURLClone(p *cop.Pass, stmts []ast.Stmt) {
	for i, stmt := range stmts {
		guard, ok := stmt.(*ast.IfStmt)
		if !ok || guard.Init != nil || guard.Else != nil || len(guard.Body.List) != 1 {
			continue
		}
		source := urlCloneObject(p.Info, urlCloneNilComparison(p.Info, guard.Cond, token.EQL))
		if source == nil || !urlClonePointer(source.Type()) {
			continue
		}
		ret, ok := guard.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 || !p.Info.Types[ret.Results[0]].IsNil() {
			continue
		}
		cloned, rest, pointer := urlCloneCopy(p.Info, stmts[i+1:], source)
		if cloned != nil && len(rest) == 2 && urlCloneUserCopy(p.Info, rest[0], source, cloned) && urlCloneReturn(p.Info, rest[1], cloned, pointer) {
			p.Report(stmts[i+1], "use url.URL.Clone for this nil-safe deep copy, including User; preserve the existing nil behavior")
		}
	}
}

// Match only adjacent copy statements whose destination is a fresh local.
func urlCloneCopy(info *types.Info, stmts []ast.Stmt, source types.Object) (types.Object, []ast.Stmt, bool) {
	if len(stmts) == 0 {
		return nil, nil, false
	}
	cloned, value := urlCloneLocal(info, stmts[0])
	if cloned == nil {
		return nil, nil, false
	}
	if star, ok := ast.Unparen(value).(*ast.StarExpr); ok && urlCloneObject(info, star.X) == source {
		return cloned, stmts[1:], false
	}
	call, ok := ast.Unparen(value).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() || calleeObject(info, call) != types.Universe.Lookup("new") || !urlClonePointer(cloned.Type()) {
		return nil, nil, false
	}
	if star, ok := ast.Unparen(call.Args[0]).(*ast.StarExpr); ok && urlCloneObject(info, star.X) == source {
		return cloned, stmts[1:], true
	}
	if !info.Types[call.Args[0]].IsType() || len(stmts) < 2 {
		return nil, nil, false
	}
	assign, ok := stmts[1].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, nil, false
	}
	left, lok := ast.Unparen(assign.Lhs[0]).(*ast.StarExpr)
	right, rok := ast.Unparen(assign.Rhs[0]).(*ast.StarExpr)
	if !lok || !rok || urlCloneObject(info, left.X) != cloned || urlCloneObject(info, right.X) != source {
		return nil, nil, false
	}
	return cloned, stmts[2:], true
}

func urlCloneUserCopy(info *types.Info, stmt ast.Stmt, source, cloned types.Object) bool {
	guard, ok := stmt.(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil {
		return false
	}
	if !urlCloneUser(info, urlCloneNilComparison(info, guard.Cond, token.NEQ), source, cloned) {
		return false
	}
	body := guard.Body.List
	if len(body) != 2 {
		return false
	}
	user, value := urlCloneLocal(info, body[0])
	if user == nil {
		return false
	}
	star, ok := ast.Unparen(value).(*ast.StarExpr)
	if !ok || !urlCloneUser(info, star.X, source, cloned) {
		return false
	}
	assign, ok := body[1].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || !urlCloneUser(info, assign.Lhs[0], cloned) {
		return false
	}
	address, ok := ast.Unparen(assign.Rhs[0]).(*ast.UnaryExpr)
	return ok && address.Op == token.AND && urlCloneObject(info, address.X) == user
}

func urlCloneUser(info *types.Info, expr ast.Expr, objects ...types.Object) bool {
	selector, ok := ast.Unparen(expr).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "User" {
		return false
	}
	for _, obj := range objects {
		if urlCloneObject(info, selector.X) == obj {
			return true
		}
	}
	return false
}

func urlCloneReturn(info *types.Info, stmt ast.Stmt, cloned types.Object, pointer bool) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	expr := ret.Results[0]
	if !pointer {
		address, ok := ast.Unparen(expr).(*ast.UnaryExpr)
		if !ok || address.Op != token.AND {
			return false
		}
		expr = address.X
	}
	return urlCloneObject(info, expr) == cloned
}

func urlCloneLocal(info *types.Info, stmt ast.Stmt) (types.Object, ast.Expr) {
	id, value := newExprLocal(stmt)
	if id == nil || id.Name == "_" {
		return nil, nil
	}
	return info.Defs[id], value
}

func urlCloneNilComparison(info *types.Info, expr ast.Expr, op token.Token) ast.Expr {
	compare, ok := ast.Unparen(expr).(*ast.BinaryExpr)
	if !ok || compare.Op != op {
		return nil
	}
	if info.Types[compare.Y].IsNil() {
		return ast.Unparen(compare.X)
	}
	if info.Types[compare.X].IsNil() {
		return ast.Unparen(compare.Y)
	}
	return nil
}

func urlCloneObject(info *types.Info, expr ast.Expr) types.Object {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	return info.Uses[id]
}

func urlClonePointer(t types.Type) bool {
	ptr, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/url" && named.Obj().Name() == "URL"
}
