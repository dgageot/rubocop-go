package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintCutPrefix reports paired prefix checks and removals.
func NewLintCutPrefix(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newCutPrefixFile(opts...))
}

func newCutPrefixFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/CutPrefix",
			Description: "Use strings.CutPrefix for paired prefix checks and removals.",
			Severity:    cop.Warning,
		},
		MinStdlibVersion: "go1.20",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.IfStmt:
					checkCutPrefix(p, node.Cond, true, node.Body.List)
				case *ast.SwitchStmt:
					if node.Tag == nil {
						fallthroughCase := false
						for _, stmt := range node.Body.List {
							clause := stmt.(*ast.CaseClause)
							if !fallthroughCase && len(clause.List) == 1 {
								checkCutPrefix(p, clause.List[0], true, clause.Body)
							}
							fallthroughCase = cutPrefixFallsThrough(clause.Body)
						}
					}
				case *ast.BlockStmt:
					checkCutPrefixGuards(p, node.List)
				case *ast.CaseClause:
					checkCutPrefixGuards(p, node.Body)
				case *ast.CommClause:
					checkCutPrefixGuards(p, node.Body)
				}
				return true
			})
		},
	}, opts...)
}

func cutPrefixFallsThrough(stmts []ast.Stmt) bool {
	for _, stmt := range slices.Backward(stmts) {
		for {
			label, ok := stmt.(*ast.LabeledStmt)
			if !ok {
				break
			}
			stmt = label.Stmt
		}
		if _, empty := stmt.(*ast.EmptyStmt); empty {
			continue
		}
		branch, ok := stmt.(*ast.BranchStmt)
		return ok && branch.Tok == token.FALLTHROUGH
	}
	return false
}

func checkCutPrefixGuards(p *cop.Pass, stmts []ast.Stmt) {
	for i, stmt := range stmts {
		guard, ok := stmt.(*ast.IfStmt)
		if !ok || guard.Else != nil || len(guard.Body.List) != 1 {
			continue
		}
		switch exit := guard.Body.List[0].(type) {
		case *ast.ReturnStmt:
		case *ast.BranchStmt:
			if exit.Label != nil || exit.Tok != token.BREAK && exit.Tok != token.CONTINUE {
				continue
			}
		default:
			continue
		}
		checkCutPrefix(p, guard.Cond, false, stmts[i+1:])
	}
}

func checkCutPrefix(p *cop.Pass, cond ast.Expr, positive bool, stmts []ast.Stmt) {
	cond = ast.Unparen(cond)
	if not, ok := cond.(*ast.UnaryExpr); ok && not.Op == token.NOT {
		checkCutPrefix(p, not.X, !positive, stmts)
		return
	}
	if binary, ok := cond.(*ast.BinaryExpr); ok {
		if positive && binary.Op == token.LAND || !positive && binary.Op == token.LOR {
			// Calls in the remaining condition may change the checked arguments.
			if cutPrefixInert(p.Info, binary.Y) {
				checkCutPrefix(p, binary.X, positive, stmts)
			}
			if cutPrefixInert(p.Info, binary.X) {
				checkCutPrefix(p, binary.Y, positive, stmts)
			}
		}
		return
	}
	call := cutPrefixStringsCall(p.Info, cond, "HasPrefix")
	if !positive || call == nil || !cutPrefixSame(p.Info, call.Args[0], call.Args[0]) || !cutPrefixSame(p.Info, call.Args[1], call.Args[1]) {
		return
	}
	for _, stmt := range stmts {
		if cutPrefixUse(p.Info, stmt, call) {
			p.Report(call, "use strings.CutPrefix instead of HasPrefix followed by TrimPrefix or slicing; preserve short-circuit evaluation and assignments to existing variables")
			return
		}
		if !cutPrefixLocalInit(p.Info, stmt) {
			return
		}
	}
}

// Only stable expressions can be evaluated once in place of two evaluations.
func cutPrefixSame(info *types.Info, a, b ast.Expr) bool {
	a, b = ast.Unparen(a), ast.Unparen(b)
	if av, bv := info.Types[a].Value, info.Types[b].Value; av != nil && bv != nil {
		return av.Kind() == bv.Kind() && constant.Compare(av, token.EQL, bv)
	}
	switch a := a.(type) {
	case *ast.Ident:
		b, ok := b.(*ast.Ident)
		return ok && info.Uses[a] != nil && info.Uses[a] == info.Uses[b]
	case *ast.SelectorExpr:
		b, ok := b.(*ast.SelectorExpr)
		return ok && info.Uses[a.Sel] != nil && info.Uses[a.Sel] == info.Uses[b.Sel] && cutPrefixSame(info, a.X, b.X)
	case *ast.BinaryExpr:
		b, ok := b.(*ast.BinaryExpr)
		return ok && a.Op == token.ADD && b.Op == a.Op && cutPrefixSame(info, a.X, b.X) && cutPrefixSame(info, a.Y, b.Y)
	}
	return false
}

func cutPrefixRemoval(info *types.Info, expr ast.Expr, check *ast.CallExpr) bool {
	if trim := cutPrefixStringsCall(info, expr, "TrimPrefix"); trim != nil {
		return cutPrefixSame(info, trim.Args[0], check.Args[0]) && cutPrefixSame(info, trim.Args[1], check.Args[1])
	}
	slice, ok := ast.Unparen(expr).(*ast.SliceExpr)
	if !ok || !types.Identical(info.TypeOf(slice), types.Typ[types.String]) || slice.Low == nil || slice.High != nil || slice.Slice3 || !cutPrefixSame(info, slice.X, check.Args[0]) {
		return false
	}
	if length, ok := ast.Unparen(slice.Low).(*ast.CallExpr); ok && len(length.Args) == 1 && calleeObject(info, length) == types.Universe.Lookup("len") {
		return cutPrefixSame(info, length.Args[0], check.Args[1])
	}
	prefix, offset := info.Types[check.Args[1]].Value, info.Types[slice.Low].Value
	if prefix == nil || prefix.Kind() != constant.String || offset == nil || offset.Kind() != constant.Int {
		return false
	}
	n, ok := constant.Int64Val(offset)
	return ok && n == int64(len(constant.StringVal(prefix)))
}

// Calls enclosing the removal execute after it; earlier calls may mutate inputs.
func cutPrefixUse(info *types.Info, stmt ast.Stmt, check *ast.CallExpr) bool {
	var exprs []ast.Expr
	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		exprs = append(exprs, stmt.Lhs...)
		exprs = append(exprs, stmt.Rhs...)
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || len(decl.Specs) != 1 {
			return false
		}
		value, ok := decl.Specs[0].(*ast.ValueSpec)
		if !ok {
			return false
		}
		exprs = value.Values
	case *ast.ReturnStmt:
		exprs = stmt.Results
	case *ast.ExprStmt:
		exprs = []ast.Expr{stmt.X}
	case *ast.RangeStmt:
		// A constant-length array operand may not be evaluated at all.
		value, _ := stmt.Value.(*ast.Ident)
		if stmt.Value == nil || value != nil && value.Name == "_" {
			typ := info.TypeOf(stmt.X)
			if typ == nil {
				return false
			}
			if ptr, ok := typ.Underlying().(*types.Pointer); ok {
				typ = ptr.Elem()
			}
			if _, ok := typ.Underlying().(*types.Array); ok {
				return false
			}
		}
		exprs = []ast.Expr{stmt.X}
	default:
		return false
	}
	var removal ast.Expr
	barrier := token.NoPos
	for _, expr := range exprs {
		ast.Inspect(expr, func(n ast.Node) bool {
			if n == nil || removal != nil {
				return false
			}
			if e, ok := n.(ast.Expr); ok {
				// Constant expressions can contain unevaluated operands (e.g. Sizeof).
				if info.Types[e].Value != nil {
					return false
				}
				if call, ok := e.(*ast.CallExpr); ok {
					// Generic Sizeof/Alignof results need not be constants.
					obj := calleeObject(info, call)
					if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "unsafe" {
						switch obj.Name() {
						case "Sizeof", "Alignof", "Offsetof":
							return false
						}
					}
				}
				if cutPrefixRemoval(info, e, check) {
					removal = e
					return false
				}
			}
			var pos token.Pos
			switch node := n.(type) {
			case *ast.FuncLit:
				pos = node.Pos()
			case *ast.CallExpr:
				pos = node.End()
			case *ast.UnaryExpr:
				if node.Op == token.ARROW {
					pos = node.End()
				}
			}
			if pos.IsValid() && (!barrier.IsValid() || pos < barrier) {
				barrier = pos
			}
			_, closure := n.(*ast.FuncLit)
			return !closure
		})
	}
	return removal != nil && (!barrier.IsValid() || removal.Pos() < barrier)
}

func cutPrefixInert(info *types.Info, expr ast.Expr) bool {
	if expr == nil {
		return true
	}
	if cutPrefixSame(info, expr, expr) {
		return true
	}
	switch expr := ast.Unparen(expr).(type) {
	case *ast.UnaryExpr:
		return expr.Op == token.NOT && cutPrefixInert(info, expr.X)
	case *ast.BinaryExpr:
		return expr.Op != token.QUO && expr.Op != token.REM && cutPrefixInert(info, expr.X) && cutPrefixInert(info, expr.Y)
	case *ast.CallExpr:
		return len(expr.Args) == 1 && calleeObject(info, expr) == types.Universe.Lookup("len") && cutPrefixInert(info, expr.Args[0])
	}
	return false
}

func cutPrefixLocalInit(info *types.Info, stmt ast.Stmt) bool {
	switch stmt := stmt.(type) {
	case *ast.DeclStmt:
		gen, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
			return false
		}
		value, ok := gen.Specs[0].(*ast.ValueSpec)
		return ok && len(value.Names) == 1 && len(value.Values) == 1 && value.Names[0].Name != "_" &&
			info.Defs[value.Names[0]] != nil && cutPrefixInert(info, value.Values[0])
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE || len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
			return false
		}
		id, ok := stmt.Lhs[0].(*ast.Ident)
		return ok && id.Name != "_" && info.Defs[id] != nil && cutPrefixInert(info, stmt.Rhs[0])
	default:
		return false
	}
}

func cutPrefixStringsCall(info *types.Info, expr ast.Expr, name string) *ast.CallExpr {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || call.Ellipsis.IsValid() {
		return nil
	}
	fn, ok := calleeObject(info, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "strings" || fn.Name() != name {
		return nil
	}
	return call
}
