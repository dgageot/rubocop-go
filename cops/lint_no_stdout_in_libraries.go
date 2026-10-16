package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintNoStdoutInLibraries reserves direct fmt output to stdout for main packages.
func NewLintNoStdoutInLibraries(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/NoStdoutInLibraries",
			Description: "Use caller-provided writers instead of stdout in library packages.",
			Severity:    cop.Error,
		},
		Run: func(p *cop.Pass) {
			if p.IsTestFile() || p.IsMain() {
				return
			}

			p.ForEachCall(func(call *ast.CallExpr) {
				if !writesDirectlyToStdout(call) {
					return
				}
				p.Report(call, "library code must write to a caller-provided io.Writer instead of stdout")
			})
		},
	}, opts...)
}

func writesDirectlyToStdout(call *ast.CallExpr) bool {
	if _, ok := cop.CallTo(call, "fmt", "Print", "Printf", "Println"); ok {
		return true
	}
	if _, ok := cop.CallTo(call, "fmt", "Fprint", "Fprintf", "Fprintln"); !ok || len(call.Args) == 0 {
		return false
	}
	return cop.IsSelector(ast.Unparen(call.Args[0]), "os", "Stdout")
}
