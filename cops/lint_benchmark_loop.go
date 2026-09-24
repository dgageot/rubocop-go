package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintBenchmarkLoop loads typed tests because the shared SSA program omits them.
func NewLintBenchmarkLoop(opts ...cop.FuncOption) *prog.Func {
	fileCop := newBenchmarkLoopFile(opts...)
	return &prog.Func{
		Meta: fileCop.Meta,
		Run: func(p *prog.Pass) {
			pkgs, err := loadModernizationTests(p)
			if err != nil {
				p.Reportf(token.NoPos, "cannot inspect benchmark tests: %v", err)
				return
			}
			seen := make(map[string]bool)
			for _, pkg := range pkgs {
				if pkg.IllTyped {
					continue
				}
				for _, file := range pkg.Syntax {
					filename := p.Program.Fset.Position(file.Pos()).Filename
					if seen[filename] || !strings.HasSuffix(filename, "_test.go") {
						continue
					}
					seen[filename] = true
					pass := &cop.Pass{Cop: fileCop, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
					if fileCop.InScope(pass) {
						fileCop.Check(pass)
					}
					for _, offense := range pass.Offenses() {
						p.ReportOffense(offense)
					}
				}
			}
		},
	}
}

func newBenchmarkLoopFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/BenchmarkLoop",
			Description: "Consider b.Loop for simple benchmark loops.",
			Severity:    cop.Warning,
		},
		MinStdlibVersion: "go1.24",
		Types:            true,
		Run: func(p *cop.Pass) {
			if p.Info == nil || !p.IsTestFile() || ast.IsGenerated(p.File) {
				return
			}
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.FuncDecl:
					suffix, benchmark := strings.CutPrefix(node.Name.Name, "Benchmark")
					first, _ := utf8.DecodeRuneInString(suffix)
					if node.Recv == nil && benchmark && !unicode.IsLower(first) {
						checkBenchmarkLoop(p, node.Type, node.Body)
					}
				case *ast.CallExpr:
					if len(node.Args) != 2 || node.Ellipsis.IsValid() {
						break
					}
					selector, ok := ast.Unparen(node.Fun).(*ast.SelectorExpr)
					if !ok || !benchmarkLoopMethod(p.Info, selector, "Run") {
						break
					}
					if callback, ok := ast.Unparen(node.Args[1]).(*ast.FuncLit); ok {
						checkBenchmarkLoop(p, callback.Type, callback.Body)
					}
				}
				return true
			})
		},
	}, opts...)
}

func checkBenchmarkLoop(p *cop.Pass, typ *ast.FuncType, body *ast.BlockStmt) {
	if body == nil || typ.Params == nil || len(typ.Params.List) != 1 || len(typ.Params.List[0].Names) != 1 || typ.Results != nil && len(typ.Results.List) != 0 {
		return
	}
	benchmark := p.Info.Defs[typ.Params.List[0].Names[0]]
	if benchmark == nil || !benchmarkLoopPointer(benchmark.Type()) {
		return
	}
	// Keep setup before ResetTimer and require the measured loop to finish the function.
	stmts := body.List
	if len(stmts) < 2 {
		return
	}
	loop := stmts[len(stmts)-1]
	bound, counter := benchmarkLoopBound(p.Info, loop, benchmark)
	if bound == nil {
		return
	}
	resetIndex := len(stmts) - 2
	for resetIndex >= 0 && benchmarkLoopCall(p.Info, stmts[resetIndex], benchmark, "ReportAllocs") != nil {
		resetIndex--
	}
	if resetIndex < 0 {
		return
	}
	reset := benchmarkLoopCall(p.Info, stmts[resetIndex], benchmark, "ResetTimer")
	if reset == nil {
		return
	}
	allowedSelectors := make(map[*ast.SelectorExpr]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if ok && (call == reset || call.Pos() < loop.Pos() && benchmarkLoopMethod(p.Info, selector, "ReportAllocs", "SetBytes", "Context", "TempDir")) {
			allowedSelectors[selector] = true
		}
		return true
	})
	valid := true
	ast.Inspect(body, func(n ast.Node) bool {
		if !valid {
			return false
		}
		switch node := n.(type) {
		case *ast.RangeStmt, *ast.ForStmt:
			if n != loop && n.Pos() >= reset.Pos() {
				valid = false
			}
		case *ast.AssignStmt:
			if node.Pos() > loop.Pos() {
				for _, lhs := range node.Lhs {
					if !benchmarkLoopLocalTarget(p.Info, lhs, loop) {
						valid = false // Accumulated results require a separate measurement review.
					}
				}
			}
		case *ast.IncDecStmt:
			if node.Pos() > loop.Pos() && !benchmarkLoopLocalTarget(p.Info, node.X, loop) {
				valid = false
			}
		case *ast.CallExpr:
			obj := calleeObject(p.Info, node)
			if obj == types.Universe.Lookup("panic") || obj != nil && obj.Pkg() != nil && (obj.Pkg().Path() == "runtime" && obj.Name() == "Goexit" || obj.Pkg().Path() == "os" && obj.Name() == "Exit") {
				valid = false
			}
		case *ast.FuncLit, *ast.BranchStmt, *ast.ReturnStmt, *ast.GoStmt, *ast.DeferStmt:
			// Escaping or deferred work can alter timing or prevent Loop's final false call.
			valid = false
		case *ast.SelectorExpr:
			id, ok := ast.Unparen(node.X).(*ast.Ident)
			if !ok || p.Info.Uses[id] != benchmark {
				break
			}
			if node == bound {
				return false
			}
			// Every benchmark use must be a direct, known method call.
			if !allowedSelectors[node] {
				valid = false
			}
			return false
		case *ast.Ident:
			if p.Info.Uses[node] == benchmark {
				valid = false // An alias or helper could manipulate the timer or b.N.
			}
		}
		return valid
	})
	if !valid {
		return
	}
	if counter != nil {
		uses := 0
		for _, obj := range p.Info.Uses {
			if obj == counter {
				uses++
			}
		}
		if uses != 2 { // The condition and increment are the counter's only uses.
			return
		}
	}
	p.Report(loop, "consider for b.Loop() instead of ResetTimer and a b.N loop; review benchmark measurement effects, setup lifetime, and compiler optimizations rather than applying a mechanical rewrite")
}

func benchmarkLoopLocalTarget(info *types.Info, expr ast.Expr, loop ast.Stmt) bool {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return false
	}
	if id.Name == "_" {
		return true
	}
	obj := info.ObjectOf(id)
	return obj != nil && obj.Pos() > loop.Pos() && obj.Pos() < loop.End()
}

func benchmarkLoopBound(info *types.Info, stmt ast.Stmt, benchmark types.Object) (*ast.SelectorExpr, types.Object) {
	var expr ast.Expr
	var counter types.Object
	switch loop := stmt.(type) {
	case *ast.RangeStmt:
		if loop.Value != nil || loop.Key != nil && !benchmarkLoopBlank(loop.Key) {
			return nil, nil
		}
		expr = loop.X
	case *ast.ForStmt:
		init, ok := loop.Init.(*ast.AssignStmt)
		if !ok || init.Tok != token.DEFINE || len(init.Lhs) != 1 || len(init.Rhs) != 1 {
			return nil, nil
		}
		id, ok := init.Lhs[0].(*ast.Ident)
		zero := info.Types[init.Rhs[0]].Value
		if !ok || zero == nil || zero.Kind() != constant.Int || constant.Sign(zero) != 0 {
			return nil, nil
		}
		counter = info.Defs[id]
		cond, ok := ast.Unparen(loop.Cond).(*ast.BinaryExpr)
		if counter == nil || !ok || cond.Op != token.LSS || benchmarkLoopObject(info, cond.X) != counter {
			return nil, nil
		}
		post, ok := loop.Post.(*ast.IncDecStmt)
		if !ok || post.Tok != token.INC || benchmarkLoopObject(info, post.X) != counter {
			return nil, nil
		}
		expr = cond.Y
	default:
		return nil, nil
	}
	selector, ok := ast.Unparen(expr).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "N" || benchmarkLoopObject(info, selector.X) != benchmark {
		return nil, nil
	}
	return selector, counter
}

func benchmarkLoopCall(info *types.Info, stmt ast.Stmt, benchmark types.Object, name string) *ast.CallExpr {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := ast.Unparen(expr.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 || call.Ellipsis.IsValid() {
		return nil
	}
	selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || benchmarkLoopObject(info, selector.X) != benchmark || !benchmarkLoopMethod(info, selector, name) {
		return nil
	}
	return call
}

func benchmarkLoopMethod(info *types.Info, selector *ast.SelectorExpr, names ...string) bool {
	selection := info.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal || !benchmarkLoopPointer(info.TypeOf(selector.X)) {
		return false
	}
	obj := selection.Obj()
	if obj.Pkg() == nil || obj.Pkg().Path() != "testing" {
		return false
	}
	for _, name := range names {
		if obj.Name() == name {
			return true
		}
	}
	return false
}

func benchmarkLoopPointer(t types.Type) bool {
	if t == nil {
		return false
	}
	ptr, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == "B"
}

func benchmarkLoopObject(info *types.Info, expr ast.Expr) types.Object {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	return info.Uses[id]
}

func benchmarkLoopBlank(expr ast.Expr) bool {
	id, ok := ast.Unparen(expr).(*ast.Ident)
	return ok && id.Name == "_"
}
