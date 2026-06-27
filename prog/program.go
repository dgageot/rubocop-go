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
//     to SSA (go/ssa), then builds a call graph.
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
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// Program is a whole-program view of the code under analysis: the loaded
// packages with full type information, their SSA form, and a call graph.
//
// A Program is expensive to build (it type-checks the initial packages and
// lowers them to SSA), so the runner builds it once and shares it across
// every whole-program cop.
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
	// CallGraph is the static call graph over SSA. It includes interface
	// method calls when their concrete targets are present in the loaded
	// initial packages.
	CallGraph *callgraph.Graph
	// allFunctions caches [AllFunctions].
	allFunctions []*ssa.Function
}

// loadMode requests everything a whole-program analysis needs for the
// initial packages: names, files, imports, full type information, and
// type-annotated syntax.
const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
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

	// Lower only the initial packages to SSA. Whole-program cops operate on
	// project code; dependencies remain available for type information but
	// don't need SSA bodies or call-graph nodes.
	ssaProg := ssa.NewProgram(cfg.Fset, ssa.InstantiateGenerics)
	created := map[*types.Package]bool{}
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		if pkg.Types == nil || created[pkg.Types] {
			return false
		}
		created[pkg.Types] = true
		if isInitialPackage(pkgs, pkg) {
			ssaProg.CreatePackage(pkg.Types, pkg.Syntax, pkg.TypesInfo, true)
		} else {
			ssaProg.CreatePackage(pkg.Types, nil, nil, true)
		}
		return true
	}, nil)

	ssaPkgs := make([]*ssa.Package, len(pkgs))
	for i, pkg := range pkgs {
		ssaPkgs[i] = ssaProg.Package(pkg.Types)
	}
	if len(ssaPkgs) == 0 {
		return nil, fmt.Errorf("building SSA: no buildable packages in %v", patterns)
	}
	for _, ssaPkg := range ssaPkgs {
		if ssaPkg != nil {
			ssaPkg.Build()
		}
	}

	ssaFns := sourceFunctions(ssaPkgs)
	return &Program{
		Fset:         cfg.Fset,
		Packages:     pkgs,
		SSA:          ssaProg,
		SSAPackages:  ssaPkgs,
		CallGraph:    buildCallGraph(ssaFns),
		allFunctions: ssaFns,
	}, nil
}

func buildCallGraph(fns []*ssa.Function) *callgraph.Graph {
	graph := callgraph.New(nil)
	nodes := make(map[*ssa.Function]*callgraph.Node, len(fns))
	nodeFor := func(fn *ssa.Function) *callgraph.Node {
		if node := nodes[fn]; node != nil {
			return node
		}
		node := graph.CreateNode(fn)
		nodes[fn] = node
		return node
	}
	methodIndex := indexMethods(fns)
	for _, fn := range fns {
		caller := nodeFor(fn)
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				site, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				common := site.Common()
				if callee := common.StaticCallee(); callee != nil {
					callgraph.AddEdge(caller, site, nodeFor(callee))
					continue
				}
				if !common.IsInvoke() || common.Method == nil {
					continue
				}
				for _, callee := range methodIndex[methodKey(common.Method.Name(), common.Signature())] {
					if implementsInvokeReceiver(callee, common) {
						callgraph.AddEdge(caller, site, nodeFor(callee))
					}
				}
			}
		}
	}
	return graph
}

func indexMethods(fns []*ssa.Function) map[string][]*ssa.Function {
	index := map[string][]*ssa.Function{}
	for _, fn := range fns {
		if fn.Signature != nil && fn.Signature.Recv() != nil {
			index[methodKey(fn.Name(), fn.Signature)] = append(index[methodKey(fn.Name(), fn.Signature)], fn)
		}
	}
	return index
}

func implementsInvokeReceiver(callee *ssa.Function, common *ssa.CallCommon) bool {
	if callee.Signature == nil || callee.Signature.Recv() == nil || common.Value == nil {
		return false
	}
	iface, ok := common.Value.Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return types.Implements(callee.Signature.Recv().Type(), iface)
}

func methodKey(name string, sig *types.Signature) string {
	if sig == nil {
		return name
	}
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('(')
	writeTuple(&b, sig.Params())
	if sig.Variadic() {
		b.WriteString("...")
	}
	b.WriteString(")(")
	writeTuple(&b, sig.Results())
	b.WriteByte(')')
	return b.String()
}

func writeTuple(b *strings.Builder, tuple *types.Tuple) {
	for i := range tuple.Len() {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(types.TypeString(tuple.At(i).Type(), nil))
	}
}

func sourceFunctions(pkgs []*ssa.Package) []*ssa.Function {
	seen := map[*ssa.Function]bool{}
	for _, pkg := range pkgs {
		if pkg == nil {
			continue
		}
		for _, member := range pkg.Members {
			if fn, ok := member.(*ssa.Function); ok {
				collectFunctions(fn, seen)
			}
		}
	}
	fns := make([]*ssa.Function, 0, len(seen))
	for fn := range seen {
		if fn.Blocks != nil {
			fns = append(fns, fn)
		}
	}
	sortFunctions(fns)
	return fns
}

func isInitialPackage(initial []*packages.Package, pkg *packages.Package) bool {
	return slices.Contains(initial, pkg)
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
