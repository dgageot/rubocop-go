package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintSlicesContains suggests slices.Contains for exact membership helpers.
// Only stable parameters or constants may be compared with each element.
func NewLintSlicesContains(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newSlicesContainsFile(opts...))
}

func newSlicesContainsFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/SlicesContains",
			Description: "Use slices.Contains for simple slice membership helpers.",
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
				if body != nil && slicesContainsBody(p.Info, body, sig) {
					p.Report(body, "use slices.Contains instead of a manual slice membership loop")
				}
				return true
			})
		},
	}, opts...)
}

func slicesContainsBody(info *types.Info, body *ast.BlockStmt, sig *types.Signature) bool {
	if len(body.List) != 2 || !sliceBoolReturn(info, body.List[1], false) {
		return false
	}
	loop, ok := body.List[0].(*ast.RangeStmt)
	if !ok || loop.Tok != token.DEFINE || len(loop.Body.List) != 1 {
		return false
	}
	key, ok := loop.Key.(*ast.Ident)
	if !ok || key.Name != "_" {
		return false
	}
	value, ok := loop.Value.(*ast.Ident)
	if !ok || value.Name == "_" || info.Defs[value] == nil {
		return false
	}
	source := sliceBoolParameter(info, sig, loop.X)
	if source == nil {
		return false
	}
	slice, ok := source.Type().Underlying().(*types.Slice)
	if !ok || !types.Comparable(slice.Elem()) {
		return false
	}
	guard, ok := loop.Body.List[0].(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil || len(guard.Body.List) != 1 || !sliceBoolReturn(info, guard.Body.List[0], true) {
		return false
	}
	compare, ok := ast.Unparen(guard.Cond).(*ast.BinaryExpr)
	if !ok || compare.Op != token.EQL {
		return false
	}
	element, needle := compare.X, compare.Y
	if sliceBoolObject(info, needle, info.Defs[value]) {
		element, needle = needle, element
	}
	if !sliceBoolObject(info, element, info.Defs[value]) {
		return false
	}
	id, ok := ast.Unparen(needle).(*ast.Ident)
	if info.Types[needle].Value == nil && (!ok || (sliceBoolParameter(info, sig, needle) == nil && info.Uses[id] != types.Universe.Lookup("nil"))) {
		return false
	}
	needleType := info.TypeOf(needle)
	if needleType == nil {
		return false
	}
	basic, untyped := needleType.Underlying().(*types.Basic)
	// A typed argument must infer the same E as the slice; assignability alone
	// would wrongly recommend Contains([]any, int) without an explicit conversion.
	return types.Identical(needleType, slice.Elem()) || (untyped && basic.Info()&types.IsUntyped != 0 && types.AssignableTo(needleType, slice.Elem()))
}

// Exact bodies keep parameters stable: there are no assignments, captures, or
// calls that could make replacing repeated reads with one argument evaluation unsafe.
func sliceBoolFunction(info *types.Info, node ast.Node) (*ast.BlockStmt, *types.Signature) {
	var body *ast.BlockStmt
	var typ types.Type
	switch fn := node.(type) {
	case *ast.FuncDecl:
		body = fn.Body
		if obj := info.Defs[fn.Name]; obj != nil {
			typ = obj.Type()
		}
	case *ast.FuncLit:
		body, typ = fn.Body, info.TypeOf(fn)
	default:
		return nil, nil
	}
	sig, ok := typ.(*types.Signature)
	if !ok || body == nil || sig.Variadic() || sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), types.Typ[types.Bool]) {
		return nil, nil
	}
	return body, sig
}

func sliceBoolReturn(info *types.Info, stmt ast.Stmt, want bool) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	value := info.Types[ret.Results[0]].Value
	return value != nil && value.Kind() == constant.Bool && constant.BoolVal(value) == want
}

func sliceBoolObject(info *types.Info, expr ast.Expr, obj types.Object) bool {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	return ok && obj != nil && info.Uses[id] == obj
}

func sliceBoolParameter(info *types.Info, sig *types.Signature, expr ast.Expr) *types.Var {
	for param := range sig.Params().Variables() {
		if sliceBoolObject(info, expr, param) {
			return param
		}
	}
	return nil
}
