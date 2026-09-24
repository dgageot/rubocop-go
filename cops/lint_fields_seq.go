package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintFieldsSeq catches fields materialized only for one value-only range, allowing
// an empty-input fallback. Mutable bytes and FieldsFunc callbacks are excluded:
// scanning them lazily can change boundaries or side-effect timing.
func NewLintFieldsSeq(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newFieldsSeqFile(opts...))
}

func newFieldsSeqFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/FieldsSeq",
			Description: "Use strings.FieldsSeq when fields are only iterated once.",
			Severity:    cop.Warning,
		},
		MinGoVersion:     "go1.23",
		MinStdlibVersion: "go1.24",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil {
				return
			}
			uses := make(map[types.Object]int)
			ast.Inspect(p.File, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					uses[p.Info.Uses[id]]++
				}
				return true
			})
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.RangeStmt:
					if call := fieldsCall(p.Info, node.X); call != nil && fieldsValueRange(p.Info, node) {
						reportFieldsSeq(p, call)
					}
				case *ast.BlockStmt:
					checkFieldsSeq(p, node.List, uses)
				case *ast.CaseClause:
					checkFieldsSeq(p, node.Body, uses)
				case *ast.CommClause:
					checkFieldsSeq(p, node.Body, uses)
				}
				return true
			})
		},
	}, opts...)
}

func reportFieldsSeq(p *cop.Pass, call *ast.CallExpr) {
	p.Report(call, "use strings.FieldsSeq to avoid a slice used only for iteration; preserve input evaluation and any empty-input fallback with an explicit seen-word flag")
}

func fieldsCall(info *types.Info, expr ast.Expr) *ast.CallExpr {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return nil
	}
	fn, ok := calleeObject(info, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "strings" || fn.Name() != "Fields" {
		return nil
	}
	return call
}

func fieldsValueRange(info *types.Info, loop *ast.RangeStmt) bool {
	if loop.Key != nil {
		key, ok := loop.Key.(*ast.Ident)
		if !ok || key.Name != "_" {
			return false
		}
	}
	// Iterator bodies run in a yield callback; a direct recover stops working.
	valid := true
	for _, node := range []ast.Node{loop.Value, loop.Body} {
		if node == nil {
			continue
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && info.Uses[id] == types.Universe.Lookup("recover") {
					valid = false
				}
			}
			return valid
		})
	}
	return valid
}

func checkFieldsSeq(p *cop.Pass, stmts []ast.Stmt, uses map[types.Object]int) {
	for i, stmt := range stmts {
		obj, init := fieldsLocal(p.Info, stmt)
		call := fieldsCall(p.Info, init)
		if obj == nil || call == nil {
			continue
		}
		rest := stmts[i+1:]
		allowedUses := 1
		var guard *ast.IfStmt
		if len(rest) > 0 {
			if candidate, ok := rest[0].(*ast.IfStmt); ok && fieldsEmptyGuard(p.Info, candidate, obj) {
				guard = candidate
				allowedUses++
				rest = rest[1:]
			}
		}
		if uses[obj] != allowedUses {
			continue
		}
		for _, next := range rest {
			if loop, ok := next.(*ast.RangeStmt); ok {
				id, ok := ast.Unparen(loop.X).(*ast.Ident)
				if ok && p.Info.Uses[id] == obj && fieldsValueRange(p.Info, loop) {
					reportFieldsSeq(p, call)
				}
				break
			}
			if !fieldsSafeInit(p.Info, next, guard) {
				break
			}
		}
	}
}

func fieldsLocal(info *types.Info, stmt ast.Stmt) (types.Object, ast.Expr) {
	if decl, ok := stmt.(*ast.DeclStmt); ok {
		gen, ok := decl.Decl.(*ast.GenDecl)
		if ok && gen.Tok == token.VAR && len(gen.Specs) == 1 {
			value, ok := gen.Specs[0].(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && len(value.Values) == 1 && value.Names[0].Name != "_" {
				return info.Defs[value.Names[0]], value.Values[0]
			}
		}
		return nil, nil
	}
	return splitTrimLocal(info, stmt)
}

func fieldsEmptyGuard(info *types.Info, guard *ast.IfStmt, obj types.Object) bool {
	if guard.Init != nil || guard.Else != nil || len(guard.Body.List) == 0 {
		return false
	}
	cond, ok := ast.Unparen(guard.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.EQL {
		return false
	}
	length, zero := cond.X, cond.Y
	if value := info.Types[length].Value; value != nil {
		length, zero = zero, length
	}
	value := info.Types[zero].Value
	if value == nil || value.Kind() != constant.Int || constant.Sign(value) != 0 {
		return false
	}
	call, ok := ast.Unparen(length).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || calleeObject(info, call) != types.Universe.Lookup("len") {
		return false
	}
	id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	if !ok || info.Uses[id] != obj {
		return false
	}
	// Bare returns have implicit result bindings that later declarations can shadow.
	bareReturn := false
	ast.Inspect(guard.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) == 0 {
			bareReturn = true
		}
		return !bareReturn
	})
	if bareReturn {
		return false
	}
	switch last := guard.Body.List[len(guard.Body.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return last.Tok == token.CONTINUE && last.Label == nil
	default:
		return false
	}
}

// Only inert local initialization may precede the range. Moving the empty
// fallback past it must not change side effects, panic timing, or name binding.
func fieldsSafeInit(info *types.Info, stmt ast.Stmt, guard *ast.IfStmt) bool {
	var names []*ast.Ident
	var values []ast.Expr
	switch stmt := stmt.(type) {
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR {
			return false
		}
		for _, spec := range decl.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				return false
			}
			names = append(names, value.Names...)
			values = append(values, value.Values...)
		}
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE {
			return false
		}
		for _, lhs := range stmt.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || info.Defs[id] == nil {
				return false
			}
			names = append(names, id)
		}
		values = stmt.Rhs
	default:
		return false
	}
	for _, expr := range values {
		if info.Types[expr].Value == nil {
			return false
		}
	}
	if guard != nil {
		for _, name := range names {
			shadowed := false
			ast.Inspect(guard.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == name.Name && info.Uses[id] != nil {
					shadowed = true
				}
				return !shadowed
			})
			if shadowed {
				return false
			}
		}
	}
	return true
}
