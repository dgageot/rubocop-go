package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintContextFirstParameter requires context.Context to lead parameter lists.
// It checks function and method declarations, not function literals or types.
// Matching is syntactic; import and type aliases are not resolved. Tests are skipped.
func NewLintContextFirstParameter(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ContextFirstParameter",
			Description: "Put context.Context first in function parameter lists.",
			Severity:    cop.Convention,
		},
		Run: func(p *cop.Pass) {
			if p.IsTestFile() {
				return
			}
			p.ForEachFunc(func(fn *ast.FuncDecl) {
				if fn.Type.Params == nil {
					return
				}
				for i, field := range fn.Type.Params.List {
					if !cop.IsContextType(field.Type) {
						continue
					}
					if i > 0 {
						p.Report(field, "move context.Context to the first parameter position")
					}
					return
				}
			})
		},
	}, opts...)
}
