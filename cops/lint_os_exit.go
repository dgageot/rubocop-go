package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintOsExit returns a cop that flags os.Exit() calls outside the main
// function. Calling os.Exit bypasses deferred functions and makes code
// harder to test.
func NewLintOsExit() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Lint/OsExit",
		Description: "Avoid os.Exit outside of main()",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		p.ForEachFunc(func(fn *ast.FuncDecl) {
			// Allow os.Exit in main()
			if p.IsMain() && fn.Name.Name == "main" {
				return
			}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if cop.IsCallTo(call, "os", "Exit") {
					p.Report(call, "avoid os.Exit outside of main()")
				}
				return true
			})
		})
	})
}
