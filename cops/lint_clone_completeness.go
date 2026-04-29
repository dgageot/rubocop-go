package cops

import (
	"go/ast"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
)

// LintCloneCompleteness checks that Clone() methods copy every pointer, slice,
// and map field of the receiver struct. When a new field is added to a struct
// but the Clone() method is not updated, the shallow copy can lead to shared
// backing data and subtle mutation or data-race bugs.
type LintCloneCompleteness struct{}

func init() { cop.Register(&LintCloneCompleteness{}) }

func (*LintCloneCompleteness) Name() string        { return "Lint/CloneCompleteness" }
func (*LintCloneCompleteness) Description() string { return "Clone() must handle all pointer/slice/map fields" }
func (*LintCloneCompleteness) Severity() cop.Severity { return cop.Error }

// NeedsTypes opts the cop into type information.
func (*LintCloneCompleteness) NeedsTypes() bool { return true }

// Check inspects Clone() methods for missing field copies.
func (c *LintCloneCompleteness) Check(p *cop.Pass) []cop.Offense {
	if p.Info == nil {
		return nil
	}

	var offenses []cop.Offense

	for _, decl := range p.File.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "Clone" || fn.Body == nil {
			continue
		}

		// Resolve the receiver's underlying struct type, unwrapping pointers.
		recvType := resolveRecvStruct(fn, p.Info)
		if recvType == nil {
			continue
		}

		// Collect all fields that need deep copying (pointer, slice, map),
		// including fields from embedded structs (flattened).
		needsCopy := deepCopyFields(recvType)
		if len(needsCopy) == 0 {
			continue
		}

		// Collect field names referenced in the Clone body.
		referenced := referencedFields(fn.Body)

		for _, name := range needsCopy {
			if !referenced[name] {
				offenses = append(offenses, cop.NewOffense(c, p.FileSet, fn.Name,
					"Clone() does not copy field '"+name+"' (pointer/slice/map)"))
			}
		}
	}

	return offenses
}

// resolveRecvStruct returns the *types.Struct underlying the receiver of fn,
// unwrapping any pointer indirection. Returns nil if it's not a struct.
func resolveRecvStruct(fn *ast.FuncDecl, info *types.Info) *types.Struct {
	if len(fn.Recv.List) == 0 {
		return nil
	}

	recvExpr := fn.Recv.List[0].Type
	t := info.TypeOf(recvExpr)
	if t == nil {
		return nil
	}

	// Unwrap pointer.
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	// Unwrap named type.
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}

	st, _ := t.(*types.Struct)
	return st
}

// deepCopyFields returns the names of all fields in st (including fields from
// embedded structs) whose types are pointers, slices, or maps.
func deepCopyFields(st *types.Struct) []string {
	var names []string
	for i := range st.NumFields() {
		f := st.Field(i)

		// If embedded struct, recurse into it.
		if f.Embedded() {
			if inner := embeddedStruct(f.Type()); inner != nil {
				names = append(names, deepCopyFields(inner)...)
				continue
			}
		}

		// Skip unexported fields — Clone() methods typically can't (and
		// shouldn't) deep-copy unexported fields from other packages, and
		// for same-package unexported fields with non-reference types
		// (e.g. sync.Mutex) a copy is intentionally skipped.
		if !f.Exported() {
			continue
		}

		if needsDeepCopy(f.Type()) {
			names = append(names, f.Name())
		}
	}
	return names
}

// embeddedStruct unwraps a (possibly pointer, possibly named) type down to
// a *types.Struct, or returns nil.
func embeddedStruct(t types.Type) *types.Struct {
	t = deref(t)
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

// referencedFields collects all selector names used in a function body.
// e.g. for `x.Foo` it records "Foo".
func referencedFields(body *ast.BlockStmt) map[string]bool {
	refs := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok {
			refs[sel.Sel.Name] = true
		}
		return true
	})
	return refs
}
