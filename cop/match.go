package cop

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
)

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

// IsCallTo reports whether call is a selector call to one of the given names
// on the package identifier pkg, e.g. IsCallTo(call, "fmt", "Println", "Printf").
//
// Use [CallTo] when you also need the matched name (e.g. to interpolate it
// into a diagnostic).
//
// Note: this is a syntactic check on identifiers; if the cop has type info
// available, prefer the type-aware path.
func IsCallTo(call *ast.CallExpr, pkg string, names ...string) bool {
	_, ok := CallTo(call, pkg, names...)
	return ok
}

// CallTo is like [IsCallTo] but returns the matched method name when call
// is a selector call "pkg.Name(...)" for one of the given names.
//
//	name, ok := cop.CallTo(call, "slog", "Debug", "Info", "Warn", "Error")
//	if ok { p.Report(call, "prefer slog.%sContext", name) }
func CallTo(call *ast.CallExpr, pkg string, names ...string) (string, bool) {
	got, sel, ok := MatchSelector(call.Fun)
	if !ok || got != pkg {
		return "", false
	}
	if !slices.Contains(names, sel) {
		return "", false
	}
	return sel, true
}

// StringField returns the unquoted string-literal value bound to key in
// the composite literal cl, e.g. given `&FooEvent{Type: "foo"}` and key
// "Type", returns ("foo", true). The second result is false when the key
// is absent, the value is not a string literal, or the literal cannot be
// unquoted.
func StringField(cl *ast.CompositeLit, key string) (string, bool) {
	lit, ok := BasicLitField(cl, key, token.STRING)
	if !ok {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return val, true
}

// BasicLitField returns the *ast.BasicLit value bound to key in the
// composite literal cl, restricted to literals of the given kind
// (token.STRING, token.INT, ...). The second result is false when the key
// is absent or the value is not a basic literal of that kind.
func BasicLitField(cl *ast.CompositeLit, key string, kind token.Token) (*ast.BasicLit, bool) {
	expr, ok := CompositeLitField(cl, key)
	if !ok {
		return nil, false
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != kind {
		return nil, false
	}
	return lit, true
}

// CompositeLitField returns the value expression bound to key in cl, or
// (nil, false) when the key is absent. Only key/value elements with a bare
// identifier key (the common case for struct literals) are recognised.
func CompositeLitField(cl *ast.CompositeLit, key string) (ast.Expr, bool) {
	if cl == nil {
		return nil, false
	}
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok || id.Name != key {
			continue
		}
		return kv.Value, true
	}
	return nil, false
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

// IsNullaryFunc reports whether fn is exactly `func ...() ResultType` —
// no parameters and exactly one named or unnamed result whose syntactic
// type is the bare identifier resultType.
//
// Use it to recognise framework methods by signature shape:
//
//	if !cop.IsNullaryFunc(fn, "string") { return } // matches View() string
func IsNullaryFunc(fn *ast.FuncDecl, resultType string) bool {
	if fn == nil {
		return false
	}
	return IsNullarySig(fn.Type, resultType)
}

// IsNullarySig is the *ast.FuncType counterpart of [IsNullaryFunc]. Use it
// when matching function-typed expressions inside other types — e.g. the
// value type of a registry map: `map[string]func() Event`.
func IsNullarySig(typ *ast.FuncType, resultType string) bool {
	if typ == nil {
		return false
	}
	if typ.Params != nil && len(typ.Params.List) > 0 {
		return false
	}
	if typ.Results == nil || len(typ.Results.List) != 1 {
		return false
	}
	id, ok := typ.Results.List[0].Type.(*ast.Ident)
	return ok && id.Name == resultType
}
