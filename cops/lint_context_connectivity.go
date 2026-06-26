package cops

import (
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintContextConnectivity returns a whole-program cop that proves every
// context.Context consumed in the program is connected to the program's
// root context.
//
// This is the canonical inter-procedural dataflow rule: the question
// "is every context connected to the root context?" cannot be answered by
// looking at a single file or even a single package, because a context is
// routinely threaded through parameters, returns, struct fields, and
// several packages before it is finally passed to the function that uses
// it. The cop answers it by walking the program's SSA backwards across
// function boundaries (via prog.Program.Origins) from every place a
// context is consumed.
//
// Model
//
//	root        := the context.Background()/TODO() reachable from func main
//	derived(c)  := context.WithCancel/WithValue/... (parent: c)
//	connected(c) := c == root || c == derived(c') for some connected c'
//	             || c is a parameter threaded from a connected context
//
// A context is *consumed* when it is passed as an argument to a call. For
// each consumed context value, the cop computes its origins: the set of
// context producers it can flow from. Every origin must be either the root
// context or a parameter the program threads in from a caller. An origin
// that is a context.Background()/TODO() call other than the root is a
// *detached* context — it is created from nothing, disconnected from the
// cancellation/deadline/values tree rooted at main, and is reported.
//
// Derivation calls (context.WithCancel and friends) are looked through to
// their parent argument, so a context derived several times still traces
// back to its root.
//
// Suppress a deliberate detached root with
// //rubocop:disable Lint/ContextConnectivity on the line of the
// context.Background()/TODO() call.
func NewLintContextConnectivity() *prog.Func {
	return prog.New(cop.Meta{
		Name:        "Lint/ContextConnectivity",
		Description: "every context.Context must derive from the program's root context",
		Severity:    cop.Warning,
	}, func(p *prog.Pass) {
		roots := rootContexts(p.Program)

		// Trace every consumed context value back to its producers, and
		// report any producer that is a detached root.
		seen := map[ssa.Value]bool{}
		for _, fn := range p.Program.AllFunctions() {
			for _, cv := range consumedContexts(fn) {
				origins := p.Program.Origins(cv, prog.TraceOptions{
					Redirect: derivationParent,
				})
				for _, o := range origins {
					call, ok := o.(*ssa.Call)
					if !ok || !prog.IsContextRootCall(call) {
						continue
					}
					if roots[call] || seen[call] {
						continue
					}
					seen[call] = true
					p.Reportf(call.Pos(),
						"detached context: %s is created from nothing and never "+
							"connected to the program's root context; derive it from an "+
							"in-scope context instead (or annotate the line with "+
							"//rubocop:disable Lint/ContextConnectivity if it is an "+
							"intentional independent root)",
						rootCallName(call))
				}
			}
		}
	})
}

// rootContexts returns the set of context.Background()/TODO() calls that are
// legitimate program roots: those made directly inside func main. A program
// is allowed exactly the roots it mints in main; everything else must derive
// from one of them.
func rootContexts(p *prog.Program) map[*ssa.Call]bool {
	roots := map[*ssa.Call]bool{}
	for _, fn := range p.AllFunctions() {
		if !prog.EnclosingIsMain(fn) {
			continue
		}
		for _, call := range rootCallsIn(fn) {
			roots[call] = true
		}
	}
	return roots
}

// rootCallsIn returns every context.Background()/TODO() call in fn's body.
func rootCallsIn(fn *ssa.Function) []*ssa.Call {
	var out []*ssa.Call
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(*ssa.Call); ok && prog.IsContextRootCall(call) {
				out = append(out, call)
			}
		}
	}
	return out
}

// consumedContexts returns every context.Context value passed as an
// argument to a call within fn — the points where a context is actually
// "used". Tracing back from these consumption points (rather than from
// every context value) keeps the analysis focused on contexts that reach a
// consumer, which is what the connectivity property is about.
func consumedContexts(fn *ssa.Function) []ssa.Value {
	var out []ssa.Value
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			call, ok := instr.(ssa.CallInstruction)
			if !ok {
				continue
			}
			// Skip context derivation calls themselves: their context
			// argument is the parent, not a final consumption, and is
			// covered by the backward trace.
			if isDerivation(call) {
				continue
			}
			for _, arg := range prog.CallArgs(call) {
				if isContextValue(arg) {
					out = append(out, arg)
				}
			}
		}
	}
	return out
}

// derivationParent looks through a context derivation call
// (context.WithCancel/WithValue/WithTimeout/WithDeadline/WithCancelCause...)
// to its parent context argument, so a derived context traces back to the
// context it was derived from.
func derivationParent(call *ssa.Call) (ssa.Value, bool) {
	if !isDerivation(call) {
		return nil, false
	}
	for _, arg := range call.Common().Args {
		if isContextValue(arg) {
			return arg, true
		}
	}
	return nil, false
}

// isDerivation reports whether call is one of the context.With* derivation
// functions, which take a parent context and return a child.
func isDerivation(call ssa.CallInstruction) bool {
	pkg, name, ok := prog.CalleeCommonID(call.Common())
	if !ok || pkg != "context" {
		return false
	}
	switch name {
	case "WithCancel", "WithCancelCause", "WithValue",
		"WithTimeout", "WithTimeoutCause", "WithDeadline", "WithDeadlineCause",
		"WithoutCancel":
		return true
	}
	return false
}

// isContextValue reports whether v has type context.Context.
func isContextValue(v ssa.Value) bool {
	return v != nil && isContextType(v.Type())
}

func isContextType(t types.Type) bool {
	return prog.IsContextType(t)
}

// rootCallName renders a root call for diagnostics, e.g. "context.Background()".
func rootCallName(call *ssa.Call) string {
	pkg, name, ok := prog.CalleeID(call)
	if !ok {
		return "context.Background()"
	}
	return pkg + "." + name + "()"
}
