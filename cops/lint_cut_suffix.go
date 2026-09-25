package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintCutSuffix reports paired suffix checks and removals.
func NewLintCutSuffix(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newCutSuffixFile(opts...))
}

func newCutSuffixFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/CutSuffix",
			Description: "Use strings.CutSuffix for paired suffix checks and removals.",
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
					if node.Init == nil && len(node.Body.List) > 0 {
						checkCutSuffix(p, node.Cond, node.Body.List[0])
					}
				case *ast.BlockStmt:
					checkCutSuffixGuards(p, node.List)
				case *ast.CaseClause:
					checkCutSuffixGuards(p, node.Body)
				case *ast.CommClause:
					checkCutSuffixGuards(p, node.Body)
				}
				return true
			})
		},
	}, opts...)
}

func checkCutSuffixGuards(p *cop.Pass, stmts []ast.Stmt) {
	for i, stmt := range stmts {
		guard, ok := stmt.(*ast.IfStmt)
		if !ok || guard.Init != nil || guard.Else != nil || len(guard.Body.List) != 1 || i+1 == len(stmts) {
			continue
		}
		negated, ok := ast.Unparen(guard.Cond).(*ast.UnaryExpr)
		if !ok || negated.Op != token.NOT {
			continue
		}
		switch exit := guard.Body.List[0].(type) {
		case *ast.ReturnStmt:
		case *ast.BranchStmt:
			if exit.Tok != token.CONTINUE || exit.Label != nil {
				continue
			}
		default:
			continue
		}
		checkCutSuffix(p, negated.X, stmts[i+1])
	}
}

func checkCutSuffix(p *cop.Pass, cond ast.Expr, stmt ast.Stmt) {
	call := splitTrimStringsCall(p.Info, cond, "HasSuffix")
	if call == nil || !cutSuffixStable(p.Info, call.Args[0]) || !cutSuffixStable(p.Info, call.Args[1]) {
		return
	}
	pattern := cutSuffixPattern{info: p.Info, source: call.Args[0], suffix: call.Args[1]}
	if pattern.removal(cutSuffixValue(p.Info, stmt)) {
		p.Report(call, "use strings.CutSuffix instead of HasSuffix followed by suffix removal; preserve original values, assignment scope, and evaluation order")
	}
}

// Restrict consumption to the first evaluation, with no effectful assignment target.
func cutSuffixValue(info *types.Info, stmt ast.Stmt) ast.Expr {
	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		if (stmt.Tok == token.DEFINE || stmt.Tok == token.ASSIGN) && len(stmt.Lhs) == 1 && len(stmt.Rhs) == 1 {
			if _, ok := stmt.Lhs[0].(*ast.Ident); ok {
				return stmt.Rhs[0]
			}
		}
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if ok && decl.Tok == token.VAR && len(decl.Specs) == 1 {
			value, ok := decl.Specs[0].(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && len(value.Values) == 1 {
				return value.Values[0]
			}
		}
	case *ast.ReturnStmt:
		if len(stmt.Results) == 0 {
			return nil
		}
		for _, result := range stmt.Results[1:] {
			if value := info.Types[result]; value.Value == nil && !value.IsNil() {
				return nil
			}
		}
		return stmt.Results[0]
	}
	return nil
}

func cutSuffixStable(info *types.Info, expr ast.Expr) bool {
	if info.Types[expr].Value != nil {
		return true
	}
	id, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return false
	}
	variable, ok := info.Uses[id].(*types.Var)
	return ok && variable.Parent() != nil && variable.Pkg() != nil && variable.Parent() != variable.Pkg().Scope()
}

type cutSuffixPattern struct {
	info           *types.Info
	source, suffix ast.Expr
}

func (p cutSuffixPattern) same(a, b ast.Expr) bool {
	if a == nil || b == nil {
		return false
	}
	av, bv := p.info.Types[a].Value, p.info.Types[b].Value
	if av != nil && bv != nil && av.Kind() == bv.Kind() {
		return constant.Compare(av, token.EQL, bv)
	}
	aid, aok := ast.Unparen(a).(*ast.Ident)
	bid, bok := ast.Unparen(b).(*ast.Ident)
	return aok && bok && p.info.Uses[aid] != nil && p.info.Uses[aid] == p.info.Uses[bid]
}

func (p cutSuffixPattern) length(expr, value ast.Expr) bool {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	return ok && len(call.Args) == 1 && calleeObject(p.info, call) == types.Universe.Lookup("len") && p.same(call.Args[0], value)
}

func (p cutSuffixPattern) integer(expr ast.Expr, want int64) bool {
	value := p.info.Types[expr].Value
	if value == nil || value.Kind() != constant.Int {
		return false
	}
	got, ok := constant.Int64Val(value)
	return ok && got == want
}

func (p cutSuffixPattern) removal(expr ast.Expr) bool {
	if trim := splitTrimStringsCall(p.info, expr, "TrimSuffix"); trim != nil {
		return p.same(trim.Args[0], p.source) && p.same(trim.Args[1], p.suffix)
	}
	if slice, ok := ast.Unparen(expr).(*ast.SliceExpr); ok {
		if slice.Slice3 || !types.Identical(p.info.TypeOf(slice), types.Typ[types.String]) || !p.same(slice.X, p.source) || slice.Low != nil && !p.integer(slice.Low, 0) {
			return false
		}
		high, ok := ast.Unparen(slice.High).(*ast.BinaryExpr)
		if !ok || high.Op != token.SUB || !p.length(high.X, p.source) {
			return false
		}
		if p.length(high.Y, p.suffix) {
			return true
		}
		suffix := p.info.Types[p.suffix].Value
		return suffix != nil && suffix.Kind() == constant.String && p.integer(high.Y, int64(len(constant.StringVal(suffix))))
	}
	// Keep wrappers (e.g. TrimRight) in place; only their first argument is cut.
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 || call.Ellipsis.IsValid() {
		return false
	}
	fn, ok := calleeObject(p.info, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "strings" || fn.Type().(*types.Signature).Recv() != nil {
		return false
	}
	for _, arg := range call.Args[1:] {
		if p.info.Types[arg].Value == nil {
			return false
		}
	}
	return p.removal(call.Args[0])
}
