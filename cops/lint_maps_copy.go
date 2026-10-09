package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintMapsCopy reports plain loops that copy entries between local maps.
func NewLintMapsCopy(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newMapsCopyFile(opts...))
}

func newMapsCopyFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/MapsCopy",
			Description: "Use maps.Copy for plain map entry copy loops.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.21",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				loop, ok := n.(*ast.RangeStmt)
				if ok && mapsCopyLoop(p, loop) {
					p.Report(loop, "use maps.Copy(dst, src) instead of a plain map entry copy loop")
				}
				return true
			})
		},
	}, opts...)
}

func mapsCopyLoop(p *cop.Pass, loop *ast.RangeStmt) bool {
	if loop.Tok != token.DEFINE || len(loop.Body.List) != 1 {
		return false
	}
	key, keyOK := loop.Key.(*ast.Ident)
	value, valueOK := loop.Value.(*ast.Ident)
	if !keyOK || !valueOK || key.Name == "_" || value.Name == "_" || p.Info.Defs[key] == nil || p.Info.Defs[value] == nil {
		return false
	}
	src := mapsCopyLocal(p, loop.X)
	if src == nil {
		return false
	}
	from, ok := src.Type().Underlying().(*types.Map)
	if !ok {
		return false
	}
	assign, ok := loop.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	index, ok := ast.Unparen(assign.Lhs[0]).(*ast.IndexExpr)
	if !ok {
		return false
	}
	dst := mapsCopyLocal(p, index.X)
	if dst == nil || dst == p.Info.Defs[key] || dst == p.Info.Defs[value] {
		return false
	}
	to, ok := dst.Type().Underlying().(*types.Map)
	if !ok || !types.Identical(from.Key(), to.Key()) || !types.Identical(from.Elem(), to.Elem()) {
		return false
	}
	k, kOK := ast.Unparen(index.Index).(*ast.Ident)
	v, vOK := ast.Unparen(assign.Rhs[0]).(*ast.Ident)
	return kOK && vOK && p.Info.ObjectOf(k) == p.Info.Defs[key] && p.Info.ObjectOf(v) == p.Info.Defs[value]
}

func mapsCopyLocal(p *cop.Pass, expr ast.Expr) *types.Var {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := p.Info.ObjectOf(id).(*types.Var)
	if !ok || v.IsField() || v.Parent() == nil || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
		return nil
	}
	return v
}
