package cops

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"strings"

	"golang.org/x/tools/go/cfg"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintStreamCloseSafety compares Close with Next/Recv and their synchronous helpers.
// It checks receiver-rooted field slots, including promoted fields and wrappers,
// using must-held mutexes at each CFG node. Atomic method calls aren't plain
// field writes. It doesn't prove arbitrary aliasing, I/O cancellation, or that
// an external method is safe; those still need race and lifecycle tests.
// Helpers and goroutines can invalidate locks but cannot establish caller locks.
// Deferred calls are conservative; container elements and pointer dereferences
// are outside this field-slot analysis.
func NewLintStreamCloseSafety() *prog.Func {
	return &prog.Func{
		Meta: cop.Meta{
			Name:        "Lint/StreamCloseSafety",
			Description: "Flag potentially unsynchronized field access between Close and Next/Recv.",
			Severity:    cop.Error,
		},
		Run: func(p *prog.Pass) {
			a := streamCloseAnalysis{pass: p, methods: make(map[*types.Func]streamMethod)}
			for _, pkg := range p.Program.Packages {
				for _, file := range pkg.Syntax {
					if strings.HasSuffix(p.Program.Fset.Position(file.Pos()).Filename, "_test.go") {
						continue
					}
					for _, decl := range file.Decls {
						fn, ok := decl.(*ast.FuncDecl)
						if !ok || fn.Recv == nil || fn.Body == nil || len(fn.Recv.List[0].Names) == 0 {
							continue
						}
						obj, ok := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
						if ok {
							a.methods[obj.Origin()] = streamMethod{fn: fn, info: pkg.TypesInfo}
						}
					}
				}
			}
			// Walk declarations in source order so diagnostics are reproducible.
			for _, pkg := range p.Program.Packages {
				for _, file := range pkg.Syntax {
					for _, decl := range file.Decls {
						fn, ok := decl.(*ast.FuncDecl)
						if !ok || fn.Name.Name != "Close" {
							continue
						}
						obj, ok := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
						if ok && obj.Signature().Recv() != nil {
							a.check(obj)
						}
					}
				}
			}
		},
	}
}

type streamMethod struct {
	fn   *ast.FuncDecl
	info *types.Info
}

type streamFieldAccess struct {
	pos   token.Pos
	path  string
	write bool
	locks map[string]bool // true means exclusive; false means read lock.
}

type streamCloseAnalysis struct {
	pass        *prog.Pass
	methods     map[*types.Func]streamMethod
	graphs      map[*ast.BlockStmt]*cfg.CFG
	unlocks     map[*types.Func]map[string]bool // Immutable, receiver-relative summaries.
	uncacheable uint64
}

func (a *streamCloseAnalysis) graph(body *ast.BlockStmt) *cfg.CFG {
	if graph := a.graphs[body]; graph != nil {
		return graph
	}
	if a.graphs == nil {
		a.graphs = make(map[*ast.BlockStmt]*cfg.CFG)
	}
	graph := cfg.New(body, func(*ast.CallExpr) bool { return true })
	a.graphs[body] = graph
	return graph
}

func (a *streamCloseAnalysis) check(closeMethod *types.Func) {
	t := closeMethod.Signature().Recv().Type()
	var readers []streamFieldAccess
	for _, name := range []string{"Next", "Recv"} {
		obj, indices, _ := types.LookupFieldOrMethod(t, true, closeMethod.Pkg(), name)
		if reader, ok := obj.(*types.Func); ok {
			prefix, valid := streamFieldPath("", t, indices[:len(indices)-1])
			if valid {
				found, _ := a.accesses(reader, prefix, nil, make(map[*types.Func]bool), true)
				readers = append(readers, found...)
			}
		}
	}
	if len(readers) == 0 {
		return
	}
	seen := make(map[token.Pos]bool)
	closedAccesses, _ := a.accesses(closeMethod, "", nil, make(map[*types.Func]bool), true)
	for _, closed := range closedAccesses {
		for _, read := range readers {
			if closed.path != read.path || (!closed.write && !read.write) || streamAccessesLocked(closed, read) || seen[closed.pos] {
				continue
			}
			seen[closed.pos] = true
			a.pass.Reportf(closed.pos, "Close accesses %s without a common mutex with Next/Recv (%s); use synchronized state or keep it reader-owned", strings.TrimPrefix(closed.path, "."), a.pass.Program.Fset.Position(read.pos))
			break
		}
	}
}

func streamAccessesLocked(a, b streamFieldAccess) bool {
	for name, exclusiveA := range a.locks {
		if exclusiveB, ok := b.locks[name]; ok && (!a.write || exclusiveA) && (!b.write || exclusiveB) {
			return true
		}
	}
	return false
}

func (a *streamCloseAnalysis) accesses(fn *types.Func, prefix string, locks map[string]bool, visiting map[*types.Func]bool, collect bool) ([]streamFieldAccess, map[string]bool) {
	fn = fn.Origin()
	method, ok := a.methods[fn]
	if !ok {
		return nil, nil
	}
	if visiting[fn] {
		// Recursive effects depend on inherited locks and cannot be cached.
		a.uncacheable++
		return nil, maps.Clone(locks)
	}
	if !collect {
		if summary, cached := a.unlocks[fn]; cached {
			var unlocked map[string]bool
			for path := range summary {
				if unlocked == nil {
					unlocked = make(map[string]bool)
				}
				unlocked[prefix+path] = true
			}
			return nil, unlocked
		}
	}
	uncacheable := a.uncacheable
	visiting[fn] = true
	defer delete(visiting, fn)
	scan := streamMethodScan{
		analysis: a, method: method, prefix: prefix, visiting: visiting,
		receiver: method.info.ObjectOf(method.fn.Recv.List[0].Names[0]),
	}
	scan.body(method.fn.Body, locks, collect)
	// Recursion depends on inherited locks; callbacks add collect-only call edges.
	// Cache neither case, nor accesses with their caller-specific lock state.
	if !collect && a.uncacheable == uncacheable {
		if a.unlocks == nil {
			a.unlocks = make(map[*types.Func]map[string]bool)
		}
		var summary map[string]bool
		for path := range scan.unlocked {
			if summary == nil {
				summary = make(map[string]bool)
			}
			summary[strings.TrimPrefix(path, prefix)] = true
		}
		a.unlocks[fn] = summary
	}
	return scan.found, scan.unlocked
}

type streamMethodScan struct {
	analysis *streamCloseAnalysis
	method   streamMethod
	receiver types.Object
	prefix   string
	visiting map[*types.Func]bool
	found    []streamFieldAccess
	unlocked map[string]bool
}

func (s *streamMethodScan) body(body *ast.BlockStmt, initial map[string]bool, collect bool) {
	graph := s.analysis.graph(body)
	states := map[*cfg.Block]map[string]bool{graph.Blocks[0]: maps.Clone(initial)}
	queue := []*cfg.Block{graph.Blocks[0]}
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		locks := maps.Clone(states[block])
		for _, node := range block.Nodes {
			locks = s.inspect(node, locks, false)
		}
		for _, next := range block.Succs {
			prior, exists := states[next]
			if !exists {
				states[next] = maps.Clone(locks)
				queue = append(queue, next)
				continue
			}
			merged := maps.Clone(prior)
			for path, exclusive := range merged {
				if other, ok := locks[path]; !ok {
					delete(merged, path)
				} else {
					merged[path] = exclusive && other
				}
			}
			if !maps.Equal(prior, merged) {
				states[next] = merged
				queue = append(queue, next)
			}
		}
	}
	if !collect {
		return
	}
	for _, block := range graph.Blocks {
		locks, reachable := states[block]
		if !reachable {
			continue
		}
		locks = maps.Clone(locks)
		for _, node := range block.Nodes {
			locks = s.inspect(node, locks, true)
		}
	}
}

type streamCallMode uint8

const (
	streamCallSync streamCallMode = iota
	streamCallDeferred
	streamCallAsync
)

func (s *streamMethodScan) lockOp(call *ast.CallExpr, locks map[string]bool, mode streamCallMode) map[string]bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return locks
	}
	path, ok := s.path(sel.X)
	selection := s.method.info.Selections[sel]
	if !ok || selection == nil {
		return locks
	}
	fn, ok := selection.Obj().(*types.Func)
	if !ok || !streamMutexType(fn.Signature().Recv().Type()) {
		return locks
	}
	indices := selection.Index()
	path, ok = streamFieldPath(path, selection.Recv(), indices[:len(indices)-1])
	if !ok {
		return locks
	}
	if locks == nil && mode == streamCallSync {
		locks = make(map[string]bool)
	}
	switch sel.Sel.Name {
	case "Lock", "RLock":
		if mode == streamCallSync {
			locks[path] = sel.Sel.Name == "Lock"
		}
	case "Unlock", "RUnlock":
		if s.unlocked == nil {
			s.unlocked = make(map[string]bool)
		}
		s.unlocked[path] = true
		if mode != streamCallDeferred {
			delete(locks, path)
		}
	}
	return locks
}

func streamMutexType(t types.Type) bool {
	t = types.Unalias(t)
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "sync" && (named.Obj().Name() == "Mutex" || named.Obj().Name() == "RWMutex")
}

func (s *streamMethodScan) path(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		return s.prefix, s.method.info.ObjectOf(e) == s.receiver
	case *ast.ParenExpr:
		return s.path(e.X)
	case *ast.SelectorExpr:
		prefix, ok := s.path(e.X)
		selection := s.method.info.Selections[e]
		if !ok || selection == nil || selection.Kind() != types.FieldVal {
			return "", false
		}
		return streamFieldPath(prefix, selection.Recv(), selection.Index())
	default:
		return "", false
	}
}

func (s *streamMethodScan) inspect(node ast.Node, locks map[string]bool, collect bool) map[string]bool {
	ast.Inspect(node, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.DeferStmt:
			// The call's arguments run now; its body runs at this method's return.
			locks = s.call(v.Call, locks, collect, streamCallDeferred)
			return false
		case *ast.GoStmt:
			locks = s.call(v.Call, locks, collect, streamCallAsync)
			return false
		case *ast.FuncLit:
			// Inspect callbacks, but their effects aren't synchronous caller effects.
			if collect {
				s.literal(v, locks, true)
			} else {
				// A callback can re-enter an ancestor only during access collection.
				s.analysis.uncacheable++
			}
			return false
		case *ast.AssignStmt:
			for _, lhs := range v.Lhs {
				locks = s.inspect(lhs, locks, collect)
			}
			for _, rhs := range v.Rhs {
				locks = s.inspect(rhs, locks, collect)
			}
			if collect {
				for _, lhs := range v.Lhs {
					s.write(lhs, locks)
				}
			}
			return false
		case *ast.IncDecStmt:
			locks = s.inspect(v.X, locks, collect)
			if collect {
				s.write(v.X, locks)
			}
			return false
		case *ast.SelectorExpr:
			if collect {
				if path, ok := s.path(v); ok {
					s.found = append(s.found, streamFieldAccess{pos: v.Pos(), path: path, locks: maps.Clone(locks)})
				}
			}
		case *ast.CallExpr:
			locks = s.call(v, locks, collect, streamCallSync)
			return false
		}
		return true
	})
	return locks
}

func (s *streamMethodScan) literal(lit *ast.FuncLit, locks map[string]bool, collect bool) map[string]bool {
	nested := *s
	nested.found = nil
	nested.unlocked = nil
	nested.body(lit.Body, locks, collect)
	s.found = append(s.found, nested.found...)
	return nested.unlocked
}

func (s *streamMethodScan) call(call *ast.CallExpr, locks map[string]bool, collect bool, mode streamCallMode) map[string]bool {
	fun := ast.Unparen(call.Fun)
	lit, literal := fun.(*ast.FuncLit)
	if !literal {
		locks = s.inspect(call.Fun, locks, collect)
	}
	for _, arg := range call.Args {
		locks = s.inspect(arg, locks, collect)
	}
	inherited := locks
	if mode != streamCallSync {
		// Deferred and asynchronous bodies cannot rely on the caller's locks.
		inherited = nil
	}
	var unlocked map[string]bool
	if literal {
		unlocked = s.literal(lit, inherited, collect)
	} else if sel, ok := fun.(*ast.SelectorExpr); ok {
		prefix, valid := s.path(sel.X)
		selection := s.method.info.Selections[sel]
		if valid && selection != nil {
			if fn, ok := selection.Obj().(*types.Func); ok {
				if streamMutexType(fn.Signature().Recv().Type()) {
					return s.lockOp(call, locks, mode)
				}
				// A promoted method's receiver is the embedded field, not the wrapper.
				indices := selection.Index()
				if prefix, valid = streamFieldPath(prefix, selection.Recv(), indices[:len(indices)-1]); valid {
					var found []streamFieldAccess
					found, unlocked = s.analysis.accesses(fn, prefix, inherited, s.visiting, collect)
					s.found = append(s.found, found...)
				}
			}
		}
	}
	// Synchronous and asynchronous unlocks invalidate caller guarantees;
	// deferred unlocks affect only the summary until this method returns.
	for path := range unlocked {
		if s.unlocked == nil {
			s.unlocked = make(map[string]bool)
		}
		s.unlocked[path] = true
		if mode != streamCallDeferred {
			delete(locks, path)
		}
	}
	return locks
}

func (s *streamMethodScan) write(expr ast.Expr, locks map[string]bool) {
	if path, ok := s.path(expr); ok {
		s.found = append(s.found, streamFieldAccess{pos: expr.Pos(), path: path, write: true, locks: maps.Clone(locks)})
	}
}

// streamFieldPath expands embedded selections and pointer aliases to slot names.
func streamFieldPath(prefix string, t types.Type, indices []int) (string, bool) {
	var path strings.Builder
	path.WriteString(prefix)
	for _, index := range indices {
		t = types.Unalias(t)
		if ptr, ok := t.Underlying().(*types.Pointer); ok {
			t = types.Unalias(ptr.Elem())
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok || index >= st.NumFields() {
			return "", false
		}
		field := st.Field(index)
		path.WriteByte('.')
		path.WriteString(field.Name())
		t = field.Type()
	}
	return path.String(), true
}
