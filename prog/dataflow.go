package prog

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// TraceOptions tunes a backward dataflow walk. Both hooks are optional; the
// zero value walks until it reaches structural leaves.
type TraceOptions struct {
	// Stop, when it returns true for a value, records that value as an
	// origin and stops descending through it. Use it to halt at a domain
	// boundary — e.g. "stop at any value that is a context producer" — so
	// the result is the set of producers rather than the primitives they
	// were built from.
	Stop func(ssa.Value) bool

	// Redirect lets the walk see through a call the tracer would otherwise
	// treat as an opaque leaf (typically a call into a function with no SSA
	// body, such as a stdlib function). When it returns (v, true) the walk
	// continues from v instead of recording the call. The archetype is
	// context.WithCancel(parent): redirect to parent so a derived context
	// traces back to the context it was derived from.
	Redirect func(*ssa.Call) (ssa.Value, bool)
}

// Origins computes the set of "source" values that flow into v, walking SSA
// def-use chains backwards and crossing function boundaries via the call
// graph. It is the inter-procedural backward-dataflow primitive that
// whole-program cops build on.
//
// A value is treated as a source (and reported as an origin) when it is
// not produced by an instruction we know how to look through. Concretely,
// Origins looks through:
//
//   - Calls whose callee is known (StaticCallee, or — for interface calls
//     — every callee the call graph attributes to the site): the call's
//     origins become the origins of the values returned by each callee.
//   - Parameters: replaced by the actual argument at every call site that
//     the call graph records as reaching the parameter's function.
//   - Phi nodes, Extract (tuple component), and the common copy-like
//     instructions (ChangeType, Convert, MakeInterface, ...): replaced by
//     their operands.
//
// Everything else — a call into a function with no SSA body (an external
// or stdlib function whose source we did not lower), a global, a constant,
// a freevar, an allocation, a field load — is a leaf and is returned as an
// origin, unless opts.Redirect chooses to look through it.
//
// The walk is bounded by a visited set keyed on SSA value identity, so
// recursion through cycles (loops lowered to phi, mutually recursive
// functions) terminates.
func (p *Program) Origins(v ssa.Value, opts TraceOptions) []ssa.Value {
	t := &tracer{prog: p, opts: opts, seen: map[ssa.Value]bool{}}
	t.walk(v)
	return t.origins
}

type tracer struct {
	prog    *Program
	opts    TraceOptions
	seen    map[ssa.Value]bool
	origins []ssa.Value
}

func (t *tracer) record(v ssa.Value) {
	t.origins = append(t.origins, v)
}

func (t *tracer) walk(v ssa.Value) {
	if v == nil || t.seen[v] {
		return
	}
	t.seen[v] = true

	// A caller-supplied boundary turns v into an origin without further
	// descent. Checked before the structural cases so that, e.g., a call
	// returning a context.Context is reported as the producer instead of
	// being looked through to its arguments.
	if t.opts.Stop != nil && t.opts.Stop(v) {
		t.record(v)
		return
	}

	switch val := v.(type) {
	case *ssa.Call:
		t.walkCall(val)
	case *ssa.Parameter:
		t.walkParameter(val)
	case *ssa.Phi:
		for _, e := range val.Edges {
			t.walk(e)
		}
	case *ssa.Extract:
		// Component of a tuple (typically a multi-value call result). Look
		// through to the tuple; walkCall handles result routing.
		t.walk(val.Tuple)
	case *ssa.ChangeType:
		t.walk(val.X)
	case *ssa.Convert:
		t.walk(val.X)
	case *ssa.ChangeInterface:
		t.walk(val.X)
	case *ssa.MakeInterface:
		t.walk(val.X)
	case *ssa.Field:
		t.record(val) // a struct field load is an opaque source
	case *ssa.FieldAddr:
		t.record(val)
	case *ssa.UnOp:
		// *p (pointer load) and similar: the loaded value is opaque to a
		// def-use walk, so treat it as a source rather than chase aliases.
		t.record(val)
	default:
		t.record(v)
	}
}

// walkCall looks through a call to the origins of the values its callees
// return. For a statically known callee with a body, that is the set of
// values appearing in the callee's return statements at the result index
// this call extracts. Calls with no resolvable body (external/stdlib) are
// recorded as origins, unless opts.Redirect chooses an argument to follow.
func (t *tracer) walkCall(call *ssa.Call) {
	if t.opts.Redirect != nil {
		if target, ok := t.opts.Redirect(call); ok {
			t.walk(target)
			return
		}
	}
	callees := t.callees(call)
	if len(callees) == 0 {
		t.record(call)
		return
	}
	for _, fn := range callees {
		if fn.Blocks == nil {
			// No body to look into (external/stdlib): the call itself is
			// the origin.
			t.record(call)
			continue
		}
		for _, ret := range returnValues(fn) {
			t.walk(ret)
		}
	}
}

// walkParameter replaces a parameter with the actual arguments passed at
// every call site that the call graph records as reaching the parameter's
// function. A parameter of a function with no recorded caller (an entry
// point, or a function only reached via an edge CHA could not attribute)
// is recorded as an origin so the value is never silently dropped.
func (t *tracer) walkParameter(param *ssa.Parameter) {
	fn := param.Parent()
	if fn == nil {
		t.record(param)
		return
	}
	idx := paramIndex(fn, param)
	if idx < 0 {
		t.record(param)
		return
	}

	node := t.prog.CallGraph.Nodes[fn]
	if node == nil || len(node.In) == 0 {
		t.record(param)
		return
	}

	matched := false
	for _, edge := range node.In {
		site := edge.Site
		if site == nil {
			continue
		}
		args := site.Common().Args
		if idx < len(args) {
			matched = true
			t.walk(args[idx])
		}
	}
	if !matched {
		t.record(param)
	}
}

// callees returns the set of functions a call may dispatch to. A static
// call yields its single callee; an interface ("invoke") call yields every
// callee the call graph attributes to the site.
func (t *tracer) callees(call *ssa.Call) []*ssa.Function {
	if fn := call.Common().StaticCallee(); fn != nil {
		return []*ssa.Function{fn}
	}
	caller := call.Parent()
	if caller == nil {
		return nil
	}
	node := t.prog.CallGraph.Nodes[caller]
	if node == nil {
		return nil
	}
	var out []*ssa.Function
	for _, edge := range node.Out {
		if edge.Site == call && edge.Callee != nil && edge.Callee.Func != nil {
			out = append(out, edge.Callee.Func)
		}
	}
	return out
}

// returnValues collects every value that appears in a return statement of
// fn. When fn returns multiple values the slice mixes positions; callers
// that care about a specific result index should filter by type, which is
// sufficient for the single-context-result functions these cops target.
func returnValues(fn *ssa.Function) []ssa.Value {
	var out []ssa.Value
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			ret, ok := instr.(*ssa.Return)
			if !ok {
				continue
			}
			out = append(out, ret.Results...)
		}
	}
	return out
}

// paramIndex returns the position of param in fn.Params, or -1.
func paramIndex(fn *ssa.Function, param *ssa.Parameter) int {
	for i, p := range fn.Params {
		if p == param {
			return i
		}
	}
	return -1
}

// IsContextType reports whether t is context.Context (the named interface
// from the standard library). It is the type-level counterpart of the
// syntactic cop.IsContextType, used when walking SSA where full type
// information is available.
func IsContextType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Context" &&
		obj.Pkg() != nil && obj.Pkg().Path() == "context"
}
