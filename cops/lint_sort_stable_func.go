package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintSortStableFunc reports stable sorts with pure integer or string key comparisons.
func NewLintSortStableFunc(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newSortStableFuncFile(opts...))
}

func newSortStableFuncFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/SortStableFunc",
			Description: "Use slices.SortStableFunc for simple integer and string comparisons.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.21",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || p.IsTestFile() || ast.IsGenerated(p.File) {
				return
			}
			p.ForEachCall(func(call *ast.CallExpr) {
				fn, ok := typeutil.Callee(p.Info, call).(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "sort" || fn.Name() != "SliceStable" || len(call.Args) != 2 {
					return
				}
				slice, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
				if !ok {
					return
				}
				sliceType := p.Info.TypeOf(slice)
				if sliceType == nil {
					return
				}
				if _, ok := sliceType.Underlying().(*types.Slice); !ok {
					return
				}
				less, ok := ast.Unparen(call.Args[1]).(*ast.FuncLit)
				if !ok {
					return
				}
				sig, ok := p.Info.TypeOf(less).(*types.Signature)
				if !ok || sig.Params().Len() != 2 {
					return
				}
				match := stableSortMatcher{info: p.Info, slice: p.Info.ObjectOf(slice), left: sig.Params().At(0), right: sig.Params().At(1)}
				if match.body(less.Body.List) {
					p.Report(call, "use slices.SortStableFunc with cmp.Compare instead of sort.SliceStable; reverse cmp.Compare arguments for descending keys and preserve tie-break order")
				}
			})
		},
	}, opts...)
}

type stableSortMatcher struct {
	info        *types.Info
	slice       types.Object
	left, right *types.Var
}

func (m stableSortMatcher) body(stmts []ast.Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	for _, stmt := range stmts[:len(stmts)-1] {
		branch, ok := stmt.(*ast.IfStmt)
		if !ok || branch.Init != nil || branch.Else != nil || len(branch.Body.List) != 1 {
			return false
		}
		guard, ok := ast.Unparen(branch.Cond).(*ast.BinaryExpr)
		if !ok || guard.Op != token.NEQ {
			return false
		}
		key, ok := m.comparisonKey(guard)
		if !ok {
			return false
		}
		returnKey, ok := m.returnKey(branch.Body.List[0])
		if !ok || returnKey != key {
			return false
		}
	}
	_, ok := m.returnKey(stmts[len(stmts)-1])
	return ok
}

func (m stableSortMatcher) returnKey(stmt ast.Stmt) (string, bool) {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}
	comparison, ok := ast.Unparen(ret.Results[0]).(*ast.BinaryExpr)
	if !ok || (comparison.Op != token.LSS && comparison.Op != token.GTR) {
		return "", false
	}
	return m.comparisonKey(comparison)
}

func (m stableSortMatcher) comparisonKey(expr *ast.BinaryExpr) (string, bool) {
	leftType, rightType := m.info.TypeOf(expr.X), m.info.TypeOf(expr.Y)
	if leftType == nil || rightType == nil || !types.Identical(leftType, rightType) {
		return "", false
	}
	basic, ok := leftType.Underlying().(*types.Basic)
	// cmp.Compare orders NaNs differently from < and >.
	if !ok || basic.Info()&(types.IsInteger|types.IsString) == 0 {
		return "", false
	}
	leftKey, leftOK := m.key(expr.X, m.left)
	rightKey, rightOK := m.key(expr.Y, m.right)
	return leftKey, leftOK && rightOK && leftKey == rightKey
}

func (m stableSortMatcher) key(expr ast.Expr, index *types.Var) (string, bool) {
	switch expr := ast.Unparen(expr).(type) {
	case *ast.SelectorExpr:
		selection := m.info.Selections[expr]
		if selection == nil || selection.Kind() != types.FieldVal {
			return "", false
		}
		key, ok := m.key(expr.X, index)
		return key + "." + expr.Sel.Name, ok
	case *ast.IndexExpr:
		slice, ok := ast.Unparen(expr.X).(*ast.Ident)
		if !ok || m.info.ObjectOf(slice) != m.slice {
			return "", false
		}
		id, ok := ast.Unparen(expr.Index).(*ast.Ident)
		return "", ok && m.info.ObjectOf(id) == index
	default:
		return "", false
	}
}
