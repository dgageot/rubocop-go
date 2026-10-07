package cops

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
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

func TestStreamCloseSafetyCallLockState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		next    string
		close   string
		helpers string
		want    int
	}{
		{
			name:    "helper unlocks caller mutex",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "reader helper unlocks caller mutex",
			next:    "s.mu.Lock(); s.release(); return !s.done",
			close:   "s.mu.Lock(); defer s.mu.Unlock(); s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:  "immediately invoked literal unlocks caller mutex",
			close: "s.mu.Lock(); func() { s.mu.Unlock() }(); s.done = true",
			want:  1,
		},
		{
			name:  "parenthesized literal unlocks caller mutex",
			close: "s.mu.Lock(); (func() { s.mu.Unlock() })(); s.done = true",
			want:  1,
		},
		{
			name:  "reader literal unlocks caller mutex",
			next:  "s.mu.Lock(); func() { s.mu.Unlock() }(); return !s.done",
			close: "s.mu.Lock(); defer s.mu.Unlock(); s.done = true",
			want:  1,
		},
		{
			name:    "locked helper preserves caller mutex",
			close:   "s.mu.Lock(); defer s.mu.Unlock(); s.write(); s.done = true",
			helpers: "func (s *stream) write() { s.done = true }",
		},
		{
			name:  "locked literal preserves caller mutex",
			close: "s.mu.Lock(); defer s.mu.Unlock(); func() { s.done = true }(); s.done = true",
		},
		{
			name:    "helper locks its own accesses",
			close:   "s.write()",
			helpers: "func (s *stream) write() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }",
		},
		{
			name:  "literal locks its own accesses",
			close: "func() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }()",
		},
		{
			name:    "helper conditionally unlocks caller mutex",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { if s.flag { s.mu.Unlock() } }",
			want:    1,
		},
		{
			name:  "literal conditionally unlocks caller mutex",
			close: "s.mu.Lock(); func() { if s.flag { s.mu.Unlock() } }(); s.done = true",
			want:  1,
		},
		{
			name:    "conditional helper call invalidates merged state",
			close:   "s.mu.Lock(); if s.flag { s.release() }; s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "returning branch leaves surviving branch locked",
			close:   "s.mu.Lock(); if s.flag { s.release(); return }; s.done = true; s.mu.Unlock()",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
		},
		{
			name:    "helper loop may unlock caller mutex",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { for s.flag { s.mu.Unlock(); break } }",
			want:    1,
		},
		{
			name:    "loop back edge invalidates earlier access",
			close:   "s.mu.Lock(); for s.flag { s.done = true; s.release() }",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "loop break invalidates following access",
			close:   "s.mu.Lock(); for s.flag { s.release(); break }; s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "direct relock restores caller state",
			close:   "s.mu.Lock(); s.release(); s.mu.Lock(); s.done = true; s.mu.Unlock()",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
		},
		{
			name:    "unreachable helper unlock does not invalidate state",
			close:   "s.mu.Lock(); defer s.mu.Unlock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { return; s.mu.Unlock() }",
		},
		{
			name:    "nested helper unlocks caller mutex",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { s.unlock() }; func (s *stream) unlock() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "helper deferred unlock runs before caller resumes",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { defer s.mu.Unlock() }",
			want:    1,
		},
		{
			name:  "literal deferred unlock runs before caller resumes",
			close: "s.mu.Lock(); func() { defer s.mu.Unlock() }(); s.done = true",
			want:  1,
		},
		{
			name:    "caller deferred helper does not unlock immediately",
			close:   "s.mu.Lock(); defer s.release(); s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
		},
		{
			name:    "nested deferred helper unlocks caller mutex",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { defer s.unlock() }; func (s *stream) unlock() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "unlocking helper in assignment precedes write",
			close:   "s.mu.Lock(); s.done = s.release()",
			helpers: "func (s *stream) release() bool { s.mu.Unlock(); return true }",
			want:    1,
		},
		{
			name:    "unlocking argument precedes helper body",
			close:   "s.mu.Lock(); s.write(func() bool { s.mu.Unlock(); return true }())",
			helpers: "func (s *stream) write(done bool) { s.done = done }",
			want:    1,
		},
		{
			name:    "unlocking helper in condition invalidates both branches",
			close:   "s.mu.Lock(); if s.release() { s.done = true }",
			helpers: "func (s *stream) release() bool { s.mu.Unlock(); return true }",
			want:    1,
		},
		{
			name:    "short circuit helper may unlock",
			close:   "s.mu.Lock(); _ = s.flag && s.release(); s.done = true",
			helpers: "func (s *stream) release() bool { s.mu.Unlock(); return true }",
			want:    1,
		},
		{
			name:  "short circuit literal cannot establish lock",
			close: "_ = s.flag && func() bool { s.mu.Lock(); return true }(); s.done = true",
			want:  1,
		},
		{
			name:  "uncalled literal does not unlock caller mutex",
			close: "s.mu.Lock(); defer s.mu.Unlock(); _ = func() { s.mu.Unlock() }; s.done = true",
		},
		{
			name:  "literal loop back edge invalidates earlier access",
			close: "s.mu.Lock(); for s.flag { s.done = true; func() { s.mu.Unlock() }() }",
			want:  1,
		},
		{
			name:    "loop relock keeps accesses protected",
			close:   "for s.flag { s.mu.Lock(); s.done = true; s.release() }",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
		},
		{
			name:    "conditional relock cannot restore must held state",
			close:   "s.mu.Lock(); s.release(); if s.flag { s.mu.Lock() }; s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "locked helper access before unlock remains protected",
			close:   "s.mu.Lock(); s.write()",
			helpers: "func (s *stream) write() { s.done = true; s.mu.Unlock() }",
		},
		{
			name:  "locked literal access before unlock remains protected",
			close: "s.mu.Lock(); func() { s.done = true; s.mu.Unlock() }()",
		},
		{
			name:    "helper calls inside literals propagate unlock",
			close:   "s.mu.Lock(); func() { s.release() }(); s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "literals inside helper calls propagate unlock",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { func() { s.mu.Unlock() }() }",
			want:    1,
		},
		{
			name:    "deferred helper arguments run synchronously",
			close:   "s.mu.Lock(); defer s.ignore(s.release()); s.done = true",
			helpers: "func (s *stream) release() bool { s.mu.Unlock(); return true }; func (s *stream) ignore(bool) {}",
			want:    1,
		},
		{
			name:    "recursive helper cannot promise inherited lock",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { if s.flag { s.release() }; s.mu.Unlock() }",
			want:    1,
		},
		{
			name:    "recursive summary cannot be reused with inherited locks",
			next:    "s.recur(); s.mu.Lock(); defer s.mu.Unlock(); return !s.done",
			close:   "s.mu.Lock(); s.recur(); s.done = true",
			helpers: "func (s *stream) recur() { if s.flag { s.recur() } }",
			want:    1,
		},
		{
			name:    "mutually recursive summary cannot preserve inherited locks",
			close:   "s.mu.Lock(); s.first(); s.done = true",
			helpers: "func (s *stream) first() { if s.flag { s.second() } }; func (s *stream) second() { s.first() }",
			want:    1,
		},
		{
			name:  "goroutine directly unlocks caller mutex",
			close: "s.mu.Lock(); go s.mu.Unlock(); s.done = true",
			want:  1,
		},
		{
			name:    "goroutine helper unlocks caller mutex",
			close:   "s.mu.Lock(); go s.release(); s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:  "goroutine literal unlocks caller mutex",
			close: "s.mu.Lock(); go func() { s.mu.Unlock() }(); s.done = true",
			want:  1,
		},
		{
			name:  "goroutine parenthesized literal unlocks caller mutex",
			close: "s.mu.Lock(); go (func() { s.mu.Unlock() })(); s.done = true",
			want:  1,
		},
		{
			name:  "reader goroutine directly unlocks caller mutex",
			next:  "s.mu.Lock(); go s.mu.Unlock(); return !s.done",
			close: "s.mu.Lock(); defer s.mu.Unlock(); s.done = true",
			want:  1,
		},
		{
			name:    "reader goroutine helper unlocks caller mutex",
			next:    "s.mu.Lock(); go s.release(); return !s.done",
			close:   "s.mu.Lock(); defer s.mu.Unlock(); s.done = true",
			helpers: "func (s *stream) release() { s.mu.Unlock() }",
			want:    1,
		},
		{
			name:  "reader goroutine literal unlocks caller mutex",
			next:  "s.mu.Lock(); go func() { s.mu.Unlock() }(); return !s.done",
			close: "s.mu.Lock(); defer s.mu.Unlock(); s.done = true",
			want:  1,
		},
		{
			name:    "goroutine helper deferred unlock invalidates caller mutex",
			close:   "s.mu.Lock(); go s.release(); s.done = true",
			helpers: "func (s *stream) release() { defer s.mu.Unlock() }",
			want:    1,
		},
		{
			name:  "goroutine literal deferred unlock invalidates caller mutex",
			close: "s.mu.Lock(); go func() { defer s.mu.Unlock() }(); s.done = true",
			want:  1,
		},
		{
			name:    "helper launches unlocking goroutine",
			close:   "s.mu.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { go s.mu.Unlock() }",
			want:    1,
		},
		{
			name:  "conditional goroutine unlock invalidates merged state",
			close: "s.mu.Lock(); if s.flag { go s.mu.Unlock() }; s.done = true",
			want:  1,
		},
		{
			name:    "goroutine arguments run synchronously",
			close:   "s.mu.Lock(); go s.ignore(s.release()); s.done = true",
			helpers: "func (s *stream) release() bool { s.mu.Unlock(); return true }; func (s *stream) ignore(bool) {}",
			want:    1,
		},
		{
			name:  "goroutine lock cannot establish caller lock",
			close: "go s.mu.Lock(); s.done = true",
			want:  1,
		},
		{
			name:    "goroutine without unlock preserves caller mutex",
			close:   "s.mu.Lock(); defer s.mu.Unlock(); go s.ignore(); s.done = true",
			helpers: "func (s *stream) ignore() {}",
		},
		{
			name:  "goroutine literal without unlock preserves caller mutex",
			close: "s.mu.Lock(); defer s.mu.Unlock(); go func() {}(); s.done = true",
		},
		{
			name:  "goroutine unlock of different mutex preserves caller mutex",
			close: "s.mu.Lock(); defer s.mu.Unlock(); s.other.Lock(); go s.other.Unlock(); s.done = true",
		},
		{
			name:    "goroutine helper unlock of different mutex preserves caller mutex",
			close:   "s.mu.Lock(); defer s.mu.Unlock(); s.other.Lock(); go s.release(); s.done = true",
			helpers: "func (s *stream) release() { s.other.Unlock() }",
		},
		{
			name:  "caller deferred literal does not unlock immediately",
			close: "s.mu.Lock(); defer func() { s.mu.Unlock() }(); s.done = true",
		},
		{
			name:    "different mutex unlock preserves caller mutex",
			close:   "s.mu.Lock(); defer s.mu.Unlock(); s.other.Lock(); s.release(); s.done = true",
			helpers: "func (s *stream) release() { s.other.Unlock() }",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			next := tc.next
			if next == "" {
				next = "s.mu.Lock(); defer s.mu.Unlock(); return !s.done"
			}
			p := streamCloseTestPass(t, `package p
import "sync"
type stream struct { mu, other sync.Mutex; done, flag bool }
func (s *stream) Next() bool { `+next+` }
func (s *stream) Close() { `+tc.close+` }
`+tc.helpers)
			NewLintStreamCloseSafety().Check(p)
			assert.Len(t, p.Offenses(), tc.want, "%+v", p.Offenses())
			for _, offense := range p.Offenses() {
				assert.Contains(t, offense.Message, "Close accesses done")
			}
		})
	}
}

func TestStreamCloseSafetyNestedCallLockState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "wrapped helper unlock",
			src: `type inner struct { mu sync.Mutex; done bool }
func (s *inner) release() { s.mu.Unlock() }
type stream struct { in inner }
func (s *stream) Next() bool { s.in.mu.Lock(); defer s.in.mu.Unlock(); return !s.in.done }
func (s *stream) Close() { s.in.mu.Lock(); s.in.release(); s.in.done = true }`,
		},
		{
			name: "promoted helper unlock",
			src: `type inner struct { sync.Mutex; done bool }
func (s *inner) release() { s.Unlock() }
type stream struct { inner }
func (s *stream) Next() bool { s.Lock(); defer s.Unlock(); return !s.done }
func (s *stream) Close() { s.Lock(); s.release(); s.done = true }`,
		},
		{
			name: "generic helper unlock",
			src: `type state[T any] struct { mu sync.Mutex; done T }
func (s *state[T]) release() { s.mu.Unlock() }
type stream struct { state[bool] }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); s.release(); s.done = true }`,
		},
		{
			name: "goroutine directly releases reader lock",
			src: `type stream struct { mu sync.RWMutex; done bool }
func (s *stream) Next() bool { s.mu.RLock(); go s.mu.RUnlock(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
		{
			name: "goroutine helper releases reader lock",
			src: `type stream struct { mu sync.RWMutex; done bool }
func (s *stream) release() { s.mu.RUnlock() }
func (s *stream) Next() bool { s.mu.RLock(); go s.release(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
		{
			name: "goroutine literal releases reader lock",
			src: `type stream struct { mu sync.RWMutex; done bool }
func (s *stream) Next() bool { s.mu.RLock(); go func() { s.mu.RUnlock() }(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
		{
			name: "goroutine read lock cannot establish caller lock",
			src: `type stream struct { mu sync.RWMutex; done bool }
func (s *stream) Next() bool { go s.mu.RLock(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
		{
			name: "reader helper read unlock",
			src: `type stream struct { mu sync.RWMutex; done bool }
func (s *stream) release() { s.mu.RUnlock() }
func (s *stream) Next() bool { s.mu.RLock(); s.release(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.done = true }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := streamCloseTestPass(t, "package p\nimport \"sync\"\n"+tc.src)
			NewLintStreamCloseSafety().Check(p)
			require.Len(t, p.Offenses(), 1)
			assert.Contains(t, p.Offenses()[0].Message, "done")
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

func TestStreamCloseSafetyUnlockCache(t *testing.T) {
	t.Parallel()
	p := streamCloseTestPass(t, `package p
import "sync"
type stream struct { mu sync.RWMutex; done bool }
func (s *stream) release() { s.mu.Unlock() }
func (s *stream) read() bool { return s.done }
func (s *stream) recur() { s.recur() }
func (s *stream) first() { s.second() }
func (s *stream) second() { s.first() }
func (s *stream) callback() { _ = func() { s.callback() } }
func (s *stream) caller() { s.callback() }
`)
	a := streamCloseTestAnalysis(p)
	method := func(name string) *types.Func {
		t.Helper()
		obj, _, _ := types.LookupFieldOrMethod(p.Program.Packages[0].Types.Scope().Lookup("stream").Type(), true, p.Program.Packages[0].Types, name)
		fn, ok := obj.(*types.Func)
		require.True(t, ok)
		return fn
	}
	visiting := make(map[*types.Func]bool)
	_, unlocked := a.accesses(method("release"), ".left", nil, visiting, false)
	require.Equal(t, map[string]bool{".left.mu": true}, unlocked)
	delete(unlocked, ".left.mu") // Returned maps must not mutate cached summaries.
	_, unlocked = a.accesses(method("release"), ".right", map[string]bool{".right.mu": false, ".other.mu": true}, visiting, false)
	assert.Equal(t, map[string]bool{".right.mu": true}, unlocked)
	assert.Equal(t, map[string]bool{".mu": true}, a.unlocks[method("release")])
	visiting[method("release")] = true
	_, unlocked = a.accesses(method("release"), ".right", map[string]bool{".other.mu": false}, visiting, false)
	assert.Equal(t, map[string]bool{".other.mu": false}, unlocked, "recursive calls must bypass cached summaries")
	delete(visiting, method("release"))

	for _, exclusive := range []bool{false, true} {
		locks := map[string]bool{".right.mu": exclusive}
		a.accesses(method("read"), ".left", nil, visiting, false)
		found, _ := a.accesses(method("read"), ".right", locks, visiting, true)
		require.Len(t, found, 1)
		assert.Equal(t, ".right.done", found[0].path)
		assert.Equal(t, locks, found[0].locks)
	}

	for _, name := range []string{"recur", "first", "callback", "caller"} {
		fn := method(name)
		a.accesses(fn, "", nil, visiting, false)
		_, cached := a.unlocks[fn]
		assert.False(t, cached, "%s has context-dependent calls", name)
	}
	_, unlocked = a.accesses(method("recur"), ".right", map[string]bool{".right.mu": true, ".other.mu": false}, visiting, false)
	assert.Equal(t, map[string]bool{".right.mu": true, ".other.mu": true}, unlocked)
	assert.Empty(t, visiting)
}

func TestStreamCloseSafetyHelperChainScaling(t *testing.T) {
	for _, unlock := range []bool{false, true} {
		var smallAllocs float64
		for _, size := range []int{32, 128} {
			p := streamCloseTestPass(t, streamCloseHelperChain(size, unlock))
			a := streamCloseTestAnalysis(p)
			closeMethod, _, _ := types.LookupFieldOrMethod(p.Program.Packages[0].Types.Scope().Lookup("stream").Type(), true, nil, "Close")
			found, unlocked := a.accesses(closeMethod.(*types.Func), "", nil, make(map[*types.Func]bool), true)
			assert.Len(t, a.graphs, size+1)
			assert.Len(t, a.unlocks, size)
			assert.True(t, unlocked[".mu"]) // Close itself defers an unlock.
			var writes int
			for _, access := range found {
				if access.write && access.path == ".done" {
					writes++
					assert.Equal(t, !unlock, access.locks[".mu"])
				}
			}
			assert.Equal(t, 1, writes)

			// Allocation counts are deterministic; no wall-clock threshold is needed.
			allocs := testing.AllocsPerRun(3, func() {
				a := streamCloseAnalysis{methods: a.methods}
				a.accesses(closeMethod.(*types.Func), "", nil, make(map[*types.Func]bool), true)
			})
			if smallAllocs == 0 {
				smallAllocs = allocs
			} else {
				assert.Less(t, allocs, 6*smallAllocs, "four times the chain length should use roughly four times the allocations")
			}
		}
	}
}

func streamCloseTestAnalysis(p *prog.Pass) *streamCloseAnalysis {
	a := &streamCloseAnalysis{pass: p, methods: make(map[*types.Func]streamMethod)}
	for _, pkg := range p.Program.Packages {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil {
					obj := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
					a.methods[obj.Origin()] = streamMethod{fn: fn, info: pkg.TypesInfo}
				}
			}
		}
	}
	return a
}

func BenchmarkStreamCloseSafetyHelperChain(b *testing.B) {
	for _, unlock := range []bool{false, true} {
		for _, size := range []int{32, 128, 512} {
			b.Run(fmt.Sprintf("unlock=%t/size=%d", unlock, size), func(b *testing.B) {
				p := streamCloseTestPass(b, streamCloseHelperChain(size, unlock))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					pass := &prog.Pass{Cop: p.Cop, Program: p.Program}
					NewLintStreamCloseSafety().Check(pass)
				}
			})
		}
	}
}

func streamCloseHelperChain(size int, unlock bool) string {
	var src strings.Builder
	src.WriteString(`package p
import "sync"
type stream struct { mu sync.Mutex; done bool }
func (s *stream) Next() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.done }
func (s *stream) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.helper0() }
`)
	for i := range size {
		fmt.Fprintf(&src, "func (s *stream) helper%d() { ", i)
		if i+1 < size {
			fmt.Fprintf(&src, "s.helper%d()", i+1)
		} else {
			if unlock {
				src.WriteString("s.mu.Unlock(); ")
			}
			src.WriteString("s.done = true")
		}
		src.WriteString(" }\n")
	}
	return src.String()
}

func streamCloseTestPass(tb testing.TB, sources ...string) *prog.Pass {
	tb.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for i, src := range sources {
		file, err := parser.ParseFile(fset, string(rune('a'+i))+".go", src, 0)
		require.NoError(tb, err)
		files = append(files, file)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	config := types.Config{Importer: importer.Default()}
	pkg, err := config.Check("test", fset, files, info)
	require.NoError(tb, err)
	return &prog.Pass{Cop: NewLintStreamCloseSafety(), Program: &prog.Program{
		Fset: fset, Packages: []*packages.Package{{Types: pkg, TypesInfo: info, Syntax: files}},
	}}
}
