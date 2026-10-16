package cops

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintStdlibUUID leaves UUIDv5, custom random sources, and legacy parsers alone.
// Randomness checks include dependencies and tests; literal parsing is independent.
func NewLintStdlibUUID(opts ...cop.FuncOption) *prog.Func {
	fileCop := newStdlibUUIDFile(opts...)
	return &prog.Func{
		Meta: fileCop.Meta,
		Run: func(p *prog.Pass) {
			candidates := &prog.Pass{Cop: p.Cop, Program: p.Program}
			var hasRandom bool
			candidateCop := *fileCop
			candidateCop.Run = func(p *cop.Pass) {
				hasRandom = checkStdlibUUID(p, true) || hasRandom
			}
			prog.FromFile(&candidateCop).Check(candidates)
			if len(candidates.Offenses()) == 0 {
				return
			}
			if hasRandom {
				pkgs, err := loadUUIDPackages(p)
				if err != nil {
					p.Reportf(token.NoPos, "cannot check UUID randomness: %v", err)
				}
				if err != nil || uuidRandomnessConfigured(pkgs) {
					// Parsing canonical literals does not depend on the random source.
					literals := *fileCop
					literals.Run = func(p *cop.Pass) { checkStdlibUUID(p, false) }
					prog.FromFile(&literals).Check(p)
					return
				}
			}
			for _, offense := range candidates.Offenses() {
				p.ReportOffense(offense)
			}
		},
	}
}

func newStdlibUUIDFile(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/StdlibUUID",
			Description: "Prefer standard-library UUIDs for compatible string-producing operations.",
			Severity:    cop.Warning,
		},
		MinStdlibVersion: "go1.27",
		Types:            true,
		Run:              func(p *cop.Pass) { checkStdlibUUID(p, true) },
	}, opts...)
}

// The result records random candidates so literal-only checks need no dependency load.
func checkStdlibUUID(p *cop.Pass, randomSafe bool) (hasRandom bool) {
	if p.Info == nil || ast.IsGenerated(p.File) {
		return false
	}
	ast.PreorderStack(p.File, nil, func(node ast.Node, stack []ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := calleeObject(p.Info, call).(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "github.com/google/uuid" {
			return true
		}
		switch fn.Name() {
		case "NewString":
			if !randomSafe {
				return true
			}
			parent := len(stack) - 1
			for parent >= 0 {
				if _, ok := stack[parent].(*ast.ParenExpr); !ok {
					break
				}
				parent--
			}
			if parent >= 0 {
				switch stack[parent].(type) {
				case *ast.GoStmt, *ast.DeferStmt:
					return true // NewV4 would generate the UUID before the deferred or async call.
				}
			}
			hasRandom = true
			p.Report(call, "prefer uuid.NewV4().String() from the standard library unless custom Google UUID randomness is required; do not change configured random sources")
		case "String":
			selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return true
			}
			inner, ok := ast.Unparen(selector.X).(*ast.CallExpr)
			if !ok {
				return true
			}
			source, ok := calleeObject(p.Info, inner).(*types.Func)
			if !ok || source.Pkg() == nil || source.Pkg().Path() != "github.com/google/uuid" {
				return true
			}
			switch source.Name() {
			case "New":
				if !randomSafe {
					return true
				}
				hasRandom = true
				p.Report(call, "prefer uuid.NewV4().String() from the standard library; preserve random UUIDv4 string semantics")
			case "MustParse":
				if len(inner.Args) != 1 {
					return true
				}
				value := p.Info.Types[inner.Args[0]].Value
				if value != nil && value.Kind() == constant.String && canonicalUUID(constant.StringVal(value)) {
					p.Report(call, "prefer standard-library uuid.MustParse(literal).String() for this canonical literal; retain legacy compatibility parsers and Google UUIDv5 consumers")
				}
			}
		}
		return true
	})
	return hasRandom
}

// UUID randomness can be configured by imported packages and by test-only imports.
// Keep this heavier dependency load local to the UUID cop.
func loadUUIDPackages(p *prog.Pass) ([]*packages.Package, error) {
	var dirs []string
	seen := make(map[string]bool)
	for _, pkg := range p.Program.Packages {
		if pkg.Dir != "" && !seen[pkg.Dir] {
			dirs = append(dirs, pkg.Dir)
			seen[pkg.Dir] = true
		}
	}
	if len(dirs) == 0 {
		return nil, nil
	}
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedSyntax,
		Dir: dirs[0], Fset: p.Program.Fset, Tests: true,
	}, dirs...)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %v", dirs)
	}
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		if err == nil && len(pkg.Errors) != 0 {
			err = fmt.Errorf("loading %s: %s", pkg.PkgPath, pkg.Errors[0].Msg)
		}
	})
	return pkgs, err
}

func uuidRandomnessConfigured(pkgs []*packages.Package) bool {
	configured := false
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		if configured {
			return false
		}
		if pkg.TypesInfo == nil {
			return true
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				// The upstream configuration functions may delegate to one another.
				// Their definitions alone are not evidence that a caller uses them.
				inConfiguration := false
				if fn, ok := decl.(*ast.FuncDecl); ok {
					inConfiguration = uuidConfigurationObject(pkg.TypesInfo.Defs[fn.Name])
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					if configured {
						return false
					}
					switch n := n.(type) {
					case *ast.Ident:
						configured = !inConfiguration && uuidConfigurationObject(pkg.TypesInfo.Uses[n])
					case *ast.AssignStmt:
						for _, lhs := range n.Lhs {
							if uuidRandReader(pkg.TypesInfo, lhs) {
								configured = true
							}
						}
					case *ast.RangeStmt:
						configured = uuidRandReader(pkg.TypesInfo, n.Key) || uuidRandReader(pkg.TypesInfo, n.Value)
					case *ast.UnaryExpr:
						configured = n.Op == token.AND && uuidRandReader(pkg.TypesInfo, n.X)
					}
					return !configured
				})
			}
		}
		return !configured
	}, nil)
	return configured
}

func uuidConfigurationObject(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "github.com/google/uuid" || fn.Signature().Recv() != nil {
		return false
	}
	switch fn.Name() {
	case "SetRand", "EnableRandPool", "DisableRandPool":
		return true
	}
	return false
}

func uuidRandReader(info *types.Info, expr ast.Expr) bool {
	var obj types.Object
	switch expr := ast.Unparen(expr).(type) {
	case *ast.Ident:
		obj = info.Uses[expr]
	case *ast.SelectorExpr:
		obj = info.Uses[expr.Sel]
	}
	v, ok := obj.(*types.Var)
	return ok && v.Pkg() != nil && v.Pkg().Path() == "crypto/rand" && v.Pkg().Scope().Lookup("Reader") == v
}

func canonicalUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, ch := range s {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}
