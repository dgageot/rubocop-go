package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintJSONMarshalWrite advises replacing a single buffered encoding, never a stream.
func NewLintJSONMarshalWrite(opts ...cop.FuncOption) *prog.Func {
	return prog.FromFile(newJSONMarshalWriteFile(opts...))
}

func newJSONMarshalWriteFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/JSONMarshalWrite",
			Description: "Consider json.MarshalWrite instead of encoding and trimming a newline.",
			Severity:    cop.Warning,
		},
		MinStdlibVersion: "go1.27",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			uses := make(map[types.Object]int)
			ast.Inspect(p.File, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && p.Info.Uses[id] != nil {
					uses[p.Info.Uses[id]]++
				}
				return true
			})
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BlockStmt:
					checkJSONMarshalWrite(p, node.List, uses)
				case *ast.CaseClause:
					checkJSONMarshalWrite(p, node.Body, uses)
				case *ast.CommClause:
					checkJSONMarshalWrite(p, node.Body, uses)
				}
				return true
			})
		},
	}, opts...)
}

func checkJSONMarshalWrite(p *cop.Pass, stmts []ast.Stmt, uses map[types.Object]int) {
	// The buffer, encoder, optional escaping, error guard, and return must be adjacent.
	for i := max(0, len(stmts)-5); i+3 < len(stmts); i++ {
		buffer := jsonMarshalWriteBuffer(p.Info, stmts[i])
		if buffer == nil || uses[buffer] != 2 {
			continue
		}
		encoder, value := fieldsLocal(p.Info, stmts[i+1])
		call, ok := ast.Unparen(value).(*ast.CallExpr)
		if encoder == nil || !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			continue
		}
		fn, ok := calleeObject(p.Info, call).(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "encoding/json" || fn.Name() != "NewEncoder" {
			continue
		}
		address, ok := ast.Unparen(call.Args[0]).(*ast.UnaryExpr)
		if !ok || address.Op != token.AND || jsonMarshalWriteObject(p.Info, address.X) != buffer {
			continue
		}
		rest := stmts[i+2:]
		escape := "true"
		evaluation := ""
		encoderUses := 1
		if len(rest) == 3 {
			stmt, ok := rest[0].(*ast.ExprStmt)
			if !ok || jsonMarshalWriteMethod(p.Info, stmt.X, encoder, "SetEscapeHTML", 1) == nil {
				continue
			}
			escape = "savedEscapeHTML"
			evaluation = " evaluate and save SetEscapeHTML's argument before evaluating Encode's argument;"
			encoderUses++
			rest = rest[1:]
		}
		if len(rest) != 2 || uses[encoder] != encoderUses {
			continue
		}
		failure := jsonMarshalWriteGuard(p.Info, rest[0], encoder, uses)
		if failure == nil || !jsonMarshalWriteReturn(p.Info, rest[1], buffer, len(failure.Results)) {
			continue
		}
		p.Reportf(call, "consider jsonv2.MarshalWrite with json.DefaultOptionsV1() followed by jsontext.EscapeForHTML(%s) to omit the newline;%s keep the local buffer and error returns, discard partial JSON on error, and verify custom-marshaler behavior and call order before changing", escape, evaluation)
	}
}

func jsonMarshalWriteBuffer(info *types.Info, stmt ast.Stmt) types.Object {
	obj, value := fieldsLocal(info, stmt)
	if obj == nil {
		decl, ok := stmt.(*ast.DeclStmt)
		if !ok {
			return nil
		}
		gen, ok := decl.Decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
			return nil
		}
		spec, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 0 || spec.Names[0].Name == "_" {
			return nil
		}
		obj = info.Defs[spec.Names[0]]
	} else {
		literal, ok := ast.Unparen(value).(*ast.CompositeLit)
		if !ok || len(literal.Elts) != 0 {
			return nil
		}
	}
	if obj == nil {
		return nil
	}
	named, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "bytes" || named.Obj().Name() != "Buffer" {
		return nil
	}
	return obj
}

func jsonMarshalWriteMethod(info *types.Info, expr ast.Expr, receiver types.Object, name string, arity int) *ast.CallExpr {
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != arity || call.Ellipsis.IsValid() {
		return nil
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name || jsonMarshalWriteObject(info, sel.X) != receiver {
		return nil
	}
	selection := info.Selections[sel]
	if selection == nil || selection.Kind() != types.MethodVal {
		return nil
	}
	return call
}

func jsonMarshalWriteGuard(info *types.Info, stmt ast.Stmt, encoder types.Object, uses map[types.Object]int) *ast.ReturnStmt {
	guard, ok := stmt.(*ast.IfStmt)
	if !ok || guard.Else != nil || len(guard.Body.List) != 1 {
		return nil
	}
	err, value := fieldsLocal(info, guard.Init)
	if err == nil || uses[err] != 2 || jsonMarshalWriteMethod(info, value, encoder, "Encode", 1) == nil {
		return nil
	}
	cond, ok := ast.Unparen(guard.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ {
		return nil
	}
	left, right := cond.X, cond.Y
	if info.Types[left].IsNil() {
		left, right = right, left
	}
	if jsonMarshalWriteObject(info, left) != err || !info.Types[right].IsNil() {
		return nil
	}
	ret, ok := guard.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) < 2 || jsonMarshalWriteObject(info, ret.Results[len(ret.Results)-1]) != err {
		return nil
	}
	for _, result := range ret.Results[:len(ret.Results)-1] {
		if tv := info.Types[result]; tv.Value == nil && !tv.IsNil() {
			return nil
		}
	}
	return ret
}

func jsonMarshalWriteReturn(info *types.Info, stmt ast.Stmt, buffer types.Object, arity int) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != arity || !info.Types[ret.Results[arity-1]].IsNil() {
		return false
	}
	found := false
	for _, result := range ret.Results[:arity-1] {
		if tv := info.Types[result]; tv.Value != nil || tv.IsNil() {
			continue
		}
		call := splitTrimStringsCall(info, result, "TrimSuffix")
		if found || call == nil || jsonMarshalWriteMethod(info, call.Args[0], buffer, "String", 0) == nil {
			return false
		}
		suffix := info.Types[call.Args[1]].Value
		if suffix == nil || suffix.Kind() != constant.String || constant.StringVal(suffix) != "\n" {
			return false
		}
		found = true
	}
	return found
}

func jsonMarshalWriteObject(info *types.Info, expr ast.Expr) types.Object {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	return info.Uses[id]
}
