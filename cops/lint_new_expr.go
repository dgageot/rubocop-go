package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintNewExpr catches fresh temporaries used only to return their address. Requiring
// adjacent statements and constant companion results preserves evaluation order.
func NewLintNewExpr(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/NewExpr",
			Description: "Replace address-only temporaries with new expressions.",
			Severity:    cop.Warning,
		},
		MinGoVersion: "go1.26",
		Types:        true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			uses := make(map[types.Object]int)
			for _, obj := range p.Info.Uses {
				uses[obj]++
			}
			shadowedNew := false
			ast.Inspect(p.File, func(n ast.Node) bool {
				// Partial typing may mistake an implicitly named import for the builtin.
				if selector, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := selector.X.(*ast.Ident); ok && id.Name == "new" {
						shadowedNew = true
					}
				}
				return !shadowedNew
			})
			if shadowedNew {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BlockStmt:
					checkNewExpr(p, uses, node.List)
				case *ast.CaseClause:
					checkNewExpr(p, uses, node.Body)
				case *ast.CommClause:
					checkNewExpr(p, uses, node.Body)
				}
				return true
			})
		},
	}, opts...)
}

func checkNewExpr(p *cop.Pass, uses map[types.Object]int, stmts []ast.Stmt) {
	if len(stmts) < 2 {
		return
	}
	decl := stmts[len(stmts)-2]
	id, expr := newExprLocal(decl)
	if id == nil || id.Name == "_" {
		return
	}
	variable, ok := p.Info.Defs[id].(*types.Var)
	if !ok || uses[variable] != 1 || variable.Parent() == nil {
		return
	}
	if _, literal := ast.Unparen(expr).(*ast.CompositeLit); literal {
		return // &T{...} is clearer than new(T{...}).
	}
	ret, ok := stmts[len(stmts)-1].(*ast.ReturnStmt)
	if !ok {
		return
	}
	_, builtin := variable.Parent().LookupParent("new", ret.Pos())
	if builtin != types.Universe.Lookup("new") {
		return
	}
	found := false
	for _, result := range ret.Results {
		if address, ok := ast.Unparen(result).(*ast.UnaryExpr); ok && address.Op == token.AND {
			if returned, ok := ast.Unparen(address.X).(*ast.Ident); ok && p.Info.Uses[returned] == variable {
				found = true
				continue
			}
		}
		value := p.Info.Types[result]
		if value.Value == nil && !value.IsNil() {
			return
		}
	}
	if found {
		p.Reportf(decl, "use new(expr) instead of declaring %s only to return &%s; keep the initializer unchanged", id.Name, id.Name)
	}
}

func newExprLocal(stmt ast.Stmt) (*ast.Ident, ast.Expr) {
	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		if stmt.Tok == token.DEFINE && len(stmt.Lhs) == 1 && len(stmt.Rhs) == 1 {
			id, _ := stmt.Lhs[0].(*ast.Ident)
			return id, stmt.Rhs[0]
		}
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR || len(decl.Specs) != 1 {
			return nil, nil
		}
		spec, ok := decl.Specs[0].(*ast.ValueSpec)
		if ok && spec.Type == nil && len(spec.Names) == 1 && len(spec.Values) == 1 {
			return spec.Names[0], spec.Values[0]
		}
	}
	return nil, nil
}
