package cops

import (
	"go/ast"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintPointerHelper covers AWS forwarding wrappers missed by modernize's new-like facts.
func NewLintPointerHelper(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newPointerHelperFile(opts...))
}

func newPointerHelperFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/PointerHelper",
			Description: "Replace AWS scalar pointer helpers with new expressions.",
			Severity:    cop.Warning,
		},
		MinGoVersion: "go1.26",
		Types:        true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			p.ForEachCall(func(call *ast.CallExpr) {
				fn, ok := calleeObject(p.Info, call).(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "github.com/aws/aws-sdk-go-v2/aws" || len(call.Args) != 1 || call.Ellipsis.IsValid() {
					return
				}
				var conversion string
				switch fn.Name() {
				case "Bool":
					conversion = "bool"
				case "Byte":
					conversion = "byte"
				case "String":
					conversion = "string"
				case "Int":
					conversion = "int"
				case "Int8":
					conversion = "int8"
				case "Int16":
					conversion = "int16"
				case "Int32":
					conversion = "int32"
				case "Int64":
					conversion = "int64"
				case "Uint":
					conversion = "uint"
				case "Uint8":
					conversion = "uint8"
				case "Uint16":
					conversion = "uint16"
				case "Uint32":
					conversion = "uint32"
				case "Uint64":
					conversion = "uint64"
				case "Float32":
					conversion = "float32"
				case "Float64":
					conversion = "float64"
				default:
					return
				}
				scope := p.Package.Scope().Innermost(call.Pos())
				if scope == nil {
					return
				}
				for _, name := range []string{"new", conversion} {
					if _, obj := scope.LookupParent(name, call.Pos()); obj != types.Universe.Lookup(name) {
						return
					}
				}
				p.Reportf(call, "use new(%s(value)) instead of aws.%s; preserve the argument's evaluation and conversion (omit the conversion only when the type already matches)", conversion, fn.Name())
			})
		},
	}, opts...)
}
