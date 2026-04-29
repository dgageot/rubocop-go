package cop

import "go/ast"

// IsSelector reports whether expr is the selector "x.sel" where x is a
// bare identifier with the given name. This is a thin wrapper around the
// AST shape that gets re-implemented over and over in cops:
//
//	if cop.IsSelector(assign.Lhs[0], recv, "Cache") { ... }
func IsSelector(expr ast.Expr, x, sel string) bool {
	got, name, ok := MatchSelector(expr)
	return ok && got == x && name == sel
}

// MatchSelector returns the components of a selector expression of the
// form "x.sel" where x is a bare identifier. The third result is false
// when expr has any other shape (e.g. nested selectors a.b.c, indexed
// receivers, or call results).
func MatchSelector(expr ast.Expr) (x, sel string, ok bool) {
	se, isSel := expr.(*ast.SelectorExpr)
	if !isSel {
		return "", "", false
	}
	id, isIdent := se.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	return id.Name, se.Sel.Name, true
}

// Receiver describes a function receiver. It is the data shape that
// receiver-aware cops keep recomputing by hand.
type ReceiverInfo struct {
	Name      string // the bound name, e.g. "p" — empty for anonymous receivers
	TypeName  string // the receiver's type name, e.g. "Pass"
	IsPointer bool   // true if the receiver is *Pass rather than Pass
}

// Receiver decodes fn's receiver. The second result is false for plain
// functions (no receiver) and for receivers with unusual shapes (generic
// instantiations, etc.) that the cop is unlikely to understand anyway.
func Receiver(fn *ast.FuncDecl) (ReceiverInfo, bool) {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ReceiverInfo{}, false
	}
	field := fn.Recv.List[0]

	var (
		typeExpr = field.Type
		ptr      bool
	)
	if star, isStar := typeExpr.(*ast.StarExpr); isStar {
		typeExpr = star.X
		ptr = true
	}

	id, isIdent := typeExpr.(*ast.Ident)
	if !isIdent {
		return ReceiverInfo{}, false
	}

	info := ReceiverInfo{TypeName: id.Name, IsPointer: ptr}
	if len(field.Names) > 0 {
		info.Name = field.Names[0].Name
	}
	return info, true
}
