package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// StyleEmptyFunc detects empty function bodies.
// An empty function body might indicate unfinished code or a missing implementation.
type StyleEmptyFunc struct{}

func init() { cop.Register(&StyleEmptyFunc{}) }

func (*StyleEmptyFunc) Name() string           { return "Style/EmptyFunc" }
func (*StyleEmptyFunc) Description() string    { return "Avoid empty function bodies" }
func (*StyleEmptyFunc) Severity() cop.Severity { return cop.Convention }

func (c *StyleEmptyFunc) Check(p *cop.Pass) []cop.Offense {
	var offenses []cop.Offense

	ast.Inspect(p.File, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}

		// Skip functions with no body (e.g. external declarations).
		if fn.Body == nil {
			return true
		}

		// Skip functions that only return (interface stubs).
		if len(fn.Body.List) == 0 {
			offenses = append(offenses, cop.NewOffenseAt(c, p.FileSet, fn.Pos(), fn.Name.End(),
				"function '"+fn.Name.Name+"' has an empty body"))
		}

		return true
	})

	return offenses
}
