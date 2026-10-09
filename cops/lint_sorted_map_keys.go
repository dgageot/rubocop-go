package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintSortedMapKeys reports nil-slice key collection followed by a plain sort.
func NewLintSortedMapKeys(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newSortedMapKeysFile(opts...))
}

func newSortedMapKeysFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/SortedMapKeys",
			Description: "Use slices.Sorted(maps.Keys(m)) for equivalent sorted map key collection.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.23",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BlockStmt:
					checkSortedMapKeys(p, n.List)
				case *ast.CaseClause:
					checkSortedMapKeys(p, n.Body)
				case *ast.CommClause:
					checkSortedMapKeys(p, n.Body)
				}
				return true
			})
		},
	}, opts...)
}

func checkSortedMapKeys(p *cop.Pass, stmts []ast.Stmt) {
	for i := 0; i+2 < len(stmts); i++ {
		dst := sortedMapKeysDeclaration(p, stmts[i])
		if dst == nil {
			continue
		}
		loop, ok := stmts[i+1].(*ast.RangeStmt)
		if !ok || !sortedMapKeysLoop(p, loop, dst) || !sortedMapKeysSort(p, stmts[i+2], dst) {
			continue
		}
		p.ReportAt(stmts[i].Pos(), stmts[i+2].End(), "use slices.Sorted(maps.Keys(src)) instead of collecting keys into a nil slice and sorting; preserve any downstream capacity contract")
	}
}

func sortedMapKeysDeclaration(p *cop.Pass, stmt ast.Stmt) *types.Var {
	var id *ast.Ident
	switch stmt := stmt.(type) {
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR || len(decl.Specs) != 1 {
			return nil
		}
		spec, ok := decl.Specs[0].(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) > 1 || len(spec.Values) == 1 && !sortedMapKeysNil(p, spec.Values[0]) {
			return nil
		}
		id = spec.Names[0]
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE || len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 || !sortedMapKeysNil(p, stmt.Rhs[0]) {
			return nil
		}
		id, _ = stmt.Lhs[0].(*ast.Ident)
	default:
		return nil
	}
	if id == nil {
		return nil
	}
	dst, ok := p.Info.Defs[id].(*types.Var)
	if !ok {
		return nil
	}
	slice, ok := dst.Type().Underlying().(*types.Slice)
	if !ok || !types.Identical(dst.Type(), slice) {
		return nil
	}
	key, ok := slice.Elem().Underlying().(*types.Basic)
	if !ok || key.Info()&(types.IsInteger|types.IsString) == 0 {
		return nil
	}
	return dst
}

func sortedMapKeysNil(p *cop.Pass, expr ast.Expr) bool {
	if id, ok := ast.Unparen(expr).(*ast.Ident); ok {
		return p.Info.ObjectOf(id) == types.Universe.Lookup("nil")
	}
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !p.Info.Types[call.Fun].IsType() {
		return false
	}
	id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	return ok && p.Info.ObjectOf(id) == types.Universe.Lookup("nil")
}

func sortedMapKeysLoop(p *cop.Pass, loop *ast.RangeStmt, dst *types.Var) bool {
	if loop.Tok != token.DEFINE || len(loop.Body.List) != 1 {
		return false
	}
	key, ok := loop.Key.(*ast.Ident)
	if !ok || key.Name == "_" || p.Info.Defs[key] == nil {
		return false
	}
	if loop.Value != nil {
		value, ok := loop.Value.(*ast.Ident)
		if !ok || value.Name != "_" {
			return false
		}
	}
	srcID, ok := ast.Unparen(loop.X).(*ast.Ident)
	if !ok {
		return false
	}
	src, ok := p.Info.ObjectOf(srcID).(*types.Var)
	if !ok || src.IsField() || src.Parent() == nil || src.Pkg() == nil || src.Parent() == src.Pkg().Scope() {
		return false
	}
	mapType, ok := src.Type().Underlying().(*types.Map)
	if !ok || !types.Identical(mapType.Key(), dst.Type().Underlying().(*types.Slice).Elem()) {
		return false
	}
	assign, ok := loop.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || !sortedMapKeysIdent(p, assign.Lhs[0], dst) {
		return false
	}
	appendCall, ok := ast.Unparen(assign.Rhs[0]).(*ast.CallExpr)
	return ok && !appendCall.Ellipsis.IsValid() && len(appendCall.Args) == 2 && p.CalleeObject(appendCall) == types.Universe.Lookup("append") && sortedMapKeysIdent(p, appendCall.Args[0], dst) && sortedMapKeysIdent(p, appendCall.Args[1], p.Info.Defs[key])
}

func sortedMapKeysSort(p *cop.Pass, stmt ast.Stmt, dst *types.Var) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(expr.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() || !sortedMapKeysIdent(p, call.Args[0], dst) {
		return false
	}
	fn, ok := p.CalleeObject(call).(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}
	return fn.Pkg().Path() == "sort" && (fn.Name() == "Strings" || fn.Name() == "Ints") || fn.Pkg().Path() == "slices" && fn.Name() == "Sort"
}

func sortedMapKeysIdent(p *cop.Pass, expr ast.Expr, obj types.Object) bool {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	return ok && obj != nil && p.Info.ObjectOf(id) == obj
}
