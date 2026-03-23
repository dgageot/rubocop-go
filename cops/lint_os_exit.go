package cops

import (
	"go/ast"
	"go/token"

	"github.com/dgageot/rubocop-go/cop"
)

// LintOsExit detects os.Exit() calls outside the main function.
// Calling os.Exit bypasses deferred functions and makes code harder to test.
type LintOsExit struct{}

func init() { cop.Register(&LintOsExit{}) }

func (*LintOsExit) Name() string        { return "Lint/OsExit" }
func (*LintOsExit) Description() string { return "Avoid os.Exit outside of main()" }
func (*LintOsExit) Severity() cop.Severity { return cop.Warning }

func (c *LintOsExit) Check(fset *token.FileSet, file *ast.File) []cop.Offense {
	var offenses []cop.Offense

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// Allow os.Exit in main()
		if file.Name.Name == "main" && fn.Name.Name == "main" {
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
				offenses = append(offenses, cop.NewOffense(c, fset, call.Pos(), call.End(), "avoid os.Exit outside of main()"))
			}

			return true
		})
	}

	return offenses
}
