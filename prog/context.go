package prog

import (
	"golang.org/x/tools/go/ssa"
)

// CalleeID returns the package path and name of a call's static callee, e.g.
// ("context", "Background") for context.Background(). The third result is
// false for dynamically dispatched (interface) calls and for callees with
// no enclosing package (some synthetic wrappers).
func CalleeID(call *ssa.Call) (pkgPath, name string, ok bool) {
	if call == nil {
		return "", "", false
	}
	return CalleeCommonID(call.Common())
}

// CalleeCommonID is like [CalleeID] for any call-like instruction's common
// call payload (ordinary calls, go calls, and defer calls).
func CalleeCommonID(common *ssa.CallCommon) (pkgPath, name string, ok bool) {
	if common == nil {
		return "", "", false
	}
	fn := common.StaticCallee()
	if fn == nil {
		return "", "", false
	}
	return funcID(fn)
}

func funcID(fn *ssa.Function) (pkgPath, name string, ok bool) {
	if fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return "", "", false
	}
	return fn.Pkg.Pkg.Path(), fn.Name(), true
}

// IsContextRootCall reports whether call mints a fresh root context, i.e.
// it is context.Background() or context.TODO(). These are the only two
// functions in the standard library that produce a context from nothing;
// every other context must be derived from an existing one.
func IsContextRootCall(call *ssa.Call) bool {
	pkg, name, ok := CalleeID(call)
	return ok && pkg == "context" && (name == "Background" || name == "TODO")
}

// EnclosingIsMain reports whether fn is func main of a main package — the
// conventional home of a program's root context.
func EnclosingIsMain(fn *ssa.Function) bool {
	if fn == nil || fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return false
	}
	return fn.Name() == "main" && fn.Pkg.Pkg.Name() == "main"
}

// contextResultIndex returns the position of the (single) context.Context
// result in a function's signature, or -1 when the function does not return
// a context. Used to decide whether a call is a "context producer" worth
// stopping the backward trace at.
func contextResultIndex(fn *ssa.Function) int {
	if fn == nil || fn.Signature == nil {
		return -1
	}
	res := fn.Signature.Results()
	for i := range res.Len() {
		if IsContextType(res.At(i).Type()) {
			return i
		}
	}
	return -1
}

// ProducesContext reports whether call yields a context.Context, either as
// its sole result or as one component of a tuple result.
func ProducesContext(call *ssa.Call) bool {
	if IsContextType(call.Type()) {
		return true
	}
	if fn := call.Common().StaticCallee(); fn != nil {
		return contextResultIndex(fn) >= 0
	}
	return false
}
