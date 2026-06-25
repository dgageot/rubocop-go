package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintFmtPrint returns a cop that flags fmt.Print/Println/Printf calls
// in non-main packages. These are usually debugging leftovers that should
// be replaced with proper logging.
func NewLintFmtPrint() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Lint/FmtPrint",
		Description: "Avoid fmt.Print* in library code (use a logger)",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			if cop.IsCallTo(call, "fmt", "Print", "Println", "Printf") {
				sel := call.Fun.(*ast.SelectorExpr)
				p.Reportf(call, "fmt.%s in library code — use a logger instead", sel.Sel.Name)
			}
		})
	}, cop.WithScope(func(p *cop.Pass) bool { return !p.IsMain() }))
}
