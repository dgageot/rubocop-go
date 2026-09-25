package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintFieldsSeqLookup reports first-field and membership lookups that can stop early.
func NewLintFieldsSeqLookup(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newFieldsSeqLookupFile(opts...))
}

func newFieldsSeqLookupFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/FieldsSeqLookup",
			Description: "Use strings.FieldsSeq for first-field and membership lookups.",
			Severity:    cop.Warning,
		},
		Types:            true,
		MinGoVersion:     "go1.23",
		MinStdlibVersion: "go1.24",
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
				case *ast.CallExpr:
					if lookupFunction(p.Info, node, "slices", "Contains") && len(node.Args) == 2 {
						if call := lookupFieldsCall(p.Info, node.Args[0]); call != nil {
							p.Report(call, "use strings.FieldsSeq with an early exit for membership lookup; evaluate the input and search value once, in their original order, before iterating")
						}
					}
				case *ast.IfStmt:
					obj, call := lookupFieldsLocal(p.Info, node.Init)
					if call != nil && uses[obj] == 2 && lookupLengthGuard(p.Info, node, obj, false) && len(node.Body.List) > 0 && lookupFirstRead(p.Info, node.Body.List[0], obj) {
						reportFirstField(p, call)
					}
				case *ast.BlockStmt:
					checkFirstFieldLookup(p, node.List, uses)
				case *ast.CaseClause:
					checkFirstFieldLookup(p, node.Body, uses)
				case *ast.CommClause:
					checkFirstFieldLookup(p, node.Body, uses)
				}
				return true
			})
		},
	}, opts...)
}

func lookupFunction(info *types.Info, call *ast.CallExpr, path, name string) bool {
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == path && fn.Name() == name && fn.Signature().Recv() == nil
}

func lookupFieldsCall(info *types.Info, expr ast.Expr) *ast.CallExpr {
	if expr == nil {
		return nil
	}
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !lookupFunction(info, call, "strings", "Fields") {
		return nil
	}
	return call
}

func lookupFieldsLocal(info *types.Info, stmt ast.Stmt) (types.Object, *ast.CallExpr) {
	var name *ast.Ident
	var value ast.Expr
	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE || len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
			return nil, nil
		}
		name, _ = stmt.Lhs[0].(*ast.Ident)
		value = stmt.Rhs[0]
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR || len(decl.Specs) != 1 {
			return nil, nil
		}
		spec, ok := decl.Specs[0].(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return nil, nil
		}
		name, value = spec.Names[0], spec.Values[0]
	}
	if name == nil || name.Name == "_" || info.Defs[name] == nil {
		return nil, nil
	}
	return info.Defs[name], lookupFieldsCall(info, value)
}

func checkFirstFieldLookup(p *cop.Pass, stmts []ast.Stmt, uses map[types.Object]int) {
	for i := 0; i+2 < len(stmts); i++ {
		obj, call := lookupFieldsLocal(p.Info, stmts[i])
		if call == nil || uses[obj] != 2 {
			continue
		}
		guard, ok := stmts[i+1].(*ast.IfStmt)
		if !ok || guard.Init != nil || guard.Else != nil || !lookupLengthGuard(p.Info, guard, obj, true) || len(guard.Body.List) == 0 {
			continue
		}
		if _, ok := guard.Body.List[len(guard.Body.List)-1].(*ast.ReturnStmt); !ok {
			continue
		}
		if lookupFirstRead(p.Info, stmts[i+2], obj) {
			reportFirstField(p, call)
		}
	}
}

func lookupLengthGuard(info *types.Info, guard *ast.IfStmt, obj types.Object, empty bool) bool {
	cond, ok := ast.Unparen(guard.Cond).(*ast.BinaryExpr)
	if !ok {
		return false
	}
	length, zero, op := cond.X, cond.Y, cond.Op
	if lookupZero(info, length) {
		length, zero = zero, length
		switch op {
		case token.LSS:
			op = token.GTR
		case token.GTR:
			op = token.LSS
		}
	}
	if !lookupZero(info, zero) || (empty && op != token.EQL) || (!empty && op != token.GTR && op != token.NEQ) {
		return false
	}
	call, ok := ast.Unparen(length).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || typeutil.Callee(info, call) != types.Universe.Lookup("len") {
		return false
	}
	id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	return ok && info.Uses[id] == obj
}

// Restrict extraction to reads evaluated immediately after the empty-input guard.
func lookupFirstRead(info *types.Info, stmt ast.Stmt, obj types.Object) bool {
	var exprs []ast.Expr
	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		exprs = stmt.Rhs
	case *ast.ReturnStmt:
		exprs = stmt.Results
	case *ast.SwitchStmt:
		if stmt.Init == nil && stmt.Tag != nil {
			exprs = []ast.Expr{stmt.Tag}
		}
	}
	found := false
	for _, expr := range exprs {
		ast.Inspect(expr, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.UnaryExpr:
				if node.Op == token.AND {
					return false
				}
			case *ast.IndexExpr:
				id, ok := ast.Unparen(node.X).(*ast.Ident)
				if ok && info.Uses[id] == obj && lookupZero(info, node.Index) {
					found = true
				}
			}
			return true
		})
	}
	return found
}

func lookupZero(info *types.Info, expr ast.Expr) bool {
	value := info.Types[expr].Value
	return value != nil && value.Kind() == constant.Int && constant.Sign(value) == 0
}

func reportFirstField(p *cop.Pass, call *ast.CallExpr) {
	p.Report(call, "use strings.FieldsSeq and stop after the first field; extract it at the original declaration and preserve the empty-input fallback")
}
