package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

func init() { cop.Register(NewLintFmtPrint()) }

// NewLintFmtPrint returns a cop that flags fmt.Print/Println/Printf calls
// in non-main packages. These are usually debugging leftovers that should
// be replaced with proper logging.
func NewLintFmtPrint() *cop.Func {
	return &cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/FmtPrint",
			Description: "Avoid fmt.Print* in library code (use a logger)",
			Severity:    cop.Warning,
		},
		// Allow fmt.Print in main packages — that's often intentional CLI output.
		Scope: func(p *cop.Pass) bool { return !p.IsMain() },
		Run: func(p *cop.Pass) {
			p.ForEachCall(func(call *ast.CallExpr) {
				if cop.IsCallTo(call, "fmt", "Print", "Println", "Printf") {
					sel := call.Fun.(*ast.SelectorExpr)
					p.Report(call, "fmt.%s in library code — use a logger instead", sel.Sel.Name)
				}
			})
		},
	}
}
