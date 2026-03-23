package cops

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

// StyleErrorNaming checks that error variables follow the err/Err naming convention.
// In Go, error variables returned from function calls should be named "err" or
// start with "err" (e.g. errNotFound), not arbitrary names like "e" or "error".
type StyleErrorNaming struct{}

func init() { cop.Register(&StyleErrorNaming{}) }

func (*StyleErrorNaming) Name() string           { return "Style/ErrorNaming" }
func (*StyleErrorNaming) Description() string    { return "Error variables should be named err or start with err" }
func (*StyleErrorNaming) Severity() cop.Severity { return cop.Convention }

func (c *StyleErrorNaming) Check(fset *token.FileSet, file *ast.File) []cop.Offense {
	var offenses []cop.Offense

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		// Only check short variable declarations (:=)
		if assign.Tok != token.DEFINE {
			return true
		}

		// Check if the last return value looks like an error.
		// Convention: if a function returns (T, error), the error is the last value.
		if len(assign.Lhs) < 2 {
			return true
		}

		lastLhs := assign.Lhs[len(assign.Lhs)-1]
		ident, ok := lastLhs.(*ast.Ident)
		if !ok || ident.Name == "_" {
			return true
		}

		// Check if the last RHS is a function call.
		if len(assign.Rhs) != 1 {
			return true
		}
		_, isCall := assign.Rhs[0].(*ast.CallExpr)
		if !isCall {
			return true
		}

		// The last LHS variable should be "err" or start with "err".
		if !strings.HasPrefix(strings.ToLower(ident.Name), "err") {
			offenses = append(offenses, cop.NewOffense(c, fset, ident.Pos(), ident.End(),
				"error variable '"+ident.Name+"' should be named 'err' or start with 'err'"))
		}

		return true
	})

	return offenses
}
