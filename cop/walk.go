package cop

import (
	"go/ast"
	"slices"
	"strings"
)

// ForEachCall walks the file and calls fn for every *ast.CallExpr.
func (p *Pass) ForEachCall(fn func(*ast.CallExpr)) {
	ast.Inspect(p.File, func(n ast.Node) bool {
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

// IsCallTo reports whether call is a selector call to one of the given names
// on the package identifier pkg, e.g. IsCallTo(call, "fmt", "Println", "Printf").
//
// Note: this is a syntactic check on identifiers; if the cop has type info
// available, prefer the type-aware path.
func IsCallTo(call *ast.CallExpr, pkg string, names ...string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != pkg {
		return false
	}
	return slices.Contains(names, sel.Sel.Name)
}

// ImportPath returns the unquoted import path of an *ast.ImportSpec.
func ImportPath(imp *ast.ImportSpec) string {
	return strings.Trim(imp.Path.Value, `"`)
}
