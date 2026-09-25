package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintNoContextField flags context.Context fields in top-level named structs.
// Matching is syntactic; import and type aliases are not resolved. Tests are skipped.
func NewLintNoContextField(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/NoContextField",
			Description: "Pass contexts to operations instead of storing them in structs.",
			Severity:    cop.Warning,
		},
		Run: func(p *cop.Pass) {
			if p.IsTestFile() {
				return
			}
			p.ForEachStruct(func(_ *ast.TypeSpec, structType *ast.StructType) {
				for _, field := range structType.Fields.List {
					if cop.IsContextType(field.Type) {
						p.Report(field, "do not store context.Context in a struct; pass it to each operation")
					}
				}
			})
		},
	}, opts...)
}
