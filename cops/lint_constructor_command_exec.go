package cops

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintConstructorCommandExec keeps command setup and execution out of constructors.
// Execution methods require resolved os/exec types; lazy closures are excluded.
func NewLintConstructorCommandExec(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newConstructorCommandExecFile(opts...))
}

func newConstructorCommandExecFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ConstructorCommandExec",
			Description: "Avoid preparing or executing external commands in constructors.",
			Severity:    cop.Error,
		},
		Types: true,
		Run: func(p *cop.Pass) {
			p.ForEachFunc(func(fn *ast.FuncDecl) {
				if !isConstructor(fn) || fn.Body == nil {
					return
				}
				forEachTypedConstructionCall(p.Info, fn.Body, func(call *ast.CallExpr) {
					if name, ok := constructorCommandCall(p.Info, call); ok {
						p.Reportf(call, "constructor %s calls %s; move command setup/execution behind an explicit method so process side effects are deliberate", fn.Name.Name, name)
					}
				})
			})
		},
	}, opts...)
}

func constructorCommandCall(info *types.Info, call *ast.CallExpr) (string, bool) {
	if info == nil {
		name, ok := cop.CallTo(call, "exec", "Command", "CommandContext")
		return "os/exec." + name, ok
	}
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "os/exec" {
		return "", false
	}
	if fn.Signature().Recv() == nil {
		return "os/exec." + fn.Name(), fn.Name() == "Command" || fn.Name() == "CommandContext"
	}
	switch fn.Name() {
	case "Start", "Run", "Output", "CombinedOutput":
		return "os/exec.Cmd." + fn.Name(), true
	default:
		return "", false
	}
}
