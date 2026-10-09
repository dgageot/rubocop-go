package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintSlicesEqual suggests slices.Equal for exact element-wise helpers.
// Nil-sensitive guards and incompatible named slice types are excluded.
func NewLintSlicesEqual(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newSlicesEqualFile(opts...))
}

func newSlicesEqualFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/SlicesEqual",
			Description: "Use slices.Equal for simple element-wise slice equality helpers.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.21",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				body, sig := sliceBoolFunction(p.Info, n)
				if body != nil && slicesEqualBody(p.Info, body, sig) {
					p.Report(body, "use slices.Equal instead of a manual element-wise comparison; nil and empty slices remain equal")
				}
				return true
			})
		},
	}, opts...)
}

func slicesEqualBody(info *types.Info, body *ast.BlockStmt, sig *types.Signature) bool {
	if len(body.List) != 3 || !sliceBoolReturn(info, body.List[2], true) {
		return false
	}
	guard, ok := body.List[0].(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil || len(guard.Body.List) != 1 || !sliceBoolReturn(info, guard.Body.List[0], false) {
		return false
	}
	lengths, ok := ast.Unparen(guard.Cond).(*ast.BinaryExpr)
	if !ok || lengths.Op != token.NEQ {
		return false
	}
	a, b := slicesEqualLength(info, sig, lengths.X), slicesEqualLength(info, sig, lengths.Y)
	if a == nil || b == nil {
		return false
	}
	as, aok := a.Type().Underlying().(*types.Slice)
	bs, bok := b.Type().Underlying().(*types.Slice)
	if !aok || !bok || !types.Identical(as.Elem(), bs.Elem()) || !types.Comparable(as.Elem()) {
		return false
	}
	// Equal has one shared slice type parameter, not independent S1 and S2.
	if !types.AssignableTo(a.Type(), b.Type()) && !types.AssignableTo(b.Type(), a.Type()) {
		return false
	}
	loop, ok := body.List[1].(*ast.RangeStmt)
	if !ok || loop.Tok != token.DEFINE || len(loop.Body.List) != 1 {
		return false
	}
	index, ok := loop.Key.(*ast.Ident)
	if !ok || index.Name == "_" || info.Defs[index] == nil {
		return false
	}
	source := sliceBoolParameter(info, sig, loop.X)
	if source != a && source != b {
		return false
	}
	other := b
	if source == b {
		other = a
	}
	var value types.Object
	if loop.Value != nil {
		id, ok := loop.Value.(*ast.Ident)
		if !ok {
			return false
		}
		if id.Name != "_" {
			value = info.Defs[id]
			if value == nil {
				return false
			}
		}
	}
	check, ok := loop.Body.List[0].(*ast.IfStmt)
	if !ok || check.Init != nil || check.Else != nil || len(check.Body.List) != 1 || !sliceBoolReturn(info, check.Body.List[0], false) {
		return false
	}
	compare, ok := ast.Unparen(check.Cond).(*ast.BinaryExpr)
	if !ok || compare.Op != token.NEQ {
		return false
	}
	left, right := compare.X, compare.Y
	if slicesEqualIndex(info, left, other, info.Defs[index]) {
		left, right = right, left
	}
	return (sliceBoolObject(info, left, value) || slicesEqualIndex(info, left, source, info.Defs[index])) && slicesEqualIndex(info, right, other, info.Defs[index])
}

func slicesEqualLength(info *types.Info, sig *types.Signature, expr ast.Expr) *types.Var {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || info.Uses[id] != types.Universe.Lookup("len") {
		return nil
	}
	return sliceBoolParameter(info, sig, call.Args[0])
}

func slicesEqualIndex(info *types.Info, expr ast.Expr, source, index types.Object) bool {
	access, ok := ast.Unparen(expr).(*ast.IndexExpr)
	return ok && sliceBoolObject(info, access.X, source) && sliceBoolObject(info, access.Index, index)
}
