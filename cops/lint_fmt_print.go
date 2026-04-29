package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// LintFmtPrint detects fmt.Print/Println/Printf calls in non-main packages.
// These are usually debugging leftovers that should be replaced with proper logging.
type LintFmtPrint struct {
	cop.Meta
}

func init() { cop.Register(NewLintFmtPrint()) }

// NewLintFmtPrint returns a fully configured LintFmtPrint cop.
func NewLintFmtPrint() *LintFmtPrint {
	return &LintFmtPrint{Meta: cop.Meta{
		CopName:     "Lint/FmtPrint",
		CopDesc:     "Avoid fmt.Print* in library code (use a logger)",
		CopSeverity: cop.Warning,
	}}
}

var fmtPrintFuncs = map[string]bool{
	"Print":   true,
	"Println": true,
	"Printf":  true,
}

func (c *LintFmtPrint) Check(p *cop.Pass) {
	// Allow fmt.Print in main packages — that's often intentional CLI output.
	if p.IsMain() {
		return
	}

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
			p.Report(call, "fmt.%s in library code — use a logger instead", sel.Sel.Name)
		}

		return true
	})
}
