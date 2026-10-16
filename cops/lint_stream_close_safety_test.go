package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/prog"
)

func TestStreamCloseSafety(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{
			name: "plain close flag",
			src: `type stream struct { done bool }
func (s *stream) Next() bool { return !s.done }
func (s *stream) Close() { s.done = true }`,
			want: 1,
		},
		{
			name: "reader writes close reads",
			src: `type stream struct { err error }
func (s *stream) Next() bool { s.err = nil; return false }
func (s *stream) Close() error { return s.err }`,
			want: 1,
		},
		{
			name: "nested wrapper",
			src: `type inner struct { done bool }
func (s *inner) Next() bool { return !s.done }
type stream struct { inner *inner }
func (s *stream) Next() bool { return s.inner.Next() }
func (s *stream) Close() { s.inner.done = true }`,
			want: 1,
		},
		{
			name: "promoted generic retry helper",
			src: `type retry[T any] struct { stream *T }
func (r *retry[T]) next() bool { r.stream = new(T); return true }
type stream struct { retry[int] }
func (s *stream) Recv() bool { return s.next() }
func (s *stream) Close() { _ = s.stream }`,
			want: 1,
		},
		{
			name: "different wrapper fields do not alias",
			src: `type state struct { done bool }
type stream struct { read state; close state }
func (s *stream) Next() bool { return !s.read.done }
func (s *stream) Close() { s.close.done = true }`,
		},
		{
			name: "read only fields",
			src: `type stream struct { done chan struct{} }
func (s *stream) Next() bool { <-s.done; return false }
func (s *stream) Close() { close(s.done) }`,
		},
		{
			name: "atomic flag",
			src: `import "sync/atomic"
type stream struct { done atomic.Bool }
func (s *stream) Next() bool { return !s.done.Load() }
func (s *stream) Close() { s.done.Store(true) }`,
		},
		{
			name: "same mutex",
			src: `import "sync"
type stream struct { mu sync.Mutex; done bool }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
		{
			name: "read and write mutex",
			src: `import "sync"
type stream struct { mu sync.RWMutex; done bool }
func (s *stream) Next() bool { s.mu.RLock(); defer s.mu.RUnlock(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
		{
			name: "rlock does not permit writes",
			src: `import "sync"
type stream struct { mu sync.RWMutex; done bool }
func (s *stream) Next() bool { s.mu.RLock(); defer s.mu.RUnlock(); return !s.done }
func (s *stream) Close() { s.mu.RLock(); defer s.mu.RUnlock(); s.done = true }`,
			want: 1,
		},
		{
			name: "different mutexes",
			src: `import "sync"
type stream struct { a, b sync.Mutex; done bool }
func (s *stream) Next() bool { s.a.Lock(); defer s.a.Unlock(); return !s.done }
func (s *stream) Close() { s.b.Lock(); defer s.b.Unlock(); s.done = true }`,
			want: 1,
		},
		{
			name: "conditional lock",
			src: `import "sync"
type stream struct { mu sync.Mutex; done bool; lock bool }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.done }
func (s *stream) Close() { if s.lock { s.mu.Lock(); defer s.mu.Unlock() }; s.done = true }`,
			want: 1,
		},
		{
			name: "unlocked read after critical section",
			src: `import "sync"
type stream struct { mu sync.Mutex; done bool }
func (s *stream) Next() bool { s.mu.Lock(); s.mu.Unlock(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
			want: 1,
		},
		{
			name: "helper inherits lock",
			src: `import "sync"
type stream struct { mu sync.Mutex; done bool }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.read() }
func (s *stream) read() bool { return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
		{
			name: "once does not synchronize reader",
			src: `import "sync"
type stream struct { once sync.Once; done bool }
func (s *stream) Next() bool { return !s.done }
func (s *stream) Close() { s.once.Do(func() { s.done = true }) }`,
			want: 1,
		},
		{
			name: "deferred write runs after unlock",
			src: `import "sync"
type stream struct { mu sync.Mutex; done bool }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.done }
func (s *stream) Close() { defer func() { s.done = true }(); s.mu.Lock(); defer s.mu.Unlock() }`,
			want: 1,
		},
		{
			name: "promoted reader with shadowed field",
			src: `type inner struct { done bool }; func (s *inner) Next() bool { return !s.done }
type stream struct { *inner; done bool }; func (s *stream) Close() { s.done = true }`,
		},
		{
			name: "promoted reader same field",
			src: `type inner struct { done bool }; func (s *inner) Next() bool { return !s.done }
type stream struct { *inner }; func (s *stream) Close() { s.done = true }`, want: 1,
		},
		{
			name: "pointer alias",
			src: `type inner struct { done bool }; type P = *inner
type stream struct { in P }; func (s *stream) Next() bool { return s.in.done }; func (s *stream) Close() { s.in.done = true }`, want: 1,
		},
		{
			name: "defined pointer",
			src: `type inner struct { done bool }; type P *inner
type stream struct { in P }; func (s *stream) Next() bool { return s.in.done }; func (s *stream) Close() { s.in.done = true }`, want: 1,
		},
		{
			name: "embedded mutex",
			src: `import "sync"
type stream struct { sync.Mutex; done bool }
func (s *stream) Next() bool { s.Lock(); defer s.Unlock(); return !s.done }
func (s *stream) Close() { s.Lock(); defer s.Unlock(); s.done = true }`,
		},
		{
			name: "non stream",
			src: `type client struct { done bool }
func (c *client) Read() bool { return !c.done }
func (c *client) Close() { c.done = true }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := streamCloseTestPass(t, "package p\n"+tc.src)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
			for _, offense := range p.Offenses() {
				assert.Equal(t, "Lint/StreamCloseSafety", offense.CopName)
			}
		})
	}
}

func TestStreamCloseSafetyAcrossFiles(t *testing.T) {
	t.Parallel()
	p := streamCloseTestPass(t,
		`package p; type stream struct { done bool }; func (s *stream) Next() bool { return s.read() }`,
		`package p; func (s *stream) read() bool { return s.done }; func (s *stream) Close() { s.done = true }`,
	)
	NewLintStreamCloseSafety().Check(p)
	require.Len(t, p.Offenses(), 1)
	assert.Contains(t, p.Offenses()[0].Message, "done")
}

func streamCloseTestPass(t *testing.T, sources ...string) *prog.Pass {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for i, src := range sources {
		file, err := parser.ParseFile(fset, string(rune('a'+i))+".go", src, 0)
		require.NoError(t, err)
		files = append(files, file)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	config := types.Config{Importer: importer.Default()}
	pkg, err := config.Check("test", fset, files, info)
	require.NoError(t, err)
	return &prog.Pass{Cop: NewLintStreamCloseSafety(), Program: &prog.Program{
		Fset: fset, Packages: []*packages.Package{{Types: pkg, TypesInfo: info, Syntax: files}},
	}}
}

func TestStreamCloseSafetyClosureLocks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"stored closure invoked after unlock", `s.mu.Lock(); read := func() bool { return s.done }; s.mu.Unlock(); return read()`, 1},
		{"escaping callback", `s.mu.Lock(); defer s.mu.Unlock(); save(func() bool { return s.done }); return true`, 1},
		{"immediate closure inherits lock", `s.mu.Lock(); defer s.mu.Unlock(); return (func() bool { return s.done })()`, 0},
		{"immediate closure releases lock", `s.mu.Lock(); func() { s.mu.Unlock() }(); return s.done`, 1},
		{"immediate closure defers release", `s.mu.Lock(); func() { defer s.mu.Unlock() }(); return s.done`, 1},
		{"nested immediate closure releases lock", `s.mu.Lock(); func() { func() { s.mu.Unlock() }() }(); return s.done`, 1},
		{"conditional release invalidates lock", `s.mu.Lock(); func() { if s.done { s.mu.Unlock() } }(); return s.done`, 1},
		{"read-only immediate closure preserves lock", `s.mu.Lock(); defer s.mu.Unlock(); func() { _ = s.done }(); return s.done`, 0},
		{"closure acquires own lock", `read := func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.done }; return read()`, 0},
		{"goroutine does not inherit lock", `s.mu.Lock(); defer s.mu.Unlock(); go func() { _ = s.done }(); return true`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
import "sync"
type stream struct { mu sync.Mutex; done bool }
func save(func() bool) {}
func (s *stream) Next() bool { `+tc.body+` }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyValueReceivers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"scalar write is copied", `type stream struct { done bool }
func (s *stream) Next() bool { return s.done }
func (s stream) Close() { s.done = true }`, 0},
		{"nested value write is copied", `type state struct { done bool }; type stream struct { state state }
func (s *stream) Next() bool { return s.state.done }
func (s stream) Close() { s.state.done = true }`, 0},
		{"pointer field slot is copied", `type state struct { done bool }; type stream struct { state *state }
func (s *stream) Next() bool { return s.state.done }
func (s stream) Close() { s.state = nil }`, 0},
		{"pointee remains shared", `type state struct { done bool }; type stream struct { state *state }
func (s *stream) Next() bool { return s.state.done }
func (s stream) Close() { s.state.done = true }`, 1},
		{"promoted pointee remains shared", `type state struct { done bool }; type stream struct { *state }
func (s *stream) Next() bool { return s.done }
func (s stream) Close() { s.done = true }`, 1},
		{"pointer helper operates on copy", `type stream struct { done bool }
func (s *stream) Next() bool { return s.done }
func (s *stream) set() { s.done = true }
func (s stream) Close() { s.set() }`, 0},
		{"helper pointee remains shared", `type state struct { done bool }; type stream struct { state *state }
func (s *stream) Next() bool { return s.state.done }
func (s *state) set() { s.done = true }
func (s stream) Close() { s.state.set() }`, 1},
		{"copied mutex does not protect pointee", `type state struct { done bool }; type stream struct { mu sync.Mutex; state *state }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.state.done }
func (s stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.state.done = true }`, 1},
		{"pointer mutex protects pointee", `type state struct { done bool }; type stream struct { mu *sync.Mutex; state *state }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.state.done }
func (s stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.state.done = true }`, 0},
		{"nested shared mutex", `type state struct { mu sync.Mutex; done bool }; type stream struct { state *state }
func (s *stream) Next() bool { s.state.mu.Lock(); defer s.state.mu.Unlock(); return s.state.done }
func (s stream) Close() { s.state.mu.Lock(); defer s.state.mu.Unlock(); s.state.done = true }`, 0},
		{"value reader writes its own copy", `type stream struct { done bool }
func (s stream) Next() bool { s.done = true; return true }
func (s *stream) Close() { _ = s.done }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, "package p\nimport \"sync\"\nvar _ sync.Mutex\n"+tc.src)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyHelperUnlocks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, helper, body string
		want               int
	}{
		{"direct release", `s.mu.Unlock()`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"read lock release", `s.mu.RUnlock()`, `s.mu.RLock(); s.release(); return s.done`, 1},
		{"deferred read lock release", `defer s.mu.RUnlock()`, `s.mu.RLock(); s.release(); return s.done`, 1},
		{"deferred release", `defer s.mu.Unlock()`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"conditional release", `if s.done { s.mu.Unlock() }`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"nested helper release", `s.releaseAgain()`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"deferred helper release", `defer s.releaseAgain()`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"IIFE helper release", `s.mu.Unlock()`, `s.mu.Lock(); func() { s.release() }(); return s.done`, 1},
		{"deferred IIFE helper release", `s.mu.Unlock()`, `s.mu.Lock(); func() { defer s.release() }(); return s.done`, 1},
		{"read before release", `s.mu.Unlock()`, `s.mu.Lock(); result := s.done; s.release(); return result`, 0},
		{"reacquire after helper", `s.mu.Unlock()`, `s.mu.Lock(); s.release(); s.mu.Lock(); defer s.mu.Unlock(); return s.done`, 0},
		{"deferred caller release remains held", `s.mu.Unlock()`, `s.mu.Lock(); defer s.release(); return s.done`, 0},
		{"stored closure does not release", `_ = func() { s.mu.Unlock() }`, `s.mu.Lock(); defer s.mu.Unlock(); s.release(); return s.done`, 0},
		{"goroutine closure may release before read", `go func() { s.mu.Unlock() }()`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"goroutine helper may release before read", `go s.releaseAgain()`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"joined goroutine releases before read", `done := make(chan struct{}); go func() { s.mu.Unlock(); close(done) }(); <-done`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"unreachable and release preserves lock", `_ = false && s.releaseValue() == 0`, `s.mu.Lock(); defer s.mu.Unlock(); s.release(); return s.done`, 0},
		{"unreachable or release preserves lock", `_ = true || s.releaseValue() == 0`, `s.mu.Lock(); defer s.mu.Unlock(); s.release(); return s.done`, 0},
		{"goroutine body may release before read", `go s.mu.Unlock()`, `s.mu.Lock(); s.release(); return s.done`, 1},
		{"goroutine argument does release synchronously", `go consume(s.releaseValue(), true)`, `s.mu.Lock(); s.release(); return s.done`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
import "sync"
type stream struct { mu sync.RWMutex; done bool }
func consume(int, bool) {}
func (s *stream) releaseAgain() { s.mu.Unlock() }
func (s *stream) releaseValue() int { s.mu.Unlock(); return 0 }
func (s *stream) release() { `+tc.helper+` }
func (s *stream) Next() bool { `+tc.body+` }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyEvaluationOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, next, close string
		want              int
	}{
		{"RHS closure releases before write", `s.mu.Lock(); defer s.mu.Unlock(); return s.done`, `s.mu.Lock(); s.done = func() bool { s.mu.Unlock(); return true }()`, 1},
		{"RHS helper releases before write", `s.mu.Lock(); defer s.mu.Unlock(); return s.done`, `s.mu.Lock(); s.done = s.release()`, 1},
		{"later argument after closure release", `s.mu.Lock(); return consume(func() bool { s.mu.Unlock(); return true }(), s.done)`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"earlier argument may follow helper release", `s.mu.Lock(); return consume(s.done, s.release())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"earlier argument may follow nested release", `s.mu.Lock(); return consume(s.done, consume(s.release(), true))`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"earlier RHS may follow helper release", `s.mu.Lock(); done, _ := s.done, s.release(); return done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"earlier variable initializer may follow release", `s.mu.Lock(); var done, _ = s.done, s.release(); return done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"earlier binary operand may follow release", `s.mu.Lock(); return s.done != s.release()`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"earlier composite element may follow release", `s.mu.Lock(); done := []bool{s.done, s.release()}; return done[0]`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"ordered value helper call before release", `s.mu.Lock(); return consume(s.valueRead(), s.release())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"ordered nested argument read before release", `s.mu.Lock(); return consume(consume(s.done, true), s.release())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"ordered helper call before release", `s.mu.Lock(); return consume(s.read(), s.release())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"ordered IIFE call before release", `s.mu.Lock(); return consume(func() bool { return s.done }(), s.release())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"short circuit never releases with and", `s.mu.Lock(); defer s.mu.Unlock(); _ = false && s.release(); return s.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"short circuit never releases with or", `s.mu.Lock(); defer s.mu.Unlock(); _ = true || s.release(); return s.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"short circuit always releases with and", `s.mu.Lock(); _ = true && s.release(); return s.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"short circuit always releases with or", `s.mu.Lock(); _ = false || s.release(); return s.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"dynamic and acquisition is conditional", `_ = s.done && func() bool { s.mu.Lock(); return true }(); return s.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"dynamic or acquisition is conditional", `_ = s.done || func() bool { s.mu.Lock(); return true }(); return s.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"short circuit skips field read", `return false && s.done`, `s.done = true`, 0},
		{"short circuit left read precedes release", `s.mu.Lock(); return s.done && s.release()`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"unrelated release keeps protecting mutex", `s.mu.Lock(); return consume(s.done, s.releaseOther())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"later argument after helper release", `s.mu.Lock(); return consume(s.release(), s.done)`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"later argument helper body after release", `s.mu.Lock(); return consume(s.release(), s.read())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"helper receiver evaluated after release", `s.mu.Lock(); return consume(s.release(), s.valueRead())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"method body runs after arguments", `s.mu.Lock(); return s.readArg(s.release())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"short circuit release is possible", `s.mu.Lock(); _ = s.done && s.release(); return s.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"receiver copy may follow argument release", `s.mu.Lock(); return s.valueReadArg(s.release())`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 1},
		{"read before release stays protected", `s.mu.Lock(); result := s.done; s.release(); return result`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
		{"RHS read before release stays protected", `s.mu.Lock(); result := func() bool { result := s.done; s.mu.Unlock(); return result }(); return result`, `s.mu.Lock(); defer s.mu.Unlock(); s.done = true`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
import "sync"
type stream struct { mu, other sync.Mutex; done bool }
func consume(bool, bool) bool { return true }
func (s *stream) releaseOther() bool { s.other.Unlock(); return true }
func (s *stream) release() bool { s.mu.Unlock(); return true }
func (s *stream) read() bool { return s.done }
func (s *stream) readArg(bool) bool { return s.done }
func (s stream) valueRead() bool { return s.done }
func (s stream) valueReadArg(bool) bool { return s.done }
func (s *stream) Next() bool { `+tc.next+` }
func (s *stream) Close() { `+tc.close+` }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyReceiverCopies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"top level copy before method lock", `type stream struct { mu *sync.Mutex; done bool }
func (s stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`, 1},
		{"helper copy before method lock", `type stream struct { mu *sync.Mutex; done bool }
func (s stream) read() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.done }
func (s *stream) Next() bool { return s.read() }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`, 1},
		{"unused field copied by receiver", `type stream struct { done bool }
func (s stream) Next() bool { return true }
func (s *stream) Close() { s.done = true }`, 1},
		{"Close receiver copy races with reader write", `type stream struct { mu *sync.Mutex; done bool }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); s.done = true; return true }
func (s stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); _ = s.done }`, 1},
		{"helper receiver copy inherits caller mutex", `type stream struct { mu sync.Mutex; done bool }
func (s stream) read() bool { return s.done }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.read() }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`, 0},
		{"receiver copy does not read pointee", `type state struct { done bool }; type stream struct { mu *sync.Mutex; state *state }
func (s stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.state.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.state.done = true }`, 0},
		{"receiver copy reads pointer slot", `type state struct { done bool }; type stream struct { mu *sync.Mutex; state *state }
func (s stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.state != nil }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.state = nil }`, 1},
		{"value helper cannot unlock copied mutex", `type stream struct { mu sync.Mutex; done bool }
func (s stream) release() { s.mu.Unlock() }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); s.release(); return s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`, 0},
		{"value helper releases shared pointer mutex", `type stream struct { mu *sync.Mutex; done bool }
func (s stream) release() { defer s.mu.Unlock() }
func (s *stream) Next() bool { s.mu.Lock(); s.release(); return s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`, 1},
		{"value receiver explicit scalar pointee remains shared", `type stream struct { scalar *bool }
func (s stream) Next() bool { return *s.scalar }
func (s *stream) Close() { *s.scalar = true }`, 1},
		{"value receiver explicit aggregate pointee remains shared", `type state struct { done bool }; type stream struct { state *state }
func (s stream) Next() bool { _ = *s.state; return true }
func (s *stream) Close() { s.state.done = true }`, 1},
		{"method value captures receiver copy", `type stream struct { done bool }
func (s stream) read() bool { return true }
func (s *stream) Next() bool { read := s.read; return read() }
func (s *stream) Close() { s.done = true }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, "package p\nimport \"sync\"\nvar _ sync.Mutex\n"+tc.src)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyAggregateFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, next, close string
		want              int
	}{
		{"aggregate read overlaps descendant write", `s.state.done = true; return true`, `_ = s.state`, 1},
		{"aggregate write overlaps descendant read", `return s.state.done`, `s.state = state{}`, 1},
		{"nested aggregate read overlaps descendant", `s.nested.state.done = true; return true`, `_ = s.nested`, 1},
		{"nested aggregate write overlaps descendant", `return s.nested.state.done`, `s.nested = nested{}`, 1},
		{"aggregate read does not read pointee", `s.state.ptr.done = true; return true`, `_ = s.state`, 0},
		{"aggregate read includes pointer slot", `s.state.ptr = nil; return true`, `_ = s.state`, 1},
		{"leaf write does not write ancestor", `return s.state.other`, `s.state.done = true`, 0},
		{"shared mutex protects aggregate", `s.mu.Lock(); defer s.mu.Unlock(); return s.state.done`, `s.mu.Lock(); defer s.mu.Unlock(); s.state = state{}`, 0},
		{"address taking does not read descendant", `_ = &s.state; return true`, `s.state.done = true`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
import "sync"
type state struct { done, other bool; ptr *state }
type nested struct { state state }
type stream struct { mu sync.Mutex; state state; nested nested }
func (s *stream) Next() bool { `+tc.next+` }
func (s *stream) Close() { `+tc.close+` }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyPromotedClose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"promoted pointer Close", `type base struct { done bool }
func (s *base) Close() { s.done = true }
type stream struct { *base }
func (s *stream) Next() bool { return s.done }`, 1},
		{"promoted Close through value embedding", `type base struct { done bool }
func (s *base) Close() { s.done = true }
type stream struct { base }
func (s *stream) Recv() bool { return s.done }`, 1},
		{"nested promoted Close", `type base struct { done bool }
func (s *base) Close() { s.done = true }
type middle struct { *base }
type stream struct { middle }
func (s *stream) Next() bool { return s.done }`, 1},
		{"generic promoted Close", `type base[T any] struct { done bool; value T }
func (s *base[T]) Close() { s.done = true }
type stream struct { *base[int] }
func (s *stream) Next() bool { return s.done }`, 1},
		{"promoted Close reads embedded pointer slot", `type base struct { done bool }
func (s *base) Close() { s.done = true }
type stream struct { *base }
func (s *stream) Next() bool { s.base = nil; return true }`, 1},
		{"promoted Next reads embedded pointer slot", `type base struct { done bool }
func (s *base) Next() bool { return s.done }
type stream struct { *base }
func (s *stream) Close() { s.base = nil }`, 1},
		{"promoted Close different field", `type base struct { done bool }
func (s *base) Close() { s.done = true }
type stream struct { *base; done bool }
func (s *stream) Next() bool { return s.done }`, 0},
		{"promoted Close synchronized", `type base struct { mu sync.Mutex; done bool }
func (s *base) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }
type stream struct { *base }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.done }`, 0},
		{"ambiguous Close", `type base struct { done bool }
func (s *base) Close() { s.done = true }
type other struct{}
func (*other) Close() {}
type stream struct { *base; *other }
func (s *stream) Next() bool { return s.done }`, 0},
		{"duplicate inherited pair reports once", `type base struct { done bool }
func (s *base) Close() { s.done = true }
func (s *base) Next() bool { return s.done }
type stream struct { *base }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, "package p\nimport \"sync\"\nvar _ sync.Mutex\n"+tc.src)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyGoDeferEvaluation(t *testing.T) {
	t.Parallel()
	for _, keyword := range []string{"go", "defer"} {
		for _, tc := range []struct {
			name, call string
			want       int
		}{
			{"earlier argument may follow synchronous unlock", `consumeTwo(s.done, s.release())`, 1},
			{"converted argument may follow synchronous unlock", `consumeTwo(bool(s.done), s.release())`, 1},
			{"wrapped converted argument precedes synchronous unlock", `consumeTwo(identity(bool(s.done)), s.release())`, 0},
			{"argument field read is synchronous", `consume(s.done)`, 0},
			{"argument helper is synchronous", `consume(s.read())`, 0},
			{"argument IIFE is synchronous", `consume(func() bool { return s.done }())`, 0},
			{"receiver helper is synchronous", `s.self(s.done).read()`, 0},
			{"value receiver copy is synchronous", `s.valueRead()`, 0},
			{"literal argument is synchronous", `func(bool) {}(s.done)`, 0},
			{"method body does not inherit lock", `s.read()`, 1},
			{"literal body does not inherit lock", `func() { _ = s.done }()`, 1},
			{"parenthesized literal body does not inherit lock", `(func() { _ = s.done })()`, 1},
			{"later argument sees synchronous unlock", `consumeTwo(s.release(), s.done)`, 1},
		} {
			t.Run(keyword+"/"+tc.name, func(t *testing.T) {
				p := streamCloseTestPass(t, `package p
import "sync"
type stream struct { mu sync.Mutex; done bool }
type other struct{}
func (*other) read() {}
func consume(bool) {}
func consumeTwo(bool, bool) {}
func identity(b bool) bool { return b }
func (s *stream) read() bool { return s.done }
func (s stream) valueRead() bool { return s.done }
func (s *stream) self(bool) *other { return nil }
func (s *stream) release() bool { s.mu.Unlock(); return true }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); `+keyword+` `+tc.call+`; return true }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }
`)
				NewLintStreamCloseSafety().Check(p)
				assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
			})
		}
	}
}

func TestStreamCloseSafetyAsyncRelease(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"direct unlock", `s.mu.Lock(); go s.mu.Unlock(); return s.done`, 1},
		{"read unlock", `s.mu.RLock(); go s.mu.RUnlock(); return s.done`, 1},
		{"closure unlock", `s.mu.Lock(); go func() { s.mu.Unlock() }(); return s.done`, 1},
		{"helper unlock", `s.mu.Lock(); go s.release(); return s.done`, 1},
		{"argument read before spawned unlock", `s.mu.Lock(); go s.releaseArg(s.done); return true`, 0},
		{"IIFE argument before spawned unlock", `s.mu.Lock(); go s.releaseArg(func() bool { return s.done }()); return true`, 0},
		{"argument read does not protect subsequent read", `s.mu.Lock(); go s.releaseArg(s.done); return s.done`, 1},
		{"unrelated async unlock", `s.mu.Lock(); defer s.mu.Unlock(); go s.other.Unlock(); return s.done`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
import "sync"
type stream struct { mu, other sync.RWMutex; done bool }
func (s *stream) release() { s.mu.Unlock() }
func (s *stream) releaseArg(bool) { s.mu.Unlock() }
func (s *stream) Next() bool { `+tc.body+` }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyGenericAggregates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"helper writes instantiated aggregate", `type state struct { done bool }
type base[T any] struct { value T }
func (s *base[T]) reset() { var zero T; s.value = zero }
type stream struct { base[state] }
func (s *stream) Next() bool { return s.value.done }
func (s *stream) Close() { s.reset() }`, 1},
		{"nested generic helper", `type state struct { done bool }
type base[T any] struct { value T }
func (s *base[T]) reset() { var zero T; s.value = zero }
type middle[T any] struct { base[T] }
func (s *middle[T]) clear() { s.reset() }
type stream struct { middle[state] }
func (s *stream) Next() bool { return s.value.done }
func (s *stream) Close() { s.clear() }`, 1},
		{"generic scalar helper", `type base[T any] struct { value T }
func (s *base[T]) reset() { var zero T; s.value = zero }
type stream struct { base[bool] }
func (s *stream) Next() bool { return s.value }
func (s *stream) Close() { s.reset() }`, 1},
		{"generic helper receiver copy", `type state struct { done bool }
type base[T any] struct { value T }
func (s base[T]) read() bool { return true }
type stream struct { base[state] }
func (s *stream) Next() bool { return s.read() }
func (s *stream) Close() { s.value.done = true }`, 1},
		{"generic entry receiver copy", `type state struct { done bool }
type base[T any] struct { value T }
func (s base[T]) Next() bool { return true }
type stream struct { base[state] }
func (s *stream) Close() { s.value.done = true }`, 1},
		{"generic pointer slot read conflicts with reset", `type state struct { done bool }
type base[T any] struct { value T }
func (s *base[T]) reset() { var zero T; s.value = zero }
type stream struct { base[*state] }
func (s *stream) Next() bool { return s.value.done }
func (s *stream) Close() { s.reset() }`, 1},
		{"generic copy does not read pointee", `type state struct { done bool }
type base[T any] struct { value T }
func (s base[T]) read() bool { return true }
type stream struct { base[*state] }
func (s *stream) Next() bool { return s.read() }
func (s *stream) Close() { s.value.done = true }`, 0},
		{"value generic helper only writes copy", `type state struct { done bool }
type base[T any] struct { value T }
func (s base[T]) reset() { var zero T; s.value = zero }
type stream struct { base[state] }
func (s *stream) Next() bool { return s.value.done }
func (s *stream) Close() { s.reset() }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, "package p\n"+tc.src)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyDereferencedAggregates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, next, close string
		want              int
	}{
		{"pointee aggregate read", `_ = *s.state; return true`, `s.state.done = true`, 1},
		{"pointee aggregate write", `return s.state.done`, `*s.state = state{}`, 1},
		{"whole receiver write", `return s.done`, `*s = stream{}`, 1},
		{"explicit receiver read", `return (*s).done`, `s.done = true`, 1},
		{"explicit receiver write", `return s.done`, `(*s).done = true`, 1},
		{"pointee access not pointer slot write", `_ = *s.scalar; return true`, `s.scalar = nil`, 1},
		{"pointee write not pointer slot read", `_ = s.scalar; return true`, `*s.scalar = true`, 0},
		{"pointer chain write not outer slot read", `_ = s.chain; return true`, `*s.chain = nil`, 0},
		{"double dereference read", `_ = **s.chain; return true`, `**s.chain = true`, 1},
		{"address does not read aggregate", `_ = &(*s.state); return true`, `s.state.done = true`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
type state struct { done bool }
type stream struct { done bool; state *state; scalar *bool; chain **bool }
func (s *stream) Next() bool { `+tc.next+` }
func (s *stream) Close() { `+tc.close+` }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyAtomicCopies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, next, close string
		want              int
	}{
		{"atomic operations", `return s.done.Load()`, `s.done.Store(true)`, 0},
		{"unused atomic copied by value receiver", `return s.copy()`, `s.done.Store(true)`, 1},
		{"plain atomic aggregate read", `_ = s.done; return true`, `s.done.Store(true)`, 1},
		{"plain atomic aggregate write", `return s.done.Load()`, `s.done = atomic.Bool{}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
import "sync/atomic"
type stream struct { done atomic.Bool }
func (s stream) copy() bool { return true }
func (s *stream) Next() bool { `+tc.next+` }
func (s *stream) Close() { `+tc.close+` }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyGenericReceivers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"uninstantiated parameter", `type stream[T any] struct { value T }
func (s *stream[T]) Next() T { return s.value }
func (s *stream[T]) Close() { var zero T; s.value = zero }`, 1},
		{"renamed receiver parameters", `type stream[Value any] struct { value Value }
func (s *stream[Read]) Next() Read { return s.value }
func (s *stream[Write]) Close() { var zero Write; s.value = zero }`, 1},
		{"multiple renamed receiver parameters", `type stream[A, B any] struct { first A; second B }
func (s *stream[B, A]) Next() A { return s.second }
func (s *stream[X, Y]) Close() { var zero Y; s.second = zero }`, 1},
		{"named generic field", `type state[T any] struct { value T }
type stream[T any] struct { state state[T] }
func (s *stream[Read]) Next() Read { return s.state.value }
func (s *stream[Write]) Close() { s.state = state[Write]{} }`, 1},
		{"renamed generic helper", `type stream[T any] struct { value T }
func (s *stream[Helper]) read() Helper { return s.value }
func (s *stream[Read]) Next() Read { return s.read() }
func (s *stream[Write]) Close() { var zero Write; s.value = zero }`, 1},
		{"promoted generic helper", `type base[T any] struct { value T }
func (s *base[Helper]) reset() { var zero Helper; s.value = zero }
type stream[T any] struct { base[T] }
func (s *stream[Read]) Next() Read { return s.value }
func (s *stream[Write]) Close() { s.reset() }`, 1},
		{"promoted generic entry", `type base[T any] struct { value T }
func (s *base[Read]) Next() Read { return s.value }
func (s *base[Write]) Close() { var zero Write; s.value = zero }
type stream[T any] struct { *base[T] }`, 1},
		{"generic receiver copy", `type stream[T any] struct { value T }
func (s stream[Read]) Next() bool { return true }
func (s *stream[Write]) Close() { var zero Write; s.value = zero }`, 1},
		{"generic pointer slot reset", `type stream[T any] struct { value *T }
func (s *stream[Read]) Next() Read { return *s.value }
func (s *stream[Write]) Close() { s.value = nil }`, 1},
		{"generic pointee write does not write pointer slot", `type stream[T any] struct { value *T }
func (s *stream[Read]) Next() *Read { return s.value }
func (s *stream[Write]) Close() { var zero Write; *s.value = zero }`, 0},
		{"generic pointee access", `type stream[T any] struct { value *T }
func (s *stream[Read]) Next() Read { return *s.value }
func (s *stream[Write]) Close() { var zero Write; *s.value = zero }`, 1},
		{"generic shared mutex", `import "sync"
type stream[T any] struct { mu sync.Mutex; value T }
func (s *stream[Read]) Next() Read { s.mu.Lock(); defer s.mu.Unlock(); return s.value }
func (s *stream[Write]) Close() { s.mu.Lock(); defer s.mu.Unlock(); var zero Write; s.value = zero }`, 0},
		{"constrained parameter", `type stream[T ~int] struct { value T }
func (s *stream[Read]) Next() Read { return s.value }
func (s *stream[Write]) Close() { s.value = 0 }`, 1},
		{"generic sizeof does not release", `import ("sync"; "unsafe")
type stream[T any] struct { mu sync.Mutex; value T }
func (s *stream[V]) release() V { s.mu.Unlock(); var zero V; return zero }
func (s *stream[Read]) Next() Read { s.mu.Lock(); _ = unsafe.Sizeof(s.release()); return s.value }
func (s *stream[Write]) Close() { s.mu.Lock(); defer s.mu.Unlock(); var zero Write; s.value = zero }`, 0},
		{"dot imported generic alignof does not release", `import ("sync"; . "unsafe")
type stream[T any] struct { mu sync.Mutex; value T }
func (s *stream[V]) release() V { s.mu.Unlock(); var zero V; return zero }
func (s *stream[Read]) Next() Read { s.mu.Lock(); _ = Alignof(s.release()); return s.value }
func (s *stream[Write]) Close() { s.mu.Lock(); defer s.mu.Unlock(); var zero Write; s.value = zero }`, 0},
		{"generic atomic pointer", `import "sync/atomic"
type stream[T any] struct { value atomic.Pointer[T] }
func (s *stream[Read]) Next() *Read { return s.value.Load() }
func (s *stream[Write]) Close() { s.value.Store(nil) }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, "package p\n"+tc.src)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}

func TestStreamCloseSafetyLoadedGeneric(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module streamtest\n\ngo 1.26.1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stream.go"), []byte(`package streamtest
type Stream[Value any] struct { value Value }
func (s *Stream[Read]) Next() Read { return s.value }
func (s *Stream[Write]) Close() { var zero Write; s.value = zero }
`), 0o600))
	program, err := prog.LoadDir(dir, ".")
	require.NoError(t, err)
	c := NewLintStreamCloseSafety()
	p := &prog.Pass{Cop: c, Program: program}
	c.Check(p)
	assert.Len(t, p.Offenses(), 1)
}

func TestStreamCloseSafetyConversionOrdering(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"bool conversion", `consume(bool(s.done), s.release())`, 1},
		{"named conversion", `consume(flag(s.done), s.release())`, 1},
		{"alias conversion", `consume(alias(s.done), s.release())`, 1},
		{"interface conversion", `consume(any(s.done), s.release())`, 1},
		{"parenthesized conversion", `consume((bool)(s.done), s.release())`, 1},
		{"nested conversion", `consume(bool(flag(s.done)), s.release())`, 1},
		{"conversion before IIFE release", `consume(bool(s.done), func() bool { s.mu.Unlock(); return true }())`, 1},
		{"converted RHS", `_, _ = bool(s.done), s.release()`, 1},
		{"slice length builtin", `consume(len(s.data), s.release())`, 1},
		{"omitted slice bounds", `consume(s.data[:], s.release())`, 1},
		{"omitted low bound", `consume(s.data[:1], s.release())`, 1},
		{"omitted high bound", `consume(s.data[1:], s.release())`, 1},
		{"omitted capacity bound", `consume(s.data[0:1], s.release())`, 1},
		{"full slice", `consume(s.data[0:1:1], s.release())`, 1},
		{"full slice omitted low", `consume(s.data[:1:1], s.release())`, 1},
		{"slice without release", `consume(s.data[:], true)`, 0},
		{"slice capacity builtin", `consume(cap(s.data), s.release())`, 1},
		{"append builtin", `consume(append([]bool{}, s.done), s.release())`, 1},
		{"complex builtin", `consume(complex(s.number, 0), s.release())`, 1},
		{"min builtin", `consume(min(s.number, 0), s.release())`, 1},
		{"real wrapper call", `consume(identity(s.done), s.release())`, 0},
		{"shadowed builtin is real call", `len := identity; consume(len(s.done), s.release())`, 0},
		{"shadowed type is real call", `bool := identity; consume(bool(s.done), s.release())`, 0},
		{"wrapper around conversion", `consume(identity(bool(s.done)), s.release())`, 0},
		{"conversion around wrapper", `consume(bool(identity(s.done)), s.release())`, 0},
		{"wrapper in builtin", `consume(append([]bool{}, identity(s.done)), s.release())`, 0},
		{"conversion argument releases", `consume(bool(s.release()), s.done)`, 1},
		{"builtin argument releases", `consume(append([]bool{}, s.release()), s.done)`, 1},
		{"constant array length is not a read", `consume(len(s.array), s.release())`, 0},
		{"constant sizeof does not evaluate argument", `consume(unsafe.Sizeof(s.release()), s.done)`, 0},
		{"constant sizeof does not read field", `consume(unsafe.Sizeof(s.done), s.release())`, 0},
		{"unevaluated sizeof release is not pending", `consume(s.done, unsafe.Sizeof(s.release()))`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := streamCloseTestPass(t, `package p
import ("sync"; "unsafe")
var _ = unsafe.Sizeof(0)
type flag bool
type alias = bool
type stream struct { mu sync.Mutex; done bool; data []byte; number float64; array [2]bool }
func consume(any, any) {}
func identity(b bool) bool { return b }
func (s *stream) release() bool { s.mu.Unlock(); return true }
func (s *stream) Next() bool { s.mu.Lock(); `+tc.body+`; return true }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true; s.data = nil; s.number = 1; s.array = [2]bool{} }
`)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
		})
	}
}
