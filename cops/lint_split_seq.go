package cops

import (
	"go/ast"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintSplitSeq suggests lazy splitting for direct value-only string ranges.
// Mutable byte slices and ranges that need indices are excluded.
func NewLintSplitSeq(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newSplitSeqFile(opts...))
}

func newSplitSeqFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/SplitSeq",
			Description: "Use strings.SplitSeq or SplitAfterSeq when split results are only iterated once.",
			Severity:    cop.Convention,
		},
		MinGoVersion:     "go1.23",
		MinStdlibVersion: "go1.24",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				loop, ok := n.(*ast.RangeStmt)
				if !ok {
					return true
				}
				call, ok := ast.Unparen(loop.X).(*ast.CallExpr)
				if !ok || len(call.Args) != 2 || call.Ellipsis.IsValid() {
					return true
				}
				fn, ok := p.CalleeObject(call).(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "strings" || (fn.Name() != "Split" && fn.Name() != "SplitAfter") {
					return true
				}
				if !fieldsValueRange(p.Info, loop) {
					return true
				}
				p.Reportf(call, "use strings.%sSeq to avoid allocating a slice used only for iteration; preserve input and separator evaluation", fn.Name())
				return true
			})
		},
	}, opts...)
}
