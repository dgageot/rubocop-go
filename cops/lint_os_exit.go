package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintOsExit flags os.Exit outside the direct body of main.
func NewLintOsExit() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Lint/OsExit",
		Description: "Avoid os.Exit outside of main()",
		Severity:    cop.Warning,
	}, func(p *cop.Pass) {
		for _, decl := range p.File.Decls {
			mainBody := false
			if fn, ok := decl.(*ast.FuncDecl); ok {
				mainBody = p.IsMain() && fn.Recv == nil && fn.Name.Name == "main"
			}
			var visit func(ast.Node, bool)
			visit = func(root ast.Node, allowed bool) {
				ast.Inspect(root, func(n ast.Node) bool {
					if lit, ok := n.(*ast.FuncLit); ok {
						visit(lit.Body, false)
						return false
					}
					if call, ok := n.(*ast.CallExpr); ok && !allowed {
						if _, match := standardLibraryCall(p, call, "os", "Exit"); match {
							p.Report(call, "avoid os.Exit outside of main()")
						}
					}
					return true
				})
			}
			visit(decl, mainBody)
		}
	}, cop.WithTypes())
}
