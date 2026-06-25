// Package prog adds whole-program, inter-procedural analysis to rubocop-go.
//
// The core [cop.Pass] gives a cop one file at a time, which is enough for
// the many syntactic and per-package rules that ship in cops/. Some
// questions, however, are fundamentally whole-program and inter-procedural:
// "does every context.Context consumed anywhere in the program derive from
// the single root context?" cannot be answered by looking at one file, or
// even one package, in isolation — the value may be threaded through
// parameters, returns, and several packages before it is used.
//
// prog provides the substrate for those rules:
//
//   - [Load] type-checks the whole program with go/packages and lowers it
//     to SSA (go/ssa), then builds a call graph (CHA).
//   - [Program.Origins] walks SSA def-use chains backwards, crossing
//     function boundaries via the call graph, to compute the set of
//     "source" values that flow into a given value.
//   - [Cop] / [Pass] mirror cop.Cop / cop.Pass for rules that need the
//     whole program rather than a single file.
//
// Keeping this in a separate package means the lightweight cop package
// stays free of the heavyweight go/ssa + go/packages dependencies; only
// programs that actually run whole-program cops pull them in.
package prog

import (
	"fmt"
	"go/token"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Program is a whole-program view of the code under analysis: the loaded
// packages with full type information, their SSA form, and a call graph.
//
// A Program is expensive to build (it type-checks every dependency and
// lowers the initial packages to SSA), so the runner builds it once and
// shares it across every whole-program cop.
type Program struct {
	// Fset is the shared position table for every loaded file.
	Fset *token.FileSet
	// Packages are the initial packages named by the load patterns, in
	// the order go/packages returned them.
	Packages []*packages.Package
	// SSA is the whole-program SSA, already built.
	SSA *ssa.Program
	// SSAPackages are the SSA packages corresponding 1:1 with Packages
	// (nil entries for packages that failed to lower).
	SSAPackages []*ssa.Package
	// CallGraph is the Class Hierarchy Analysis call graph over SSA. It is
	// sound (no real edge is missing) but may contain spurious edges for
	// interface calls; inter-procedural tracers must tolerate that.
	CallGraph *callgraph.Graph
}

// loadMode requests everything a whole-program analysis needs: names,
// files, imports of every dependency, full type information, and
// type-annotated syntax.
const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedModule

// Load type-checks the packages named by patterns (the same patterns the
// go tool accepts, e.g. "./...") and lowers them to SSA with a call graph.
//
// Load tolerates partial failures the way the rest of rubocop-go does: a
// package that does not type-check contributes whatever information was
// recovered. Load only returns an error when nothing at all could be
// loaded, since a cop has nothing to inspect in that case.
func Load(patterns ...string) (*Program, error) {
	return LoadDir("", patterns...)
}

// LoadDir is like [Load] but runs the go/packages query in dir (empty means
// the current working directory). Use it to load a program that lives
// outside the process working directory — e.g. a synthesized module in a
// test's temp directory — without mutating global process state.
func LoadDir(dir string, patterns ...string) (*Program, error) {
	if len(patterns) == 0 {
		patterns = []string{"."}
	}

	cfg := &packages.Config{Mode: loadMode, Dir: dir, Fset: token.NewFileSet()}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %v", patterns)
	}

	// Lower every initial package and its dependencies to SSA. SanityCheck
	// is off for speed; we trust go/ssa's output.
	ssaProg, ssaPkgs := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	if ssaProg == nil {
		return nil, fmt.Errorf("building SSA: no buildable packages in %v", patterns)
	}
	ssaProg.Build()

	return &Program{
		Fset:        cfg.Fset,
		Packages:    pkgs,
		SSA:         ssaProg,
		SSAPackages: ssaPkgs,
		CallGraph:   cha.CallGraph(ssaProg),
	}, nil
}

// HasErrors reports whether any loaded package had load or type errors.
// Whole-program cops may use this to soften their conclusions on code that
// did not fully type-check.
func (p *Program) HasErrors() bool {
	bad := false
	packages.Visit(p.Packages, nil, func(pkg *packages.Package) {
		if len(pkg.Errors) > 0 {
			bad = true
		}
	})
	return bad
}
