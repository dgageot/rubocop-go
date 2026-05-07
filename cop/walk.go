package cop

import (
	"go/ast"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

// ForEachCall walks the file and calls fn for every *ast.CallExpr.
func (p *Pass) ForEachCall(fn func(*ast.CallExpr)) {
	ForEachCallIn(p.File, fn)
}

// ForEachCallIn walks the given AST root and calls fn for every *ast.CallExpr.
// Use it when you want to scan a foreign file (e.g. one returned by
// Pass.ParseSibling) without changing your own pass.
func ForEachCallIn(root ast.Node, fn func(*ast.CallExpr)) {
	ast.Inspect(root, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			fn(call)
		}
		return true
	})
}

// ForEachAssign walks the file and calls fn for every *ast.AssignStmt.
func (p *Pass) ForEachAssign(fn func(*ast.AssignStmt)) {
	ast.Inspect(p.File, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			fn(a)
		}
		return true
	})
}

// ForEachFunc walks the file and calls fn for every *ast.FuncDecl declared at
// the top level (methods and functions, not function literals).
func (p *Pass) ForEachFunc(fn func(*ast.FuncDecl)) {
	for _, decl := range p.File.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn(fd)
		}
	}
}

// FuncDecl returns the top-level *ast.FuncDecl named name, or nil if no
// such declaration exists in the file. Methods (functions with a receiver)
// are skipped — name lookups would otherwise be ambiguous, and the helper
// is meant for the recurring "anchor a diagnostic on the dispatch function"
// pattern where the target is always a plain function.
//
//	anchor := ast.Node(p.File.Name)
//	if fn := p.FuncDecl("versions"); fn != nil {
//	    anchor = fn.Name
//	}
func (p *Pass) FuncDecl(name string) *ast.FuncDecl {
	for _, decl := range p.File.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name == nil {
			continue
		}
		if fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

// StructType returns the *ast.TypeSpec and *ast.StructType for the
// top-level type declaration named name, or (nil, nil) if no such struct
// exists in the file. Useful for "find the schema struct then iterate its
// fields" cops:
//
//	ts, st := p.StructType("HooksConfig")
//	if ts == nil { return }
//	for _, f := range st.Fields.List { ... }
func (p *Pass) StructType(name string) (*ast.TypeSpec, *ast.StructType) {
	var (
		ts *ast.TypeSpec
		st *ast.StructType
	)
	ForEachStructIn(p.File, func(t *ast.TypeSpec, s *ast.StructType) {
		if t.Name != nil && t.Name.Name == name {
			ts, st = t, s
		}
	})
	return ts, st
}

// PointerReceiverMethods returns the set of type names T for which the
// file declares `func (*T) name(...)`. Use it for syntactic
// interface-satisfaction checks ("does X implement SessionScoped?")
// without invoking the type checker:
//
//	impls := p.PointerReceiverMethods("GetSessionID")
//	if !impls[typeName] { /* missing */ }
//
// Methods with a value receiver, or with no usable receiver type, are
// excluded.
func (p *Pass) PointerReceiverMethods(name string) map[string]bool {
	return p.methodReceivers(name, true)
}

// ValueReceiverMethods is the value-receiver counterpart of
// [Pass.PointerReceiverMethods].
func (p *Pass) ValueReceiverMethods(name string) map[string]bool {
	return p.methodReceivers(name, false)
}

func (p *Pass) methodReceivers(name string, ptr bool) map[string]bool {
	out := map[string]bool{}
	p.ForEachFunc(func(fn *ast.FuncDecl) {
		if fn.Name == nil || fn.Name.Name != name {
			return
		}
		r, ok := Receiver(fn)
		if !ok || r.IsPointer != ptr {
			return
		}
		out[r.TypeName] = true
	})
	return out
}

// FirstMethodCall returns the first CallExpr in the file whose callee
// selector ends with method, or nil when no such call exists. Use it for
// the recurring "anchor on the first dispatch call, fall back to the
// package clause" pattern:
//
//	var anchor ast.Node = p.File.Name
//	if call := p.FirstMethodCall("RegisterBuiltin"); call != nil {
//	    anchor = call
//	}
func (p *Pass) FirstMethodCall(method string) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(p.File, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
			found = call
		}
		return true
	})
	return found
}

// FieldNames returns the comma-separated list of identifiers declared by
// f, suitable for diagnostic messages. Anonymous (embedded) fields fall
// back to the placeholder "<embedded>".
//
//	p.Report(field.Tag, "field %s ...", cop.FieldNames(field))
func FieldNames(f *ast.Field) string {
	if len(f.Names) == 0 {
		return "<embedded>"
	}
	names := make([]string, 0, len(f.Names))
	for _, n := range f.Names {
		names = append(names, n.Name)
	}
	return strings.Join(names, ", ")
}

// ForEachImport calls fn for every import in the file.
func (p *Pass) ForEachImport(fn func(*ast.ImportSpec)) {
	for _, imp := range p.File.Imports {
		fn(imp)
	}
}

// ForEachConst calls fn for every top-level *ast.GenDecl that declares
// constants. Useful for cops that audit `const Version = …` style declarations.
func (p *Pass) ForEachConst(fn func(*ast.GenDecl)) {
	for _, decl := range p.File.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "const" {
			continue
		}
		fn(gen)
	}
}

// ForEachStruct calls fn for every top-level type declaration whose right-hand
// side is a struct, passing both the *ast.TypeSpec and the *ast.StructType.
// Embedded structs and field types are not visited; only the top-level type
// declarations.
func (p *Pass) ForEachStruct(fn func(*ast.TypeSpec, *ast.StructType)) {
	ForEachStructIn(p.File, fn)
}

// ForEachStructIn is the foreign-file variant of ForEachStruct.
func ForEachStructIn(file *ast.File, fn func(*ast.TypeSpec, *ast.StructType)) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fn(ts, st)
		}
	}
}

// ForEachStructField calls fn for every field of every top-level struct in
// the file. The field's struct tag is unquoted and parsed once for the
// caller's convenience; it is the zero value if the field has no tag.
//
//	p.ForEachStructField(func(ts *ast.TypeSpec, f *ast.Field, tag reflect.StructTag) {
//	    name, ok := tag.Lookup("json")
//	    ...
//	})
func (p *Pass) ForEachStructField(fn func(*ast.TypeSpec, *ast.Field, reflect.StructTag)) {
	p.ForEachStruct(func(ts *ast.TypeSpec, st *ast.StructType) {
		if st.Fields == nil {
			return
		}
		for _, f := range st.Fields.List {
			fn(ts, f, FieldTag(f))
		}
	})
}

// FieldTag unquotes f.Tag.Value as a [reflect.StructTag]. Returns the
// zero value when the field has no tag or the tag literal cannot be
// unquoted.
//
// Use it together with [Pass.StructType] when you want to walk a
// specific struct's fields manually:
//
//	ts, st := p.StructType("HooksConfig")
//	for _, f := range st.Fields.List {
//	    opts, _ := cop.ParseTagOptions(cop.FieldTag(f), "json")
//	    ...
//	}
func FieldTag(f *ast.Field) reflect.StructTag {
	if f == nil || f.Tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(raw)
}

// ForEachMethodCall walks the file and calls fn for every CallExpr whose
// callee is a selector ending in method, e.g. "x.Register(...)" matches
// method == "Register". The receiver expression is intentionally not
// inspected — many dispatch-style rules don't care which value the method
// is called on.
func (p *Pass) ForEachMethodCall(method string, fn func(*ast.CallExpr)) {
	p.ForEachCall(func(call *ast.CallExpr) {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != method {
			return
		}
		fn(call)
	})
}

// IdentSetFromCalls scans the file for calls of the form `<anything>.method(args...)`
// and returns the set of identifier names appearing at the given argument
// position. Calls whose argument is not a bare *ast.Ident are skipped.
//
// Use it to answer the recurring "which X are wired up via this dispatch
// call?" question:
//
//	registered := p.IdentSetFromCalls("RegisterBuiltin", 0)
func (p *Pass) IdentSetFromCalls(method string, argIndex int) map[string]bool {
	out := map[string]bool{}
	p.ForEachMethodCall(method, func(call *ast.CallExpr) {
		if argIndex >= len(call.Args) {
			return
		}
		if id, ok := call.Args[argIndex].(*ast.Ident); ok {
			out[id.Name] = true
		}
	})
	return out
}

// SelectorReceivers returns the set of bare identifiers x appearing as the
// receiver of any call of the form `x.method(...)` in the file. Calls whose
// receiver is not a bare *ast.Ident are skipped.
//
// Use it for dispatch tables that key on the receiver itself (e.g. package
// aliases): `v0.Register(...)` should record "v0".
func (p *Pass) SelectorReceivers(method string) map[string]bool {
	out := map[string]bool{}
	p.ForEachMethodCall(method, func(call *ast.CallExpr) {
		sel := call.Fun.(*ast.SelectorExpr) // ForEachMethodCall guarantees this.
		if id, ok := sel.X.(*ast.Ident); ok {
			out[id.Name] = true
		}
	})
	return out
}

// StringConsts returns the set of top-level `const Name = "value"` string
// constants declared in the file, as a map from name to value. Constants
// without a string-literal value are ignored, as are iota-driven groups.
func (p *Pass) StringConsts() map[string]string {
	return StringConstsIn(p.File, nil)
}

// StringConstsMatching is like StringConsts but only returns entries whose
// name passes pred. A nil pred behaves like StringConsts.
func (p *Pass) StringConstsMatching(pred func(name string) bool) map[string]string {
	return StringConstsIn(p.File, pred)
}

// StringConstNodes is like StringConsts but returns the *ast.BasicLit
// node for each constant. Use it when you want to report a diagnostic
// anchored on the literal (e.g. "this string should be …").
func (p *Pass) StringConstNodes() map[string]*ast.BasicLit {
	return StringConstNodesIn(p.File, nil)
}

// StringConstNodesIn is the foreign-file variant of StringConstNodes.
func StringConstNodesIn(file *ast.File, pred func(name string) bool) map[string]*ast.BasicLit {
	out := map[string]*ast.BasicLit{}
	forEachStringConst(file, pred, func(name string, lit *ast.BasicLit, _ string) {
		out[name] = lit
	})
	return out
}

// StringConstsIn is the foreign-file variant of StringConsts. Use it when
// you parsed a sibling file (e.g. via Pass.ParseSibling) and want to
// extract its string constants.
func StringConstsIn(file *ast.File, pred func(name string) bool) map[string]string {
	out := map[string]string{}
	forEachStringConst(file, pred, func(name string, _ *ast.BasicLit, val string) {
		out[name] = val
	})
	return out
}

func forEachStringConst(file *ast.File, pred func(name string) bool, fn func(name string, lit *ast.BasicLit, val string)) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if pred != nil && !pred(name.Name) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				fn(name.Name, lit, val)
			}
		}
	}
}

// ImportPath returns the unquoted import path of an *ast.ImportSpec.
func ImportPath(imp *ast.ImportSpec) string {
	return strings.Trim(imp.Path.Value, `"`)
}
