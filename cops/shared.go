package cops

import (
	"go/ast"
	"go/types"
	"go/version"

	"github.com/dgageot/rubocop-go/cop"
)

// Consumer options refine policy without weakening the required Go version.
func configuredFileCop(c *cop.Func, opts ...cop.FuncOption) *cop.Func {
	minimum, stdlib := c.MinGoVersion, c.MinStdlibVersion
	for _, opt := range opts {
		opt(c)
	}
	if minimum != "" && (!version.IsValid(c.MinGoVersion) || version.Compare(c.MinGoVersion, minimum) < 0) {
		c.MinGoVersion = minimum
	}
	if stdlib != "" && (!version.IsValid(c.MinStdlibVersion) || version.Compare(c.MinStdlibVersion, stdlib) < 0) {
		c.MinStdlibVersion = stdlib
	}
	return c
}

// calleeObject resolves the object a call's callee denotes, whether the callee
// is a bare identifier (f()) or a selector (pkg.F() / recv.F()). Returns nil
// when info is unavailable or the callee has no resolvable object.
func calleeObject(info *types.Info, call *ast.CallExpr) types.Object {
	if info == nil {
		return nil
	}
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return info.Uses[fun.Sel]
	case *ast.Ident:
		return info.Uses[fun]
	default:
		return nil
	}
}

// forEachConstructionCallExpr invokes fn for every call expression that runs as
// part of executing body.
func forEachConstructionCallExpr(body *ast.BlockStmt, fn func(*ast.CallExpr)) {
	inspectConstructionBody(body, func(n ast.Node) {
		if call, ok := n.(*ast.CallExpr); ok {
			fn(call)
		}
	})
}
