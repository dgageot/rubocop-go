package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintMapsClone reports equivalent nil-safe shallow map copy helpers.
func NewLintMapsClone(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newMapsCloneFile(opts...))
}

func newMapsCloneFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/MapsClone",
			Description: "Use maps.Clone for equivalent nil-safe shallow map copy helpers.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.21",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch fn := n.(type) {
				case *ast.FuncDecl:
					obj, ok := p.Info.Defs[fn.Name].(*types.Func)
					if ok && fn.Recv == nil && mapsCloneHelper(p, obj.Type(), fn.Body) {
						p.Report(fn, "use maps.Clone(src) for this nil-safe shallow copy; preserve the map's named type")
					}
				case *ast.FuncLit:
					if mapsCloneHelper(p, p.Info.TypeOf(fn), fn.Body) {
						p.Report(fn, "use maps.Clone(src) for this nil-safe shallow copy; preserve the map's named type")
					}
				}
				return true
			})
		},
	}, opts...)
}

func mapsCloneHelper(p *cop.Pass, typ types.Type, body *ast.BlockStmt) bool {
	sig, ok := typ.(*types.Signature)
	if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 1 || body == nil || len(body.List) != 4 {
		return false
	}
	src := sig.Params().At(0)
	mapType, ok := src.Type().Underlying().(*types.Map)
	if !ok || !types.Identical(src.Type(), sig.Results().At(0).Type()) || !mapsCloneReflexive(mapType.Key()) {
		return false
	}
	guard, ok := body.List[0].(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil || len(guard.Body.List) != 1 {
		return false
	}
	condition, ok := ast.Unparen(guard.Cond).(*ast.BinaryExpr)
	if !ok || condition.Op != token.EQL {
		return false
	}
	left, right := condition.X, condition.Y
	if mapsCloneIdent(p, right, src) {
		left, right = right, left
	}
	if !mapsCloneIdent(p, left, src) || !mapsCloneIdent(p, right, types.Universe.Lookup("nil")) {
		return false
	}
	ret, ok := guard.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 || !mapsCloneIdent(p, ret.Results[0], types.Universe.Lookup("nil")) {
		return false
	}
	assign, ok := body.List[1].(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	id, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return false
	}
	dst, ok := p.Info.Defs[id].(*types.Var)
	if !ok || !types.Identical(dst.Type(), src.Type()) || !mapsCloneAllocation(p, assign.Rhs[0], src) {
		return false
	}
	loop, ok := body.List[2].(*ast.RangeStmt)
	if !ok || !mapsCloneLoop(p, loop, src, dst) {
		return false
	}
	ret, ok = body.List[3].(*ast.ReturnStmt)
	return ok && len(ret.Results) == 1 && mapsCloneIdent(p, ret.Results[0], dst)
}

func mapsCloneAllocation(p *cop.Pass, expr ast.Expr, src *types.Var) bool {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || p.CalleeObject(call) != types.Universe.Lookup("make") || call.Ellipsis.IsValid() || len(call.Args) < 1 || len(call.Args) > 2 || !types.Identical(p.Info.TypeOf(call), src.Type()) {
		return false
	}
	if len(call.Args) == 1 {
		return true
	}
	length, ok := ast.Unparen(call.Args[1]).(*ast.CallExpr)
	return ok && p.CalleeObject(length) == types.Universe.Lookup("len") && len(length.Args) == 1 && mapsCloneIdent(p, length.Args[0], src)
}

func mapsCloneLoop(p *cop.Pass, loop *ast.RangeStmt, src, dst *types.Var) bool {
	if loop.Tok != token.DEFINE || len(loop.Body.List) != 1 || !mapsCloneIdent(p, loop.X, src) {
		return false
	}
	key, keyOK := loop.Key.(*ast.Ident)
	value, valueOK := loop.Value.(*ast.Ident)
	if !keyOK || !valueOK || key.Name == "_" || value.Name == "_" || p.Info.Defs[key] == nil || p.Info.Defs[value] == nil {
		return false
	}
	assign, ok := loop.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	index, ok := ast.Unparen(assign.Lhs[0]).(*ast.IndexExpr)
	return ok && mapsCloneIdent(p, index.X, dst) && mapsCloneIdent(p, index.Index, p.Info.Defs[key]) && mapsCloneIdent(p, assign.Rhs[0], p.Info.Defs[value])
}

func mapsCloneIdent(p *cop.Pass, expr ast.Expr, obj types.Object) bool {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	return ok && obj != nil && p.Info.ObjectOf(id) == obj
}

// Runtime cloning retains NaN keys; reinserting ranged keys need not. Interfaces
// and type parameters can hide non-reflexive keys, even inside arrays or structs.
func mapsCloneReflexive(typ types.Type) bool {
	switch typ := typ.Underlying().(type) {
	case *types.Basic:
		return typ.Info()&(types.IsBoolean|types.IsInteger|types.IsString) != 0 || typ.Kind() == types.UnsafePointer
	case *types.Pointer, *types.Chan:
		return true
	case *types.Array:
		return mapsCloneReflexive(typ.Elem())
	case *types.Struct:
		for field := range typ.Fields() {
			if !mapsCloneReflexive(field.Type()) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
