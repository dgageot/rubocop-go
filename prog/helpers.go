package prog

import (
	"fmt"
	"sort"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// sprintf applies fmt.Sprintf only when args are present, so a literal
// message containing '%' is reported verbatim.
func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// allFunctions is a thin wrapper over ssautil.AllFunctions so callers do
// not import ssautil directly.
func allFunctions(prog *ssa.Program) map[*ssa.Function]bool {
	return ssautil.AllFunctions(prog)
}

// sortFunctions orders functions by position then name for deterministic
// iteration. Functions without a position (synthetic) sort last by name.
func sortFunctions(fns []*ssa.Function) {
	sort.Slice(fns, func(i, j int) bool {
		pi, pj := fns[i].Pos(), fns[j].Pos()
		if pi != pj {
			return pi < pj
		}
		return fns[i].String() < fns[j].String()
	})
}
