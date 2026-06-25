// Package cops contains all built-in cops.
//
// Use [All] to get the full set of built-in cops as a slice. The bundled CLI
// in main.go does this; embedders that want a subset can either pick named
// constructors directly (see examples/embed) or filter the slice.
package cops

import (
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// All returns a fresh slice containing every built-in cop. The returned
// slice is independent of the package state, so callers may freely append
// or filter it.
func All() []cop.Cop {
	return []cop.Cop{
		NewLintCloneCompleteness(),
		NewLintFmtPrint(),
		NewLintOsExit(),
		NewStyleEmptyFunc(),
		NewStyleErrorNaming(),
	}
}

// AllProgram returns a fresh slice containing every built-in whole-program
// cop. These run once over the entire loaded program (via go/packages +
// go/ssa) rather than once per file, so they can answer inter-procedural,
// cross-package questions. Pass them to the runner with
// runner.Runner.WithProgramCops.
func AllProgram() []prog.Cop {
	return []prog.Cop{
		NewLintContextConnectivity(),
	}
}
