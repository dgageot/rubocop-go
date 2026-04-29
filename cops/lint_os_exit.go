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
	for _, decl := range p.File.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// Allow os.Exit in main()
		if p.IsMain() && fn.Name.Name == "main" {
			continue
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			if ident.Name == "os" && sel.Sel.Name == "Exit" {
				p.Report(call, "avoid os.Exit outside of main()")
			}

			return true
		})
	}
}
