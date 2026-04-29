package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// LintOsExit detects os.Exit() calls outside the main function.
// Calling os.Exit bypasses deferred functions and makes code harder to test.
type LintOsExit struct {
	cop.Meta
}

func init() { cop.Register(NewLintOsExit()) }

// NewLintOsExit returns a fully configured LintOsExit cop.
func NewLintOsExit() *LintOsExit {
	return &LintOsExit{Meta: cop.Meta{
		CopName:     "Lint/OsExit",
		CopDesc:     "Avoid os.Exit outside of main()",
		CopSeverity: cop.Warning,
	}}
}

func (c *LintOsExit) Check(p *cop.Pass) {
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
}
