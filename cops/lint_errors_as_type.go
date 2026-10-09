package cops

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintErrorsAsType reports fresh error targets consumed only on success.
func NewLintErrorsAsType(opts ...cop.FuncOption) *prog.Func {
	return modernizationProgram(newErrorsAsTypeFile(opts...))
}

func newErrorsAsTypeFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/ErrorsAsType",
			Description: "Use errors.AsType for fresh error targets consumed only on success.",
			Severity:    cop.Convention,
		},
		MinStdlibVersion: "go1.26",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BlockStmt:
					checkErrorsAsType(p, n.List)
				case *ast.CaseClause:
					checkErrorsAsType(p, n.Body)
				case *ast.CommClause:
					checkErrorsAsType(p, n.Body)
				}
				return true
			})
		},
	}, opts...)
}

func checkErrorsAsType(p *cop.Pass, stmts []ast.Stmt) {
	for i := 0; i+1 < len(stmts); i++ {
		decl, ok := stmts[i].(*ast.DeclStmt)
		if !ok {
			continue
		}
		gen, ok := decl.Decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
			continue
		}
		spec, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 0 {
			continue
		}
		target := p.Info.Defs[spec.Names[0]]
		if target == nil || !types.Implements(target.Type(), types.Universe.Lookup("error").Type().Underlying().(*types.Interface)) {
			continue
		}
		guard, ok := stmts[i+1].(*ast.IfStmt)
		if !ok || guard.Init != nil {
			continue
		}
		call, ok := ast.Unparen(guard.Cond).(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			continue
		}
		fn, ok := p.CalleeObject(call).(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "errors" || fn.Name() != "As" {
			continue
		}
		addr, ok := ast.Unparen(call.Args[1]).(*ast.UnaryExpr)
		if !ok || addr.Op != token.AND {
			continue
		}
		id, ok := ast.Unparen(addr.X).(*ast.Ident)
		if !ok || p.Info.ObjectOf(id) != target || !errorsAsTypeNoCustomAs(p.Info, call.Args[0], target.Type()) {
			continue
		}
		// Keep the target inside the success scope and reject explicit aliases.
		valid := true
		ast.Inspect(p.File, func(n ast.Node) bool {
			if use, ok := n.(*ast.Ident); ok && p.Info.Uses[use] == target && use != id {
				if use.Pos() < guard.Body.Pos() || use.End() > guard.Body.End() {
					valid = false
				}
			}
			if address, ok := n.(*ast.UnaryExpr); ok && address.Op == token.AND && address != addr {
				ast.Inspect(address.X, func(n ast.Node) bool {
					if use, ok := n.(*ast.Ident); ok && p.Info.Uses[use] == target {
						valid = false
					}
					return true
				})
			}
			return valid
		})
		if valid {
			p.Report(call, "use errors.AsType[T](err) with a success-scoped target instead of var plus errors.As")
		}
	}
}

// Custom As can retain the target pointer; AsType returns a copy of that target.
// Only match a guaranteed direct assignment or a concrete, unwrapped leaf error.
func errorsAsTypeNoCustomAs(info *types.Info, source ast.Expr, target types.Type) bool {
	typ := info.TypeOf(source)
	if typ == nil {
		return false
	}
	if types.AssignableTo(typ, target) {
		_, interfaceTarget := target.Underlying().(*types.Interface)
		_, parameterTarget := types.Unalias(target).(*types.TypeParam)
		// As uses reflection assignability; a non-interface type assertion needs
		// an identical dynamic type, not just an identical underlying type.
		return types.Identical(typ, target) || (interfaceTarget && !parameterTarget)
	}
	if _, ok := typ.Underlying().(*types.Interface); ok {
		return false
	}
	methods := types.NewMethodSet(typ)
	return methods.Lookup(nil, "As") == nil && methods.Lookup(nil, "Unwrap") == nil
}
