package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

func init() { cop.Register(NewStyleEmptyFunc()) }

// NewStyleEmptyFunc returns a cop that flags empty function bodies. An
// empty function body might indicate unfinished code or a missing
// implementation.
func NewStyleEmptyFunc() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Style/EmptyFunc",
		Description: "Avoid empty function bodies",
		Severity:    cop.Convention,
	}, func(p *cop.Pass) {
		p.ForEachFunc(func(fn *ast.FuncDecl) {
			// Skip functions with no body (e.g. external declarations) and
			// non-empty bodies.
			if fn.Body == nil || len(fn.Body.List) > 0 {
				return
			}
			p.ReportAt(fn.Pos(), fn.Name.End(),
				"function '%s' has an empty body", fn.Name.Name)
		})
	})
}
