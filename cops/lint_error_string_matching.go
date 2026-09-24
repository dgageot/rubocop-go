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
			if p.IsTestFile() {
				return
			}

			p.ForEachCall(func(call *ast.CallExpr) {
				if !cop.IsCallTo(call, "strings", "Contains", "HasPrefix", "HasSuffix") || len(call.Args) == 0 {
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
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" {
		return nil
	}

	errorType, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	receiverType := p.Info.TypeOf(sel.X)
	if receiverType == nil || !types.Implements(receiverType, errorType) {
		return nil
	}
	return sel.X
}
