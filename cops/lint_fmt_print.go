package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// LintFmtPrint detects fmt.Print/Println/Printf calls in non-main packages.
// These are usually debugging leftovers that should be replaced with proper logging.
type LintFmtPrint struct{}

func init() { cop.Register(&LintFmtPrint{}) }

func (*LintFmtPrint) Name() string           { return "Lint/FmtPrint" }
func (*LintFmtPrint) Description() string    { return "Avoid fmt.Print* in library code (use a logger)" }
func (*LintFmtPrint) Severity() cop.Severity { return cop.Warning }

var fmtPrintFuncs = map[string]bool{
	"Print":   true,
	"Println": true,
	"Printf":  true,
}

func (c *LintFmtPrint) Check(p *cop.Pass) []cop.Offense {
	// Allow fmt.Print in main packages — that's often intentional CLI output.
	if p.IsMain() {
		return nil
	}

	var offenses []cop.Offense

	ast.Inspect(p.File, func(n ast.Node) bool {
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

		if ident.Name == "fmt" && fmtPrintFuncs[sel.Sel.Name] {
			offenses = append(offenses, cop.NewOffense(c, p.FileSet, call,
				"fmt."+sel.Sel.Name+" in library code — use a logger instead"))
		}

		return true
	})

	return offenses
}
