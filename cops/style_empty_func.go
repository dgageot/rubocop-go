package cops

import (
	"go/ast"

	"github.com/dgageot/rubocop-go/cop"
)

// StyleEmptyFunc detects empty function bodies.
// An empty function body might indicate unfinished code or a missing implementation.
type StyleEmptyFunc struct {
	cop.Meta
}

func init() { cop.Register(NewStyleEmptyFunc()) }

// NewStyleEmptyFunc returns a fully configured StyleEmptyFunc cop.
func NewStyleEmptyFunc() *StyleEmptyFunc {
	return &StyleEmptyFunc{Meta: cop.Meta{
		CopName:     "Style/EmptyFunc",
		CopDesc:     "Avoid empty function bodies",
		CopSeverity: cop.Convention,
	}}
}

func (c *StyleEmptyFunc) Check(p *cop.Pass) {
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
			p.ReportAt(fn.Pos(), fn.Name.End(),
				"function '%s' has an empty body", fn.Name.Name)
		}

		return true
	})
}
