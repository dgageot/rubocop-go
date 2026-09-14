package cop

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
)

// StringLiteral returns the unquoted value of a string literal.
func StringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// StringValue returns a string constant's value using available type information,
// falling back to string literals. Unresolved expressions return false.
func (p *Pass) StringValue(expr ast.Expr) (string, bool) {
	if p.Info != nil {
		if value := p.Info.Types[expr].Value; value != nil && value.Kind() == constant.String {
			return constant.StringVal(value), true
		}
	}
	return StringLiteral(expr)
}

// CalleeObject resolves the object denoted by a callee, including generic and
// parenthesized calls. The object may also be a variable, type, or builtin.
// It returns nil when type information is unavailable
// or the callee is not an identifier or selector. Imported symbols require
// resolved imports in Info; the runner's partial type checker may omit them.
func (p *Pass) CalleeObject(call *ast.CallExpr) types.Object {
	if p.Info == nil || call == nil {
		return nil
	}
	fun := call.Fun
	for {
		switch expr := fun.(type) {
		case *ast.ParenExpr:
			fun = expr.X
		case *ast.IndexExpr:
			fun = expr.X
		case *ast.IndexListExpr:
			fun = expr.X
		case *ast.Ident:
			return p.Info.Uses[expr]
		case *ast.SelectorExpr:
			return p.Info.Uses[expr.Sel]
		default:
			return nil
		}
	}
}
