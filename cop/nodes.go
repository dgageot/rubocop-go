package cop

import (
	"go/ast"
	"iter"
	"reflect"
)

// Nodes yields nodes of type T in depth-first order, including root when it
// matches. Returning from the loop stops traversal. A nil root yields nothing.
func Nodes[T ast.Node](root ast.Node) iter.Seq[T] {
	return func(yield func(T) bool) {
		value := reflect.ValueOf(root)
		if root == nil || (value.Kind() == reflect.Pointer && value.IsNil()) {
			return
		}
		for node := range ast.Preorder(root) {
			if n, ok := node.(T); ok && !yield(n) {
				return
			}
		}
	}
}

// On creates a cop that checks each node of type T in the file. It accepts
// the same scope and type-information options as New. Function literals
// and local declarations are included; use Pass.ForEachFunc for top-level functions.
//
//	cop.On(meta, func(p *cop.Pass, call *ast.CallExpr) {
//		if cop.IsCallTo(call, "os", "Exit") {
//			p.Report(call, "avoid os.Exit")
//		}
//	})
func On[T ast.Node](meta Meta, check func(*Pass, T), opts ...FuncOption) *Func {
	return New(meta, func(p *Pass) {
		for node := range Nodes[T](p.File) {
			check(p, node)
		}
	}, opts...)
}
