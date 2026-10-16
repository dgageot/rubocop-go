package cops

import (
	"go/ast"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintErrorStringMatching prevents control flow from depending on error text.
// Sentinel and typed errors keep checks stable when messages are reworded and
// preserve wrapped-error handling through errors.Is and errors.As.
func NewLintErrorStringMatching(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ErrorStringMatching",
			Description: "Prefer structured error checks over matching error text.",
			Severity:    cop.Error,
		},
		Types: true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || p.IsTestFile() {
				return
			}

			p.ForEachCall(func(call *ast.CallExpr) {
				fn, ok := p.CalleeObject(call).(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "strings" || len(call.Args) != 2 {
					return
				}
				if fn.Name() != "Contains" && fn.Name() != "HasPrefix" && fn.Name() != "HasSuffix" {
					return
				}
				if errorStringReceiver(p, call.Args[0]) == nil {
					return
				}
				p.Report(call, "do not match err.Error() text; expose a sentinel or typed error and use errors.Is/errors.As")
			})
		},
	}, opts...)
}

func errorStringReceiver(p *cop.Pass, expr ast.Expr) ast.Expr {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" {
		return nil
	}

	selection := p.Info.Selections[sel]
	if selection == nil || selection.Kind() != types.MethodVal {
		return nil
	}
	// The selected method accounts for implicit address-taking and promotion.
	sig, ok := selection.Obj().Type().(*types.Signature)
	if !ok || sig.Variadic() || sig.Params().Len() != 0 || sig.Results().Len() != 1 ||
		!types.Identical(sig.Results().At(0).Type(), types.Typ[types.String]) {
		return nil
	}
	return sel.X
}
