package cops

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

func init() { cop.Register(NewStyleErrorNaming()) }

// NewStyleErrorNaming returns a cop that enforces the err/Err naming
// convention for error variables. In Go, error variables returned from
// function calls should be named "err" or start with "err" (e.g.
// errNotFound), not arbitrary names like "e" or "error".
func NewStyleErrorNaming() *cop.Func {
	return cop.New(cop.Meta{
		Name:        "Style/ErrorNaming",
		Description: "Error variables should be named err or start with err",
		Severity:    cop.Convention,
	}, func(p *cop.Pass) {
		p.ForEachAssign(func(assign *ast.AssignStmt) {
			// Only short variable declarations (:=) returning at least two values
			// from a function call.
			if assign.Tok != token.DEFINE || len(assign.Lhs) < 2 || len(assign.Rhs) != 1 {
				return
			}
			if _, isCall := assign.Rhs[0].(*ast.CallExpr); !isCall {
				return
			}

			lastLhs := assign.Lhs[len(assign.Lhs)-1]
			ident, ok := lastLhs.(*ast.Ident)
			if !ok || ident.Name == "_" {
				return
			}

			// The last LHS variable should be "err" or start with "err".
			if !strings.HasPrefix(strings.ToLower(ident.Name), "err") {
				p.Report(ident, "error variable '%s' should be named 'err' or start with 'err'", ident.Name)
			}
		})
	})
}
