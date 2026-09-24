package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintReflectFields covers paired metadata/value loops not handled by stditerators.
func NewLintReflectFields(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newReflectFieldsFile(opts...))
}

func newReflectFieldsFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ReflectFields",
			Description: "Use reflection iterators for paired field metadata and values.",
			Severity:    cop.Warning,
		},
		MinGoVersion:     "go1.23",
		MinStdlibVersion: "go1.26",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.FuncDecl:
					checkReflectFields(p, node.Type, node.Body)
				case *ast.FuncLit:
					checkReflectFields(p, node.Type, node.Body)
				}
				return true
			})
		},
	}, opts...)
}

func checkReflectFields(p *cop.Pass, fn *ast.FuncType, body *ast.BlockStmt) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		loop, ok := n.(*ast.RangeStmt)
		if !ok || loop.Tok != token.DEFINE || loop.Value != nil {
			return true
		}
		index, ok := loop.Key.(*ast.Ident)
		if !ok || index.Name == "_" || p.Info.Defs[index] == nil {
			return true
		}
		bound := reflectFieldsMethod(p.Info, loop.X, "Value", "NumField", 0)
		if bound == nil {
			return true
		}
		receiver, ok := ast.Unparen(bound.X).(*ast.Ident)
		if !ok {
			return true
		}
		obj := p.Info.Uses[receiver]
		if obj == nil || obj.Pos() < fn.Pos() || obj.Pos() >= body.End() {
			return true // A captured or global receiver can change through a callback.
		}
		allowed := map[*ast.Ident]bool{receiver: true}
		if !reflectFieldsBody(p.Info, loop.Body, p.Info.Defs[index], obj, reflectFieldsYield(p.Info, fn), allowed) {
			return true
		}
		// A dedicated receiver cannot escape, be reassigned, or be captured elsewhere.
		valid := true
		ast.Inspect(body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && p.Info.Uses[id] == obj && !allowed[id] {
				valid = false
			}
			return valid
		})
		if valid {
			p.Reportf(loop, "use range %s.Fields() for paired metadata and values; preserve declaration order, continue behavior, and early stopping", receiver.Name)
		}
		return true
	})
}

func reflectFieldsBody(info *types.Info, body *ast.BlockStmt, index, receiver, yield types.Object, allowed map[*ast.Ident]bool) bool {
	valid, values := true, false
	metadata := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if !valid {
			return false
		}
		switch node := n.(type) {
		case *ast.FuncLit:
			ast.Inspect(node, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && (info.Uses[id] == index || info.Uses[id] == receiver) {
					valid = false
				}
				return valid
			})
			return false
		case *ast.DeferStmt, *ast.GoStmt, *ast.ForStmt, *ast.RangeStmt:
			valid = false
		case *ast.BranchStmt:
			valid = node.Tok != token.GOTO
		case *ast.Ident:
			if info.Uses[node] == index || info.Uses[node] == receiver {
				valid = false
			}
		case *ast.CallExpr:
			field := reflectFieldsMethod(info, node, "Value", "Field", 1)
			isMetadata := false
			if field == nil {
				field = reflectFieldsMethod(info, node, "Type", "Field", 1)
				isMetadata = true
			}
			if field != nil {
				id, ok := ast.Unparen(node.Args[0]).(*ast.Ident)
				if !ok || info.Uses[id] != index {
					valid = false
					break
				}
				x := field.X
				if isMetadata {
					typ := reflectFieldsMethod(info, x, "Value", "Type", 0)
					if typ == nil {
						valid = false
						break
					}
					x = typ.X
				}
				id, ok = ast.Unparen(x).(*ast.Ident)
				if !ok || info.Uses[id] != receiver {
					valid = false
					break
				}
				allowed[id] = true
				if isMetadata {
					metadata++
				} else {
					values = true
				}
				return false
			}
			valid = reflectFieldsSafeCall(info, node, yield)
		}
		return valid
	})
	// Repeated Type.Field calls produce independent StructField.Index slices.
	return valid && values && metadata == 1
}

func reflectFieldsMethod(info *types.Info, expr ast.Expr, typ, method string, arity int) *ast.SelectorExpr {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != arity || call.Ellipsis.IsValid() {
		return nil
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	selection := info.Selections[sel]
	if selection == nil || selection.Kind() != types.MethodVal {
		return nil
	}
	named, ok := types.Unalias(info.TypeOf(sel.X)).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "reflect" || named.Obj().Name() != typ {
		return nil
	}
	obj := selection.Obj()
	if obj.Pkg() == nil || obj.Pkg().Path() != "reflect" || obj.Name() != method {
		return nil
	}
	return sel
}

func reflectFieldsSafeCall(info *types.Info, call *ast.CallExpr, yield types.Object) bool {
	if info.Types[call.Fun].IsType() {
		return true
	}
	obj := calleeObject(info, call)
	if obj == nil {
		return false
	}
	if obj == yield {
		return true
	}
	if builtin, ok := obj.(*types.Builtin); ok {
		switch builtin.Name() {
		case "len", "cap", "append", "make", "new":
			return true
		}
	}
	if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == "strings" && fn.Name() == "Cut" {
		return true
	}
	for _, method := range []string{"Len", "Interface", "Kind", "IsNil", "IsZero", "CanInterface"} {
		if reflectFieldsMethod(info, call, "Value", method, 0) != nil {
			return true
		}
	}
	return reflectFieldsMethod(info, call, "StructTag", "Get", 1) != nil || reflectFieldsMethod(info, call, "StructTag", "Lookup", 1) != nil
}

// An iterator's callback is allowed only with a local, non-escaping receiver.
func reflectFieldsYield(info *types.Info, fn *ast.FuncType) types.Object {
	if fn.Results != nil || fn.Params == nil || len(fn.Params.List) != 1 || len(fn.Params.List[0].Names) != 1 {
		return nil
	}
	obj := info.Defs[fn.Params.List[0].Names[0]]
	if obj == nil {
		return nil
	}
	sig, ok := obj.Type().Underlying().(*types.Signature)
	if !ok || sig.Variadic() || sig.Params().Len() < 1 || sig.Params().Len() > 2 || sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), types.Typ[types.Bool]) {
		return nil
	}
	return obj
}
