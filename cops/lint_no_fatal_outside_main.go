package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintNoFatalOutsideMain reserves log.Fatal, Fatalf, and Fatalln for package main.
// All functions in package main and all test files are exempt. Matching is
// syntactic; import aliases and logger instance methods are not resolved.
func NewLintNoFatalOutsideMain(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/NoFatalOutsideMain",
			Description: "Reserve log.Fatal calls for package main.",
			Severity:    cop.Error,
		},
		Run: func(p *cop.Pass) {
			if p.IsMain() || p.IsTestFile() {
				return
			}
			p.ForEachCall(func(call *ast.CallExpr) {
				if cop.IsCallTo(call, "log", "Fatal", "Fatalf", "Fatalln") {
					p.Report(call, "return an error instead of terminating the process outside package main")
				}
			})
		},
	}, opts...)
}
