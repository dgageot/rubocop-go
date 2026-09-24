package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintSplitTrimJoin catches suffix-only trimming that needlessly materializes and
// rejoins a split string. Only adjacent, local patterns with a single-byte
// separator are matched: overlapping separators can split differently backwards.
func NewLintSplitTrimJoin(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newSplitTrimJoinFile(opts...))
}

func newSplitTrimJoinFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/SplitTrimJoin",
			Description: "Use strings.CutLast to remove trailing segments without splitting.",
			Severity:    cop.Warning,
		},
		MinStdlibVersion: "go1.27",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				if block, ok := n.(*ast.BlockStmt); ok {
					checkSplitTrimJoin(p, block.List)
				}
				return true
			})
		},
	}, opts...)
}

func checkSplitTrimJoin(p *cop.Pass, stmts []ast.Stmt) {
	for _, size := range []int{4, 3} {
		if len(stmts) < size {
			continue
		}
		tail := stmts[len(stmts)-size:]
		parts, value := splitTrimLocal(p.Info, tail[0])
		split := splitTrimStringsCall(p.Info, value, "Split")
		if parts == nil || split == nil {
			continue
		}
		sep := p.Info.Types[split.Args[1]].Value
		if sep == nil || sep.Kind() != constant.String || len(constant.StringVal(sep)) != 1 {
			continue
		}

		pattern := splitTrimPattern{info: p.Info, parts: parts}
		if size == 4 {
			var init ast.Expr
			pattern.end, init = splitTrimLocal(p.Info, tail[1])
			if pattern.end == nil || !pattern.isLength(init) {
				continue
			}
		}
		loop, ok := tail[size-2].(*ast.ForStmt)
		if !ok || !pattern.matchesLoop(loop) {
			continue
		}
		ret, ok := tail[size-1].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		join := splitTrimStringsCall(p.Info, ret.Results[0], "Join")
		if join == nil {
			continue
		}
		joinSep := p.Info.Types[join.Args[1]].Value
		if joinSep == nil || joinSep.Kind() != constant.String || constant.StringVal(joinSep) != constant.StringVal(sep) {
			continue
		}
		if pattern.end == nil {
			if !pattern.isObject(join.Args[0], parts) {
				continue
			}
		} else if !pattern.isPrefix(join.Args[0], pattern.isEnd) {
			continue
		}
		p.Report(split, "use strings.CutLast to trim trailing segments without Split/Join allocations; preserve the first segment and predicate evaluation order")
		return
	}
}

// splitTrimLocal requires a fresh variable so aliases cannot observe trimming.
func splitTrimLocal(info *types.Info, stmt ast.Stmt) (types.Object, ast.Expr) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, nil
	}
	id, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, nil
	}
	return info.Defs[id], assign.Rhs[0]
}

func splitTrimStringsCall(info *types.Info, expr ast.Expr, name string) *ast.CallExpr {
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

type splitTrimPattern struct {
	info  *types.Info
	parts types.Object
	end   types.Object // nil for the in-place reslicing form
}

func (p splitTrimPattern) isObject(expr ast.Expr, obj types.Object) bool {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	return ok && obj != nil && p.info.Uses[id] == obj
}

func (p splitTrimPattern) isLength(expr ast.Expr) bool {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	return ok && len(call.Args) == 1 && calleeObject(p.info, call) == types.Universe.Lookup("len") && p.isObject(call.Args[0], p.parts)
}

func (p splitTrimPattern) isEnd(expr ast.Expr) bool {
	if p.end == nil {
		return p.isLength(expr)
	}
	return p.isObject(expr, p.end)
}

func (p splitTrimPattern) isInteger(expr ast.Expr, want int64) bool {
	value := p.info.Types[expr].Value
	if value == nil || value.Kind() != constant.Int {
		return false
	}
	got, ok := constant.Int64Val(value)
	return ok && got == want
}

func (p splitTrimPattern) isLast(expr ast.Expr) bool {
	binary, ok := ast.Unparen(expr).(*ast.BinaryExpr)
	return ok && binary.Op == token.SUB && p.isEnd(binary.X) && p.isInteger(binary.Y, 1)
}

func (p splitTrimPattern) isPrefix(expr ast.Expr, high func(ast.Expr) bool) bool {
	slice, ok := ast.Unparen(expr).(*ast.SliceExpr)
	return ok && !slice.Slice3 && p.isObject(slice.X, p.parts) &&
		(slice.Low == nil || p.isInteger(slice.Low, 0)) && high(slice.High)
}

func (p splitTrimPattern) matchesLoop(loop *ast.ForStmt) bool {
	if loop.Init != nil || loop.Body == nil {
		return false
	}
	cond, ok := ast.Unparen(loop.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.LAND {
		return false
	}
	guard, ok := ast.Unparen(cond.X).(*ast.BinaryExpr)
	if !ok || !p.isEnd(guard.X) {
		return false
	}
	bounded := guard.Op == token.GTR && p.isInteger(guard.Y, 1) || guard.Op == token.GEQ && p.isInteger(guard.Y, 2)
	if !bounded || !p.suffixPredicate(cond.Y) {
		return false
	}
	var step ast.Stmt
	switch {
	case loop.Post == nil && len(loop.Body.List) == 1:
		step = loop.Body.List[0]
	case loop.Post != nil && len(loop.Body.List) == 0:
		step = loop.Post
	default:
		return false
	}
	if p.end != nil {
		dec, ok := step.(*ast.IncDecStmt)
		return ok && dec.Tok == token.DEC && p.isObject(dec.X, p.end)
	}
	assign, ok := step.(*ast.AssignStmt)
	return ok && assign.Tok == token.ASSIGN && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 &&
		p.isObject(assign.Lhs[0], p.parts) && p.isPrefix(assign.Rhs[0], p.isLast)
}

func (p splitTrimPattern) suffixPredicate(expr ast.Expr) bool {
	valid, found := true, false
	ast.Inspect(expr, func(n ast.Node) bool {
		if !valid {
			return false
		}
		switch node := n.(type) {
		case *ast.IndexExpr:
			if p.isObject(node.X, p.parts) && p.isLast(node.Index) {
				found = true
				return false
			}
		case *ast.Ident:
			obj := p.info.Uses[node]
			if obj == p.parts || p.end != nil && obj == p.end {
				valid = false
			}
		case *ast.UnaryExpr:
			// A predicate must not mutate or retain a pointer into the slice.
			if node.Op == token.AND {
				valid = false
			}
		case *ast.FuncLit:
			valid = false
		}
		return valid
	})
	return valid && found
}
