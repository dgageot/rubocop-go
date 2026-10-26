// Package cops contains all built-in cops.
//
// [Catalog] describes every shipped cop by name. The bundled CLI in main.go
// selects cops from it with .rubocop-go.yml. [All] and [AllProgram] return the
// catalog's default cops. Embedders that want a subset can either pick named
// constructors directly (see examples/embed) or filter these slices.
package cops

import (
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// Entry describes one shipped cop that the bundled CLI can select by name.
// Exactly one of File and Program is set. That constructor returns a fresh
// instance in the cop's canonical execution mode.
type Entry struct {
	// Name is the cop name used in .rubocop-go.yml, suppressions, and reports.
	Name string

	// Default reports whether the CLI runs the cop when the configuration
	// does not set its enabled value and does not set default_cops: false.
	Default bool

	// File constructs a file cop for runner.New.
	File func() cop.Cop

	// Program constructs a whole-program cop for runner.Runner.WithProgramCops.
	Program func() prog.Cop
}

// Catalog returns a fresh slice that describes every shipped cop once, in
// name order. Each cop appears in its canonical execution mode. Alternate
// file-based constructors, such as [NewLintSlicesCloneFile], are not separate
// entries.
func Catalog() []Entry {
	return []Entry{
		{Name: "Lint/BenchmarkLoop", Program: func() prog.Cop { return NewLintBenchmarkLoop() }},
		{Name: "Lint/CancellationLoop", Program: func() prog.Cop { return NewLintCancellationLoop() }},
		{Name: "Lint/CancellationSafeResultSend", Program: func() prog.Cop { return NewLintCancellationSafeResultSend() }},
		{Name: "Lint/ChannelStateCheck", Program: func() prog.Cop { return NewLintChannelStateCheck() }},
		{Name: "Lint/CloneCompleteness", Default: true, File: func() cop.Cop { return NewLintCloneCompleteness() }},
		{Name: "Lint/ClosedChannelLoop", Program: func() prog.Cop { return NewLintClosedChannelLoop() }},
		{Name: "Lint/ConstructorCommandExec", Program: func() prog.Cop { return NewLintConstructorCommandExec() }},
		{Name: "Lint/ConstructorNetworkIO", File: func() cop.Cop { return NewLintConstructorNetworkIO() }},
		{Name: "Lint/ConstructorPurity", File: func() cop.Cop { return NewLintConstructorPurity() }},
		{Name: "Lint/ContextConnectivity", Default: true, Program: func() prog.Cop { return NewLintContextConnectivity() }},
		{Name: "Lint/ContextFirstParameter", File: func() cop.Cop { return NewLintContextFirstParameter() }},
		{Name: "Lint/CutPrefix", Program: func() prog.Cop { return NewLintCutPrefix() }},
		{Name: "Lint/CutSuffix", Program: func() prog.Cop { return NewLintCutSuffix() }},
		{Name: "Lint/DeferMutexUnlock", File: func() cop.Cop { return NewLintDeferMutexUnlock() }},
		{Name: "Lint/ErrorStringMatching", File: func() cop.Cop { return NewLintErrorStringMatching() }},
		{Name: "Lint/ErrorsAsType", Program: func() prog.Cop { return NewLintErrorsAsType() }},
		{Name: "Lint/FieldsSeq", Program: func() prog.Cop { return NewLintFieldsSeq() }},
		{Name: "Lint/FieldsSeqLookup", Program: func() prog.Cop { return NewLintFieldsSeqLookup() }},
		{Name: "Lint/FmtPrint", Default: true, File: func() cop.Cop { return NewLintFmtPrint() }},
		{Name: "Lint/HTTPRequestWithContext", File: func() cop.Cop { return NewLintHTTPRequestWithContext() }},
		{Name: "Lint/HTTPTestRequestWithContext", Program: func() prog.Cop { return NewLintHTTPTestRequestWithContext() }},
		{Name: "Lint/JSONMarshalWrite", Program: func() prog.Cop { return NewLintJSONMarshalWrite() }},
		{Name: "Lint/MapsClone", Program: func() prog.Cop { return NewLintMapsClone() }},
		{Name: "Lint/MapsCopy", Program: func() prog.Cop { return NewLintMapsCopy() }},
		{Name: "Lint/NewExpr", File: func() cop.Cop { return NewLintNewExpr() }},
		{Name: "Lint/NoContextField", File: func() cop.Cop { return NewLintNoContextField() }},
		{Name: "Lint/NoFatalOutsideMain", File: func() cop.Cop { return NewLintNoFatalOutsideMain() }},
		{Name: "Lint/NoStdoutInLibraries", File: func() cop.Cop { return NewLintNoStdoutInLibraries() }},
		{Name: "Lint/NonblockingChannelReplacement", Program: func() prog.Cop { return NewLintNonblockingChannelReplacement() }},
		{Name: "Lint/OsExit", Default: true, File: func() cop.Cop { return NewLintOsExit() }},
		{Name: "Lint/PointerHelper", Program: func() prog.Cop { return NewLintPointerHelper() }},
		{Name: "Lint/ReflectFields", Program: func() prog.Cop { return NewLintReflectFields() }},
		{Name: "Lint/SlicesClone", Program: func() prog.Cop { return NewLintSlicesClone() }},
		{Name: "Lint/SlicesConcat", Program: func() prog.Cop { return NewLintSlicesConcat() }},
		{Name: "Lint/SlicesContains", Program: func() prog.Cop { return NewLintSlicesContains() }},
		{Name: "Lint/SlicesEqual", Program: func() prog.Cop { return NewLintSlicesEqual() }},
		{Name: "Lint/SlogContextual", File: func() cop.Cop { return NewLintSlogContextual() }},
		{Name: "Lint/SortStableFunc", Program: func() prog.Cop { return NewLintSortStableFunc() }},
		{Name: "Lint/SortedMapKeys", Program: func() prog.Cop { return NewLintSortedMapKeys() }},
		{Name: "Lint/SplitSeq", Program: func() prog.Cop { return NewLintSplitSeq() }},
		{Name: "Lint/SplitTrimJoin", Program: func() prog.Cop { return NewLintSplitTrimJoin() }},
		{Name: "Lint/StdlibUUID", Program: func() prog.Cop { return NewLintStdlibUUID() }},
		{Name: "Lint/StreamCloseSafety", Program: func() prog.Cop { return NewLintStreamCloseSafety() }},
		{Name: "Lint/URLClone", Program: func() prog.Cop { return NewLintURLClone() }},
		{Name: "Lint/WaitGroupGo", Program: func() prog.Cop { return NewLintWaitGroupGo() }},
		{Name: "Lint/WrapErrors", File: func() cop.Cop { return NewLintWrapErrors() }},
		{Name: "Style/EmptyFunc", Default: true, File: func() cop.Cop { return NewStyleEmptyFunc() }},
		{Name: "Style/ErrorNaming", Default: true, File: func() cop.Cop { return NewStyleErrorNaming() }},
	}
}

// All returns a fresh slice containing a fresh instance of every default
// built-in file cop, in [Catalog] order. The returned slice is independent of
// the package state, so callers may freely append or filter it. Shared opt-in
// cops are not included.
func All() []cop.Cop {
	var out []cop.Cop
	for _, e := range Catalog() {
		if e.Default && e.File != nil {
			out = append(out, e.File())
		}
	}
	return out
}

// AllProgram returns a fresh slice containing a fresh instance of every
// default built-in whole-program cop, in [Catalog] order. These run once over
// the entire loaded program (via go/packages + go/ssa) rather than once per
// file, so they can answer inter-procedural, cross-package questions. Pass
// them to the runner with runner.Runner.WithProgramCops.
func AllProgram() []prog.Cop {
	var out []prog.Cop
	for _, e := range Catalog() {
		if e.Default && e.Program != nil {
			out = append(out, e.Program())
		}
	}
	return out
}
