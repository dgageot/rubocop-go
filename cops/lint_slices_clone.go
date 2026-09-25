package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintSlicesClone suggests shallow copies, including in typed tests.
// Rewrites must preserve nilness, destination types, and capacity contracts.
func NewLintSlicesClone(opts ...cop.FuncOption) *prog.Func {
	fileCop := newSlicesCloneFile(opts...)
	return &prog.Func{
		Meta: fileCop.Meta,
		Run: func(p *prog.Pass) {
			pkgs, err := loadModernizationPackages(p)
			if err != nil {
				p.Reportf(token.NoPos, "cannot inspect slice copies: %v", err)
				return
			}
			seen := make(map[string]bool)
			for _, pkg := range pkgs {
				if pkg.IllTyped {
					continue
				}
				for _, file := range pkg.Syntax {
					pass := &cop.Pass{Cop: fileCop, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
					if seen[pass.Filename()] || !fileCop.InScope(pass) {
						continue
					}
					seen[pass.Filename()] = true
					fileCop.Check(pass)
					for _, offense := range pass.Offenses() {
						p.ReportOffense(offense)
					}
				}
			}
		},
	}
}

func newSlicesCloneFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/SlicesClone",
			Description: "Consider slices.Clone for shallow copies, preserving nilness, types, and capacity contracts.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.21",
		Types:            true,
		Run:              checkSlicesClone,
	}, opts...)
}

func checkSlicesClone(p *cop.Pass) {
	if p.Info == nil || ast.IsGenerated(p.File) {
		return
	}
	ast.Inspect(p.File, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if cloneBuiltin(p, n, "append", 2) && n.Ellipsis.IsValid() && cloneSlice(p, n.Args[1]) && cloneEmptySlice(p, n.Args[0]) {
				p.Report(n, "use slices.Clone instead of appending to an empty slice; preserve nil/empty behavior and the destination type")
			}
		case *ast.BlockStmt:
			checkCloneStatements(p, n.List)
		case *ast.CaseClause:
			checkCloneStatements(p, n.Body)
		case *ast.CommClause:
			checkCloneStatements(p, n.Body)
		}
		return true
	})
}

func cloneBuiltin(p *cop.Pass, expr ast.Expr, name string, args int) bool {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	return ok && len(call.Args) == args && p.CalleeObject(call) == types.Universe.Lookup(name)
}

func cloneSlice(p *cop.Pass, expr ast.Expr) bool {
	t := p.Info.TypeOf(expr)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Slice)
	return ok
}

func cloneEmptySlice(p *cop.Pass, expr ast.Expr) bool {
	if !cloneSlice(p, expr) {
		return false
	}
	switch expr := ast.Unparen(expr).(type) {
	case *ast.CompositeLit:
		return len(expr.Elts) == 0
	case *ast.CallExpr:
		if len(expr.Args) == 1 && p.Info.Types[expr.Fun].IsType() {
			id, ok := ast.Unparen(expr.Args[0]).(*ast.Ident)
			return ok && p.Info.Uses[id] == types.Universe.Lookup("nil")
		}
		if cloneBuiltin(p, expr, "make", 2) {
			return cloneZero(p, expr.Args[1])
		}
	}
	return false
}

func cloneZero(p *cop.Pass, expr ast.Expr) bool {
	value := p.Info.Types[expr].Value
	return value != nil && value.ExactString() == "0"
}

func checkCloneStatements(p *cop.Pass, stmts []ast.Stmt) {
	for i := 0; i+1 < len(stmts); i++ {
		assign, ok := stmts[i].(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		dst, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || p.Info.Defs[dst] == nil || !cloneBuiltin(p, assign.Rhs[0], "make", 2) {
			continue
		}
		allocation := ast.Unparen(assign.Rhs[0]).(*ast.CallExpr)
		if !cloneBuiltin(p, allocation.Args[1], "len", 1) {
			continue
		}
		src := ast.Unparen(allocation.Args[1]).(*ast.CallExpr).Args[0]
		stmt, ok := stmts[i+1].(*ast.ExprStmt)
		if !ok || !cloneBuiltin(p, stmt.X, "copy", 2) {
			continue
		}
		copyCall := ast.Unparen(stmt.X).(*ast.CallExpr)
		to, ok := ast.Unparen(copyCall.Args[0]).(*ast.Ident)
		if !ok || p.Info.ObjectOf(to) != p.Info.Defs[dst] || !cloneSlice(p, src) || !cloneSameSource(p, src, copyCall.Args[1]) {
			continue
		}
		p.Report(assign, "use slices.Clone instead of make followed by copy; preserve non-nil empty results, the destination type, and any capacity contract")
	}
}

// Restrict repeated sources to stable names and field selections, not calls,
// indexing, or dereferences whose evaluation could change or panic.
func cloneSameSource(p *cop.Pass, a, b ast.Expr) bool {
	switch a := ast.Unparen(a).(type) {
	case *ast.Ident:
		b, ok := ast.Unparen(b).(*ast.Ident)
		return ok && p.Info.ObjectOf(a) != nil && p.Info.ObjectOf(a) == p.Info.ObjectOf(b)
	case *ast.SelectorExpr:
		b, ok := ast.Unparen(b).(*ast.SelectorExpr)
		return ok && p.Info.ObjectOf(a.Sel) != nil && p.Info.ObjectOf(a.Sel) == p.Info.ObjectOf(b.Sel) && cloneSameSource(p, a.X, b.X)
	}
	return false
}
