package cops

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintCloneCompleteness returns a cop that checks Clone() methods copy
// every pointer, slice, and map field of the receiver struct. When a new
// field is added to a struct but the Clone() method is not updated, the
// shallow copy can lead to shared backing data and subtle mutation or
// data-race bugs.
func NewLintCloneCompleteness() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Lint/CloneCompleteness",
		Description: "Clone() must handle all pointer/slice/map fields",
		Severity:    cop.Error,
	}, func(p *cop.Pass) {
		if p.Info == nil {
			return
		}

		p.ForEachFunc(func(fn *ast.FuncDecl) {
			if fn.Recv == nil || fn.Name.Name != "Clone" || fn.Body == nil {
				return
			}

			// Resolve the receiver's underlying struct type, unwrapping pointers.
			recvType := cloneValueType(p.Info.TypeOf(fn.Recv.List[0].Type))
			recvStruct := embeddedStruct(recvType)
			if recvStruct == nil {
				return
			}

			// Collect all fields that need deep copying (pointer, slice, map),
			// including fields from embedded structs (flattened).
			needsCopy := cloneFields(recvStruct, p.Package)
			if len(needsCopy) == 0 {
				return
			}

			var source types.Object
			if len(fn.Recv.List[0].Names) > 0 {
				source = p.Info.Defs[fn.Recv.List[0].Names[0]]
			}
			handled := handledCloneFields(fn.Body, p.Info, recvType, source)

			for _, field := range needsCopy {
				if !handled[field.name] {
					p.Reportf(fn.Name, "Clone() does not copy field '%s' (pointer/slice/map)", field.name)
				}
			}
		})
	}, cop.WithTypes())
}

// Keep named-type identity when comparing clone destinations.
func cloneValueType(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	return types.Unalias(deref(types.Unalias(t)))
}

type cloneField struct {
	name string
}

// Embedded pointers are copy obligations themselves, not recursive struct walks.
func cloneFields(st *types.Struct, pkg *types.Package) []cloneField {
	var fields []cloneField
	var walk func(*types.Struct, string)
	walk = func(st *types.Struct, prefix string) {
		for field := range st.Fields() {
			name := prefix + field.Name()
			if field.Embedded() && !needsDeepCopy(field.Type()) {
				if inner := embeddedStruct(field.Type()); inner != nil {
					walk(inner, name+".")
				}
				continue
			}
			if !field.Exported() && field.Pkg() != pkg {
				continue
			}
			if needsDeepCopy(field.Type()) {
				fields = append(fields, cloneField{name: name})
			}
		}
	}
	walk(st, "")
	return fields
}

// embeddedStruct unwraps a (possibly pointer, possibly named) type down to
// a *types.Struct, or returns nil.
func embeddedStruct(t types.Type) *types.Struct {
	if t == nil {
		return nil
	}
	t = deref(types.Unalias(t))
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}
	st, _ := t.(*types.Struct)
	return st
}

// needsDeepCopy reports whether a type requires explicit deep copying in a
// Clone() method (pointer, slice, or map).
func needsDeepCopy(t types.Type) bool {
	t = t.Underlying()
	switch t.(type) {
	case *types.Pointer, *types.Slice, *types.Map:
		return true
	}
	return false
}

// deref unwraps one level of pointer indirection.
func deref(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// Look for destination writes, keeping distinct embedding paths separate.
func handledCloneFields(body *ast.BlockStmt, info *types.Info, receiver types.Type, source types.Object) map[string]bool {
	handled := make(map[string]bool)
	var mark func(string, *types.Var, ast.Expr)
	var literal func(*ast.CompositeLit, string)
	mark = func(path string, field *types.Var, value ast.Expr) {
		if shallowCloneSource(value, info, source) {
			return
		}
		if needsDeepCopy(field.Type()) {
			handled[path] = true
			return
		}
		if !field.Embedded() {
			return
		}
		value = ast.Unparen(value)
		if cl, ok := value.(*ast.CompositeLit); ok {
			literal(cl, path+".")
			return
		}
		if _, ok := value.(*ast.CallExpr); ok {
			if inner := embeddedStruct(field.Type()); inner != nil {
				for _, nested := range cloneFields(inner, field.Pkg()) {
					handled[path+"."+nested.name] = true
				}
			}
		}
	}
	literal = func(cl *ast.CompositeLit, prefix string) {
		st := embeddedStruct(info.TypeOf(cl))
		if st == nil {
			return
		}
		for index, elt := range cl.Elts {
			var field *types.Var
			value := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				value = kv.Value
				for candidate := range st.Fields() {
					if candidate.Name() == key.Name {
						field = candidate
						break
					}
				}
			} else if index < st.NumFields() {
				field = st.Field(index)
			}
			if field != nil {
				mark(prefix+field.Name(), field, value)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				path, field := cloneDestination(lhs, info, receiver)
				if field == nil {
					continue
				}
				if len(node.Rhs) == len(node.Lhs) {
					if cloneSelfCopy(lhs, node.Rhs[i], info, receiver) {
						continue
					}
					mark(path, field, node.Rhs[i])
				} else if len(node.Rhs) == 1 {
					if tuple, ok := info.TypeOf(node.Rhs[0]).(*types.Tuple); ok && i < tuple.Len() && types.AssignableTo(tuple.At(i).Type(), field.Type()) {
						mark(path, field, node.Rhs[0])
					}
				}
			}
		case *ast.CompositeLit:
			if types.Identical(cloneValueType(info.TypeOf(node)), receiver) {
				literal(node, "")
			}
		case *ast.CallExpr:
			if id, ok := ast.Unparen(node.Fun).(*ast.Ident); ok && info.Uses[id] == types.Universe.Lookup("copy") && len(node.Args) == 2 {
				if path, field := cloneDestination(node.Args[0], info, receiver); field != nil {
					handled[path] = true
				}
			}
		}
		return true
	})
	return handled
}

func shallowCloneSource(value ast.Expr, info *types.Info, source types.Object) bool {
	value = ast.Unparen(value)
	switch expr := value.(type) {
	case *ast.Ident:
		return expr.Name == "nil"
	case *ast.UnaryExpr:
		if expr.Op == token.AND {
			return cloneSourceRoot(expr.X, info) == source && source != nil
		}
	case *ast.SelectorExpr:
		if field, ok := info.Uses[expr.Sel].(*types.Var); ok {
			return source != nil && cloneSourceRoot(expr, info) == source && (needsDeepCopy(field.Type()) || embeddedStruct(field.Type()) != nil)
		}
	case *ast.SliceExpr:
		return shallowCloneSource(expr.X, info, source)
	}
	return false
}

func cloneDestination(expr ast.Expr, info *types.Info, receiver types.Type) (string, *types.Var) {
	expr = ast.Unparen(expr)
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}
	var prefix string
	if parent, ok := ast.Unparen(sel.X).(*ast.SelectorExpr); ok {
		path, field := cloneDestination(parent, info, receiver)
		if field == nil {
			return "", nil
		}
		prefix = path + "."
	} else if !types.Identical(cloneValueType(info.TypeOf(sel.X)), receiver) {
		return "", nil
	}
	var pkg *types.Package
	if obj := info.Uses[sel.Sel]; obj != nil {
		pkg = obj.Pkg()
	}
	obj, indices, _ := types.LookupFieldOrMethod(info.TypeOf(sel.X), true, pkg, sel.Sel.Name)
	field, ok := obj.(*types.Var)
	if !ok {
		return "", nil
	}
	st := embeddedStruct(info.TypeOf(sel.X))
	var path strings.Builder
	path.WriteString(prefix)
	for i, index := range indices {
		if st == nil || index >= st.NumFields() {
			return "", nil
		}
		member := st.Field(index)
		path.WriteString(member.Name())
		if i < len(indices)-1 {
			path.WriteByte('.')
			st = embeddedStruct(member.Type())
		}
	}
	return path.String(), field
}

func cloneSourceRoot(expr ast.Expr, info *types.Info) types.Object {
	switch expr := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return info.Uses[expr]
	case *ast.SelectorExpr:
		return cloneSourceRoot(expr.X, info)
	case *ast.IndexExpr:
		return cloneSourceRoot(expr.X, info)
	case *ast.SliceExpr:
		return cloneSourceRoot(expr.X, info)
	case *ast.UnaryExpr:
		if expr.Op == token.AND {
			return cloneSourceRoot(expr.X, info)
		}
	case *ast.StarExpr:
		return cloneSourceRoot(expr.X, info)
	}
	return nil
}

// Assigning a destination field to itself does not detach its backing storage.
func cloneSelfCopy(lhs, rhs ast.Expr, info *types.Info, receiver types.Type) bool {
	rhs = ast.Unparen(rhs)
	for {
		slice, ok := rhs.(*ast.SliceExpr)
		if !ok {
			break
		}
		rhs = ast.Unparen(slice.X)
	}
	leftPath, leftField := cloneDestination(lhs, info, receiver)
	rightPath, rightField := cloneDestination(rhs, info, receiver)
	return leftField != nil && rightField != nil && leftPath == rightPath && cloneSourceRoot(lhs, info) == cloneSourceRoot(rhs, info)
}
