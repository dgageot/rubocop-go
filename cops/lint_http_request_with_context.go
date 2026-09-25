package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintHTTPRequestWithContext prefers http.NewRequestWithContext over http.NewRequest.
// It does not track contexts attached later or prove caller-context propagation.
// Matching is syntactic; import aliases are not resolved. Tests are skipped.
func NewLintHTTPRequestWithContext(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/HTTPRequestWithContext",
			Description: "Use http.NewRequestWithContext to construct HTTP requests.",
			Severity:    cop.Error,
		},
		Run: func(p *cop.Pass) {
			if p.IsTestFile() {
				return
			}
			p.ForEachCall(func(call *ast.CallExpr) {
				if cop.IsCallTo(call, "http", "NewRequest") {
					p.Report(call, "use http.NewRequestWithContext with the caller's context")
				}
			})
		},
	}, opts...)
}
