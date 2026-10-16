package cops

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/cfg"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// NewLintStreamCloseSafety compares Close with Next/Recv and their synchronous helpers.
// It checks receiver-rooted field slots, including promoted fields and wrappers,
// using must-held mutexes at each CFG node. Atomic method calls aren't plain
// field writes. It doesn't prove arbitrary aliasing, I/O cancellation, or that
// an external method is safe; those still need race and lifecycle tests. Lock
// helpers and deferred bodies are conservative; their receivers and arguments
// are evaluated synchronously. Container elements and arbitrary pointer
// dereferences are outside this field-slot analysis.
func NewLintStreamCloseSafety() *prog.Func {
	return &prog.Func{
		Meta: cop.Meta{
			Name:        "Lint/StreamCloseSafety",
			Description: "Flag potentially unsynchronized field access between Close and Next/Recv.",
			Severity:    cop.Error,
		},
		Run: func(p *prog.Pass) {
			a := streamCloseAnalysis{pass: p, methods: make(map[*types.Func]streamMethod), reported: make(map[token.Pos]bool)}
			for _, pkg := range p.Program.Packages {
				for _, file := range pkg.Syntax {
					if strings.HasSuffix(p.Program.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
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
			// Include wrapper types whose Close method is promoted from an embedding.
			for _, pkg := range p.Program.Packages {
				for _, file := range pkg.Syntax {
					if strings.HasSuffix(p.Program.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
						continue
					}
					for _, decl := range file.Decls {
						gen, ok := decl.(*ast.GenDecl)
						if !ok || gen.Tok != token.TYPE {
							continue
						}
						for _, spec := range gen.Specs {
							name := pkg.TypesInfo.Defs[spec.(*ast.TypeSpec).Name].(*types.TypeName)
							if !name.IsAlias() {
								a.check(types.NewPointer(name.Type()), pkg.Types)
							}
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
	pos      token.Pos
	path     string
	typ      types.Type // Distinguishes a pointer slot from its pointee at the same path.
	write    bool
	atomic   bool
	indirect bool
	locks    map[string]bool // true means exclusive; false means read lock.
}

type streamCloseAnalysis struct {
	pass     *prog.Pass
	methods  map[*types.Func]streamMethod
	reported map[token.Pos]bool
}

func (a *streamCloseAnalysis) check(t types.Type, pkg *types.Package) {
	closeObj, indices, _ := types.LookupFieldOrMethod(t, true, pkg, "Close")
	closeMethod, ok := closeObj.(*types.Func)
	if !ok {
		return
	}
	closePrefix, ok := streamFieldPath("", t, indices[:len(indices)-1])
	if !ok {
		return
	}
	var readers []streamFieldAccess
	for _, name := range []string{"Next", "Recv"} {
		obj, indices, _ := types.LookupFieldOrMethod(t, true, pkg, name)
		if reader, ok := obj.(*types.Func); ok {
			prefix, valid := streamFieldPath("", t, indices[:len(indices)-1])
			if valid {
				readers = append(readers, a.entryAccesses(reader, t, indices[:len(indices)-1], prefix)...)
			}
		}
	}
	if len(readers) == 0 {
		return
	}
	for _, closed := range a.entryAccesses(closeMethod, t, indices[:len(indices)-1], closePrefix) {
		for _, read := range readers {
			if closed.path != read.path || !types.Identical(closed.typ, read.typ) || (closed.atomic && read.atomic) || (!closed.write && !read.write) || streamAccessesLocked(closed, read) || a.reported[closed.pos] {
				continue
			}
			a.reported[closed.pos] = true
			a.pass.Reportf(closed.pos, "Close accesses %s without a common mutex with Next/Recv (%s); use synchronized state or keep it reader-owned", strings.TrimPrefix(closed.path, "."), a.pass.Program.Fset.Position(read.pos))
			break
		}
	}
}

func (a *streamCloseAnalysis) entryAccesses(fn *types.Func, receiver types.Type, indices []int, prefix string) []streamFieldAccess {
	var found []streamFieldAccess
	// Promoted calls must first read the embedded pointer slots on the wrapper.
	for i := 1; i <= len(indices); i++ {
		t := streamFieldType(receiver, indices[:i])
		if _, pointer := t.Underlying().(*types.Pointer); pointer {
			path, ok := streamFieldPath("", receiver, indices[:i])
			if ok {
				found = streamAppendAccess(found, fn.Pos(), path, t, false, nil)
			}
		}
	}
	t := fn.Signature().Recv().Type()
	if _, pointer := t.Underlying().(*types.Pointer); !pointer {
		// The receiver copy precedes every lock acquired by the method body.
		found = streamAppendAccess(found, fn.Pos(), prefix, t, false, nil)
	}
	return append(found, a.accesses(fn, prefix, nil, make(map[*types.Func]bool))...)
}

func streamAccessesLocked(a, b streamFieldAccess) bool {
	for name, exclusiveA := range a.locks {
		if exclusiveB, ok := b.locks[name]; ok && (!a.write || exclusiveA) && (!b.write || exclusiveB) {
			return true
		}
	}
	return false
}

func (a *streamCloseAnalysis) accesses(fn *types.Func, prefix string, locks map[string]bool, visiting map[*types.Func]bool) []streamFieldAccess {
	origin := fn.Origin()
	method, ok := a.methods[origin]
	if !ok || visiting[origin] {
		return nil
	}
	visiting[origin] = true
	defer delete(visiting, origin)
	scan := streamMethodScan{
		analysis: a, method: method, prefix: prefix, visiting: visiting,
		receiver:     method.info.ObjectOf(method.fn.Recv.List[0].Names[0]),
		receiverType: fn.Signature().Recv().Type(),
	}
	scan.body(method.fn.Body, locks)
	// Reads and writes of copied slots are local; copying itself is checked at the call site.
	var found []streamFieldAccess
	for _, access := range scan.found {
		if !streamSharedPath(fn.Signature().Recv().Type(), prefix, access.path, access.indirect) {
			continue
		}
		for lock := range access.locks {
			if !streamSharedPath(fn.Signature().Recv().Type(), prefix, lock, true) {
				if inherited, ok := locks[lock]; ok {
					access.locks[lock] = inherited
				} else {
					delete(access.locks, lock)
				}
			}
		}
		found = append(found, access)
	}
	return found
}

type streamMethodScan struct {
	analysis     *streamCloseAnalysis
	method       streamMethod
	receiver     types.Object
	receiverType types.Type
	prefix       string
	visiting     map[*types.Func]bool
	found        []streamFieldAccess
	unordered    map[string]bool
}

func (s *streamMethodScan) body(body *ast.BlockStmt, initial map[string]bool) {
	// A called body is sequenced separately from its caller's ordinary loads.
	unordered := s.unordered
	s.unordered = nil
	defer func() { s.unordered = unordered }()
	graph := cfg.New(body, func(*ast.CallExpr) bool { return true })
	states := map[*cfg.Block]map[string]bool{graph.Blocks[0]: maps.Clone(initial)}
	queue := []*cfg.Block{graph.Blocks[0]}
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		locks := maps.Clone(states[block])
		for _, node := range block.Nodes {
			locks = s.eval(node, locks, false)
		}
		for _, next := range block.Succs {
			prior, exists := states[next]
			if !exists {
				states[next] = maps.Clone(locks)
				queue = append(queue, next)
				continue
			}
			merged := streamMergeLocks(prior, locks)
			if !maps.Equal(prior, merged) {
				states[next] = merged
				queue = append(queue, next)
			}
		}
	}
	for _, block := range graph.Blocks {
		locks, reachable := states[block]
		if !reachable {
			continue
		}
		locks = maps.Clone(locks)
		for _, node := range block.Nodes {
			locks = s.eval(node, locks, true)
		}
	}
}

// Helpers may release caller locks, but do not establish must-held acquisitions.
func (a *streamCloseAnalysis) releasedLocks(fn *types.Func, prefix string, visiting map[*types.Func]bool) map[string]bool {
	origin := fn.Origin()
	method, ok := a.methods[origin]
	if !ok || visiting[origin] {
		return nil
	}
	visiting[origin] = true
	defer delete(visiting, origin)
	scan := streamMethodScan{
		analysis: a, method: method, prefix: prefix,
		receiver:     method.info.ObjectOf(method.fn.Recv.List[0].Names[0]),
		receiverType: fn.Signature().Recv().Type(),
	}
	released := make(map[string]bool)
	scan.releaseCalls(method.fn.Body, released, visiting)
	for path := range released {
		if !streamSharedPath(fn.Signature().Recv().Type(), prefix, path, true) {
			delete(released, path)
		}
	}
	return released
}

func (s *streamMethodScan) releaseCalls(node ast.Node, released map[string]bool, visiting map[*types.Func]bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BinaryExpr:
			if v.Op == token.LAND || v.Op == token.LOR {
				s.releaseCalls(v.X, released, visiting)
				if value, known := s.boolean(v.X); !known || value == (v.Op == token.LAND) {
					s.releaseCalls(v.Y, released, visiting)
				}
				return false
			}
		case *ast.CallExpr:
			s.releaseCalls(v.Fun, released, visiting)
			for _, arg := range v.Args {
				s.releaseCalls(arg, released, visiting)
			}
			if literal := calledFuncLit(v.Fun); literal != nil {
				s.releaseCalls(literal.Body, released, visiting)
			} else if fn, path, ok := s.target(v.Fun); ok {
				if streamMutexType(fn.Signature().Recv().Type()) {
					if fn.Name() == "Unlock" || fn.Name() == "RUnlock" {
						released[path] = true
					}
				} else {
					maps.Copy(released, s.analysis.releasedLocks(fn, path, visiting))
				}
			}
			return false
		}
		return true
	})
}

func (s *streamMethodScan) target(expr ast.Expr) (*types.Func, string, bool) {
	if paren, ok := expr.(*ast.ParenExpr); ok {
		return s.target(paren.X)
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return nil, "", false
	}
	path, ok := s.path(sel.X)
	selection := s.method.info.Selections[sel]
	if !ok || selection == nil || selection.Kind() != types.MethodVal {
		return nil, "", false
	}
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil, "", false
	}
	// Look up the method on the concrete receiver, not its generic origin.
	t := s.exprType(sel.X)
	obj, indices, _ := types.LookupFieldOrMethod(t, true, fn.Pkg(), fn.Name())
	fn, ok = obj.(*types.Func)
	if !ok {
		return nil, "", false
	}
	path, ok = streamFieldPath(path, t, indices[:len(indices)-1])
	return fn, path, ok
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
	case *ast.StarExpr:
		return s.path(e.X)
	case *ast.SelectorExpr:
		prefix, ok := s.path(e.X)
		selection := s.method.info.Selections[e]
		if !ok || selection == nil || selection.Kind() != types.FieldVal {
			return "", false
		}
		return streamFieldPath(prefix, s.exprType(e.X), selection.Index())
	default:
		return "", false
	}
}

// exprType retains instantiated fields while the AST comes from a generic origin.
func (s *streamMethodScan) exprType(expr ast.Expr) types.Type {
	switch e := expr.(type) {
	case *ast.Ident:
		if s.method.info.ObjectOf(e) == s.receiver {
			return s.receiverType
		}
	case *ast.ParenExpr:
		return s.exprType(e.X)
	case *ast.StarExpr:
		if pointer, ok := s.exprType(e.X).Underlying().(*types.Pointer); ok {
			return pointer.Elem()
		}
	case *ast.SelectorExpr:
		if selection := s.method.info.Selections[e]; selection != nil && selection.Kind() == types.FieldVal {
			return streamFieldType(s.exprType(e.X), selection.Index())
		}
	}
	return s.method.info.TypeOf(expr)
}

func streamMergeLocks(a, b map[string]bool) map[string]bool {
	merged := maps.Clone(a)
	for path, exclusive := range merged {
		if other, ok := b[path]; !ok {
			delete(merged, path)
		} else {
			merged[path] = exclusive && other
		}
	}
	return merged
}

func (s *streamMethodScan) boolean(expr ast.Expr) (bool, bool) {
	value := s.method.info.Types[expr].Value
	if value == nil || value.Kind() != constant.Bool {
		return false, false
	}
	return constant.BoolVal(value), true
}

// Calls are ordered, but ordinary loads and receiver copies may follow sibling calls.
// Keep that uncertainty separate so synchronous helper bodies retain call ordering.
func (s *streamMethodScan) evalGroup(addresses, values []ast.Expr, locks map[string]bool, collect bool) map[string]bool {
	exprs := values
	if len(addresses) > 0 {
		exprs = slices.Concat(addresses, values)
	}
	if len(exprs) == 1 {
		if len(addresses) == 1 {
			return s.address(exprs[0], locks, collect)
		}
		return s.eval(exprs[0], locks, collect)
	}
	var releases []map[string]bool
	if collect {
		releases = make([]map[string]bool, len(exprs))
		for i, expr := range exprs {
			releases[i] = make(map[string]bool)
			s.releaseCalls(expr, releases[i], make(map[*types.Func]bool))
		}
	}
	previous := s.unordered
	for i, expr := range exprs {
		if collect {
			s.unordered = maps.Clone(previous)
			for j, released := range releases {
				if i != j && len(released) > 0 {
					if s.unordered == nil {
						s.unordered = make(map[string]bool)
					}
					maps.Copy(s.unordered, released)
				}
			}
		}
		if i < len(addresses) {
			locks = s.address(expr, locks, collect)
		} else {
			locks = s.eval(expr, locks, collect)
		}
	}
	s.unordered = previous
	return locks
}

func (s *streamMethodScan) readLocks(locks map[string]bool) map[string]bool {
	if len(s.unordered) == 0 {
		return locks
	}
	locks = maps.Clone(locks)
	for path := range s.unordered {
		delete(locks, path)
	}
	return locks
}

// eval is shared by CFG transfer and access collection so both use the same order.
func (s *streamMethodScan) eval(node ast.Node, locks map[string]bool, collect bool) map[string]bool {
	if node == nil {
		return locks
	}
	switch v := node.(type) {
	case *ast.AssignStmt:
		locks = s.evalGroup(v.Lhs, v.Rhs, locks, collect)
		for _, lhs := range v.Lhs {
			if v.Tok != token.ASSIGN && v.Tok != token.DEFINE {
				s.access(lhs, false, locks, collect)
			}
			s.access(lhs, true, locks, collect)
		}
		return locks
	case *ast.IncDecStmt:
		locks = s.address(v.X, locks, collect)
		s.access(v.X, false, locks, collect)
		s.access(v.X, true, locks, collect)
		return locks
	case *ast.DeferStmt:
		return s.call(v.Call, locks, collect, false)
	case *ast.GoStmt:
		locks = s.call(v.Call, locks, collect, false)
		// A spawned release can run before the caller's next access.
		released := make(map[string]bool)
		s.releaseCalls(v.Call, released, make(map[*types.Func]bool))
		for path := range released {
			delete(locks, path)
		}
		return locks
	case *ast.CallExpr:
		return s.call(v, locks, collect, true)
	case *ast.ReturnStmt:
		return s.evalGroup(nil, v.Results, locks, collect)
	case *ast.SendStmt:
		return s.evalGroup(nil, []ast.Expr{v.Chan, v.Value}, locks, collect)
	case *ast.BinaryExpr:
		if v.Op != token.LAND && v.Op != token.LOR {
			return s.evalGroup(nil, []ast.Expr{v.X, v.Y}, locks, collect)
		}
		locks = s.eval(v.X, locks, collect)
		if value, known := s.boolean(v.X); known {
			if value == (v.Op == token.LAND) {
				return s.eval(v.Y, locks, collect)
			}
			return locks
		}
		return streamMergeLocks(locks, s.eval(v.Y, maps.Clone(locks), collect))
	case *ast.CompositeLit:
		return s.evalGroup(nil, v.Elts, locks, collect)
	case *ast.KeyValueExpr:
		return s.evalGroup(nil, []ast.Expr{v.Key, v.Value}, locks, collect)
	case *ast.IndexExpr:
		return s.evalGroup(nil, []ast.Expr{v.X, v.Index}, locks, collect)
	case *ast.SliceExpr:
		return s.evalGroup(nil, []ast.Expr{v.X, v.Low, v.High, v.Max}, locks, collect)
	case *ast.ValueSpec:
		return s.evalGroup(nil, v.Values, locks, collect)
	case *ast.StarExpr:
		locks = s.eval(v.X, locks, collect)
		s.access(v, false, locks, collect)
		return locks
	case *ast.FuncLit:
		if collect {
			s.body(v.Body, nil)
		}
		return locks
	case *ast.SelectorExpr:
		if fn, path, ok := s.target(v); ok {
			locks = s.address(v, locks, collect)
			if collect {
				t := fn.Signature().Recv().Type()
				if _, pointer := t.Underlying().(*types.Pointer); !pointer {
					s.found = streamAppendAccess(s.found, v.Pos(), path, t, false, s.readLocks(locks))
				}
			}
			return locks
		}
		locks = s.address(v, locks, collect)
		s.access(v, false, locks, collect)
		return locks
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return s.address(v.X, locks, collect)
		}
	}
	ast.Inspect(node, func(n ast.Node) bool {
		if n == node {
			return true
		}
		if n != nil {
			locks = s.eval(n, locks, collect)
		}
		return false
	})
	return locks
}

// address evaluates a field's base without reading intermediate value structs.
func (s *streamMethodScan) address(expr ast.Expr, locks map[string]bool, collect bool) map[string]bool {
	switch v := expr.(type) {
	case *ast.Ident:
		return locks
	case *ast.ParenExpr:
		return s.address(v.X, locks, collect)
	case *ast.StarExpr:
		return s.eval(v.X, locks, collect)
	case *ast.SelectorExpr:
		locks = s.address(v.X, locks, collect)
		if t := s.exprType(v.X); t != nil {
			if _, pointer := t.Underlying().(*types.Pointer); pointer {
				s.access(v.X, false, locks, collect)
			}
		}
		// Promoted selections can dereference embedded pointers implicitly.
		if selection := s.method.info.Selections[v]; selection != nil {
			prefix, ok := s.path(v.X)
			indices := selection.Index()
			for i := 1; ok && collect && i < len(indices); i++ {
				path, valid := streamFieldPath(prefix, s.exprType(v.X), indices[:i])
				if !valid {
					break
				}
				t := streamFieldType(s.exprType(v.X), indices[:i])
				if _, pointer := t.Underlying().(*types.Pointer); pointer {
					s.found = streamAppendAccess(s.found, v.Pos(), path, t, false, s.readLocks(locks))
				}
			}
		}
		return locks
	default:
		return s.eval(expr, locks, collect)
	}
}

func (s *streamMethodScan) call(call *ast.CallExpr, locks map[string]bool, collect, synchronous bool) map[string]bool {
	// This invocation, including its arguments, precedes later sibling calls.
	unordered := s.unordered
	s.unordered = nil
	defer func() { s.unordered = unordered }()
	literal := calledFuncLit(call.Fun)
	fn, path, method := s.target(call.Fun)
	var functions []ast.Expr
	if literal == nil {
		functions = []ast.Expr{call.Fun}
	}
	locks = s.evalGroup(nil, slices.Concat(functions, call.Args), locks, collect)
	bodyLocks := locks
	if !synchronous {
		bodyLocks = nil
	}
	if collect {
		if literal != nil {
			s.body(literal.Body, bodyLocks)
		} else if method {
			s.found = append(s.found, s.analysis.accesses(fn, path, bodyLocks, s.visiting)...)
			t := fn.Signature().Recv().Type()
			if pointer, ok := t.Underlying().(*types.Pointer); ok {
				t = pointer.Elem()
			}
			if named, ok := types.Unalias(t).(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "sync/atomic" {
				start := len(s.found)
				s.found = streamAppendAccess(s.found, call.Pos(), path, t, fn.Name() != "Load", bodyLocks)
				for i := start; i < len(s.found); i++ {
					s.found[i].atomic = true
				}
			}
		}
	}
	if !synchronous {
		return locks
	}
	if method && streamMutexType(fn.Signature().Recv().Type()) {
		if !streamSharedPath(s.receiverType, s.prefix, path, true) {
			return locks
		}
		switch fn.Name() {
		case "Lock", "RLock":
			if locks == nil {
				locks = make(map[string]bool)
			}
			locks[path] = fn.Name() == "Lock"
		case "Unlock", "RUnlock":
			delete(locks, path)
		}
	} else {
		var released map[string]bool
		if literal != nil {
			released = make(map[string]bool)
			s.releaseCalls(literal.Body, released, make(map[*types.Func]bool))
		} else if method {
			released = s.analysis.releasedLocks(fn, path, make(map[*types.Func]bool))
		}
		for path := range released {
			delete(locks, path)
		}
	}
	return locks
}

func (s *streamMethodScan) access(expr ast.Expr, write bool, locks map[string]bool, collect bool) {
	path, ok := s.path(expr)
	if !collect || !ok {
		return
	}
	// The receiver variable itself is local, unlike the struct it points to.
	if id, ok := expr.(*ast.Ident); ok && s.method.info.ObjectOf(id) == s.receiver {
		return
	}
	if !write {
		locks = s.readLocks(locks)
	}
	start := len(s.found)
	s.found = streamAppendAccess(s.found, expr.Pos(), path, s.exprType(expr), write, locks)
	if _, indirect := ast.Unparen(expr).(*ast.StarExpr); indirect {
		for i := start; i < len(s.found); i++ {
			s.found[i].indirect = true
		}
	}
}

// Expand aggregate slots, stopping at pointers: copying a pointer does not read its pointee.
func streamAppendAccess(found []streamFieldAccess, pos token.Pos, path string, t types.Type, write bool, locks map[string]bool) []streamFieldAccess {
	if st, ok := t.Underlying().(*types.Struct); ok {
		for field := range st.Fields() {
			if field.Name() == "_" {
				continue
			}
			found = streamAppendAccess(found, pos, path+"."+field.Name(), field.Type(), write, locks)
		}
		return found
	}
	return append(found, streamFieldAccess{pos: pos, path: path, typ: t, write: write, locks: maps.Clone(locks)})
}

func streamFieldType(t types.Type, indices []int) types.Type {
	for _, index := range indices {
		if ptr, ok := t.Underlying().(*types.Pointer); ok {
			t = ptr.Elem()
		}
		t = t.Underlying().(*types.Struct).Field(index).Type()
	}
	return t
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

// A slot belongs to the receiver copy until a pointer is dereferenced on its path.
func streamSharedPath(t types.Type, prefix, path string, dereference bool) bool {
	if path != prefix && !strings.HasPrefix(path, prefix+".") {
		return true // A lock inherited from the caller is outside this receiver.
	}
	fields := strings.TrimPrefix(strings.TrimPrefix(path, prefix), ".")
	if fields != "" {
		for name := range strings.SplitSeq(fields, ".") {
			if _, ok := t.Underlying().(*types.Pointer); ok {
				return true
			}
			st, ok := t.Underlying().(*types.Struct)
			if !ok {
				return false
			}
			var fieldType types.Type
			for field := range st.Fields() {
				if field.Name() == name {
					fieldType = field.Type()
					break
				}
			}
			if fieldType == nil {
				return false
			}
			t = fieldType
		}
	}
	_, pointer := t.Underlying().(*types.Pointer)
	return dereference && pointer
}
