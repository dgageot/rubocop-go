package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintStdlibUUID leaves UUIDv5, custom random sources, and legacy parsers alone.
func NewLintStdlibUUID(opts ...cop.FuncOption) *prog.Func {
	fileCop := newStdlibUUIDFile(opts...)
	return &prog.Func{
		Meta: fileCop.Meta,
		Run: func(p *prog.Pass) {
			candidates := &prog.Pass{Cop: p.Cop, Program: p.Program}
			prog.FromFile(fileCop).Check(candidates)
			if len(candidates.Offenses()) == 0 {
				return
			}
			pkgs, err := loadUUIDRandomnessPackages(p)
			if err != nil {
				p.Reportf(token.NoPos, "cannot check test UUID randomness: %v", err)
				return
			}
			configured := false
			packages.Visit(pkgs, func(pkg *packages.Package) bool {
				if len(pkg.Errors) != 0 {
					configured = true
					return false
				}
				if pkg.TypesInfo != nil {
					for _, obj := range pkg.TypesInfo.Uses {
						if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "github.com/google/uuid" {
							switch obj.Name() {
							case "SetRand", "EnableRandPool", "DisableRandPool":
								configured = true
							}
						}
					}
				}
				return true
			}, nil)
			if configured {
				return
			}
			prog.FromFile(fileCop).Check(p)
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
		Run: func(p *cop.Pass) {
			if p.Info == nil || ast.IsGenerated(p.File) {
				return
			}
			delayed := make(map[*ast.CallExpr]bool)
			ast.Inspect(p.File, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.DeferStmt:
					delayed[node.Call] = true
				case *ast.GoStmt:
					delayed[node.Call] = true
				}
				return true
			})
			p.ForEachCall(func(call *ast.CallExpr) {
				fn, ok := calleeObject(p.Info, call).(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "github.com/google/uuid" {
					return
				}
				switch fn.Name() {
				case "NewString":
					if delayed[call] {
						return
					}
					p.Report(call, "prefer uuid.NewV4().String() from the standard library unless custom Google UUID randomness is required; do not change configured random sources")
				case "String":
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return
					}
					inner, ok := ast.Unparen(selector.X).(*ast.CallExpr)
					if !ok {
						return
					}
					source, ok := calleeObject(p.Info, inner).(*types.Func)
					if !ok || source.Pkg() == nil || source.Pkg().Path() != "github.com/google/uuid" {
						return
					}
					switch source.Name() {
					case "New":
						p.Report(call, "prefer uuid.NewV4().String() from the standard library; preserve random UUIDv4 string semantics")
					case "MustParse":
						if len(inner.Args) != 1 {
							return
						}
						value := p.Info.Types[inner.Args[0]].Value
						if value != nil && value.Kind() == constant.String && canonicalUUID(constant.StringVal(value)) {
							p.Report(call, "prefer standard-library uuid.MustParse(literal).String() for this canonical literal; retain legacy compatibility parsers and Google UUIDv5 consumers")
						}
					}
				}
			})
		},
	}, opts...)
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

// Dependencies can install process-wide randomness from their init functions.
func loadUUIDRandomnessPackages(p *prog.Pass) ([]*packages.Package, error) {
	var dirs []string
	seen := make(map[string]bool)
	for _, pkg := range p.Program.Packages {
		if pkg.Dir != "" && !seen[pkg.Dir] {
			dirs = append(dirs, pkg.Dir)
			seen[pkg.Dir] = true
		}
	}
	if len(dirs) == 0 {
		return p.Program.Packages, nil
	}
	return packages.Load(&packages.Config{
		Mode: packages.NeedModule | packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Dir:  dirs[0], Fset: p.Program.Fset, Tests: true,
	}, dirs...)
}
