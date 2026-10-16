package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestDeferMutexUnlockTerminalSections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		unlock string
	}{
		{"plain lock", `func f() { mu.Lock(); work(); mu.Unlock() }`, "mu.Unlock"},
		{"read lock", `func f() { mu.RLock(); work(); mu.RUnlock() }`, "mu.RUnlock"},
		{"selector receiver", `func (s *State) f() { s.state.mu.Lock(); work(); s.state.mu.Unlock() }`, "s.state.mu.Unlock"},
		{"bare return", `func f() { mu.Lock(); work(); mu.Unlock(); return }`, "mu.Unlock"},
		{"named return", `func f() (result int) { mu.Lock(); result = 1; mu.Unlock(); return }`, "mu.Unlock"},
		{"work in branch", `func f() { mu.Lock(); if ready { work() }; mu.Unlock() }`, "mu.Unlock"},
		{"work in loop", `func f() { mu.Lock(); for range 2 { work() }; mu.Unlock() }`, "mu.Unlock"},
		{"closure body", `func f() { _ = func() { mu.Lock(); work(); mu.Unlock() } }`, "mu.Unlock"},
		{"defer before lock preserves order", `func f() { defer cleanup(); mu.Lock(); work(); mu.Unlock() }`, "mu.Unlock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offenses := coptest.Run(t, NewLintDeferMutexUnlock(), "package p\n"+tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/DeferMutexUnlock", offenses[0].CopName)
			assert.Equal(t, cop.Warning, offenses[0].Severity)
			assert.Contains(t, offenses[0].Message, "use `defer "+tc.unlock+"()` immediately after locking")
		})
	}
}

func TestDeferMutexUnlockExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"already deferred", `func f() { mu.Lock(); defer mu.Unlock(); work() }`},
		{"read lock already deferred", `func f() { mu.RLock(); defer mu.RUnlock(); work() }`},
		{"short critical section", `func f() { mu.Lock(); work(); mu.Unlock(); moreWork() }`},
		{"work and return after unlock", `func f() int { mu.Lock(); work(); mu.Unlock(); result := moreWork(); return result }`},
		{"unlock and relock", `func f() { mu.Lock(); mu.Unlock(); moreWork(); mu.Lock(); defer mu.Unlock(); work() }`},
		{"nested unlock", `func f() { mu.Lock(); if ready { mu.Unlock(); return }; mu.Unlock() }`},
		{"nested lock scope", `func f() { mu.Lock(); if ready { mu.Lock(); mu.Unlock() }; mu.Unlock() }`},
		{"multiple releases", `func f() { mu.Lock(); mu.Unlock(); mu.Unlock() }`},
		{"missing release", `func f() { mu.Lock(); work() }`},
		{"different receiver", `func f() { mu.Lock(); other.Unlock() }`},
		{"wrong unlock method", `func f() { mu.RLock(); mu.Unlock() }`},
		{"indexed receiver", `func f() { locks[0].Lock(); locks[0].Unlock() }`},
		{"call receiver", `func f() { mutex().Lock(); mutex().Unlock() }`},
		{"nested block is out of scope", `func f() { if ready { mu.Lock(); work(); mu.Unlock() } }`},
		{"defer after lock changes order", `func f() { mu.Lock(); defer cleanup(); work(); mu.Unlock() }`},
		{"conditional defer after lock", `func f() { mu.Lock(); if ready { defer cleanup() }; mu.Unlock() }`},
		{"defer after unlock changes order", `func f() { mu.Lock(); work(); mu.Unlock(); defer cleanup() }`},
		{"late deferred unlock", `func f() { mu.Lock(); work(); defer mu.Unlock() }`},
		{"bodyless function", `func f()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, coptest.Run(t, NewLintDeferMutexUnlock(), "package p\n"+tc.src))
		})
	}
}

func TestDeferMutexUnlockReturnExpressions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		expr string
		want int
	}{
		{"literal", `42`, 1},
		{"local", `result`, 1},
		{"selector", `state.result`, 1},
		{"multiple values", `result, nil`, 1},
		{"unary expression", `-result`, 1},
		{"address", `&result`, 1},
		{"composite literal", `Result{Value: result}`, 1},
		{"slice literal", `[]int{1, result}`, 1},
		{"function call", `compute()`, 0},
		{"method call", `state.compute()`, 0},
		{"call among results", `result, compute()`, 0},
		{"receive", `<-results`, 0},
		{"call in composite literal", `Result{Value: compute()}`, 0},
		{"call in map key", `map[int]int{compute(): result}`, 0},
		{"call receiver", `state().result`, 0},
		{"binary expression conservatively skipped", `result + 1`, 0},
		{"index conservatively skipped", `results[0]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\nfunc f() any { mu.Lock(); work(); mu.Unlock(); return " + tc.expr + " }"
			assert.Len(t, coptest.Run(t, NewLintDeferMutexUnlock(), src), tc.want)
		})
	}
}

func TestDeferMutexUnlockControlFlow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"early handoff", `func f(handoff bool) { mu.Lock(); if handoff { return }; mu.Unlock() }`, 0},
		{"early read-lock handoff", `func f(handoff bool) { mu.RLock(); if handoff { return }; mu.RUnlock() }`, 0},
		{"early result evaluation", `func f(early bool) int { mu.Lock(); if early { return compute() }; mu.Unlock(); return 0 }`, 0},
		{"switch handoff", `func f() { mu.Lock(); switch state { case 1: return }; mu.Unlock() }`, 0},
		{"loop handoff", `func f() { mu.Lock(); for ready { return }; mu.Unlock() }`, 0},
		{"select handoff", `func f() { mu.Lock(); select { case <-done: return; default: }; mu.Unlock() }`, 0},
		{"jump bypasses lock", `func f(held bool) { if held { goto done }; mu.Lock(); done: work(); mu.Unlock() }`, 0},
		{"jump to compound statement bypasses lock", `func f(held bool) { if held { goto done }; mu.Lock(); done: if ready { work() }; mu.Unlock() }`, 0},
		{"backward jump reacquires lock", `func f(again bool) { start: work(); mu.Lock(); if again { goto start }; mu.Unlock() }`, 0},
		{"jump bypasses unlock", `func f(skip bool) { mu.Lock(); if skip { goto done }; mu.Unlock(); done: return }`, 0},
		{"return before lock", `func f(skip bool) { if skip { return }; mu.Lock(); work(); mu.Unlock() }`, 1},
		{"loop break stays in scope", `func f() { mu.Lock(); for ready { if done { break }; work() }; mu.Unlock() }`, 1},
		{"loop continue stays in scope", `func f() { mu.Lock(); for ready { if skip { continue }; work() }; mu.Unlock() }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, NewLintDeferMutexUnlock(), "package p\n"+tc.src), tc.want)
		})
	}
}

func TestDeferMutexUnlockClosureScopes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"stored defer", `func f() { mu.Lock(); unused := func() { defer cleanup() }; _ = unused; work(); mu.Unlock() }`, 1},
		{"stored lock operations", `func f() { mu.Lock(); unused := func() { mu.Unlock(); work(); mu.Lock() }; _ = unused; work(); mu.Unlock() }`, 1},
		{"stored read-lock operations", `func f() { mu.RLock(); unused := func() { mu.RUnlock(); work(); mu.RLock() }; _ = unused; mu.RUnlock() }`, 1},
		{"stored return", `func f() { mu.Lock(); unused := func() { return }; _ = unused; mu.Unlock() }`, 1},
		{"immediate defer", `func f() { mu.Lock(); func() { defer cleanup() }(); work(); mu.Unlock() }`, 1},
		{"parenthesized immediate defer", `func f() { mu.Lock(); (func() { defer cleanup() })(); mu.Unlock() }`, 1},
		{"immediate return", `func f() { mu.Lock(); func() { if ready { return }; work() }(); mu.Unlock() }`, 1},
		{"identifier callback defer", `func f() { mu.Lock(); run(func() { defer cleanup() }); mu.Unlock() }`, 1},
		{"selector callback defer", `func f() { mu.Lock(); exec.Run(func() { defer cleanup() }); mu.Unlock() }`, 1},
		{"selector callback return", `func f() { mu.Lock(); exec.Run(func() { return }); mu.Unlock() }`, 1},
		{"identifier callback lock operations", `func f() { mu.Lock(); run(func() { mu.Unlock(); work(); mu.Lock() }); mu.Unlock() }`, 0},
		{"selector callback lock operations", `func f() { mu.Lock(); exec.Run(func() { mu.Unlock(); work(); mu.Lock() }); mu.Unlock() }`, 0},
		{"parenthesized callback lock operations", `func f() { mu.Lock(); exec.Run((func() { mu.Unlock(); work(); mu.Lock() })); mu.Unlock() }`, 0},
		{"selector callback read-lock operations", `func f() { mu.RLock(); exec.Run(func() { mu.RUnlock(); work(); mu.RLock() }); mu.RUnlock() }`, 0},
		{"immediate lock operations", `func f() { mu.Lock(); func() { mu.Unlock(); work(); mu.Lock() }(); mu.Unlock() }`, 0},
		{"parenthesized immediate lock operations", `func f() { mu.Lock(); (func() { mu.Unlock(); work(); mu.Lock() })(); mu.Unlock() }`, 0},
		{"immediate deferred unlock", `func f() { mu.Lock(); func() { defer mu.Unlock() }(); mu.Unlock() }`, 0},
		{"deferred immediate unlock", `func f() { mu.Lock(); defer func() { mu.Unlock() }(); mu.Unlock() }`, 0},
		{"stored closure with direct outer defer", `func f() { mu.Lock(); unused := func() { mu.Unlock() }; _ = unused; defer cleanup(); mu.Unlock() }`, 0},
		{"goroutine unlock", `func f() { mu.Lock(); go func() { mu.Unlock() }(); mu.Unlock() }`, 0},
		{"goroutine-local defer", `func f() { mu.Lock(); go func() { defer cleanup() }(); mu.Unlock() }`, 1},
		{"stored closure inside immediate closure", `func f() { mu.Lock(); func() { unused := func() { mu.Unlock() }; _ = unused }(); mu.Unlock() }`, 1},
		{"stored callback inside callback", `func f() { mu.Lock(); exec.Run(func() { unused := func() { mu.Unlock() }; _ = unused }); mu.Unlock() }`, 1},
		{"closure scopes both reported", `func f() { mu.Lock(); unused := func() { mu.Lock(); work(); mu.Unlock() }; _ = unused; mu.Unlock() }`, 2},
		{"immediate closure own defer remains excluded", `func f() { mu.Lock(); func() { other.Lock(); defer cleanup(); other.Unlock() }(); mu.Unlock() }`, 1},
		{"immediate closure own return remains excluded", `func f() { mu.Lock(); func() { other.Lock(); if ready { return }; other.Unlock() }(); mu.Unlock() }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, NewLintDeferMutexUnlock(), "package p\n"+tc.src), tc.want)
		})
	}
}

func TestDeferMutexUnlockCallArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"identifier immediate argument", `func f() { mu.Lock(); run(func() int { mu.Unlock(); mu.Lock(); return 1 }()); mu.Unlock() }`},
		{"selector immediate argument", `func f() { mu.Lock(); exec.Run(func() int { mu.Unlock(); mu.Lock(); return 1 }()); mu.Unlock() }`},
		{"selector immediate receiver", `func f() { mu.Lock(); func() Executor { mu.Unlock(); mu.Lock(); return exec }().Run(); mu.Unlock() }`},
		{"nested selector argument", `func f() { mu.Lock(); exec.Run(makeCallback(func() { mu.Unlock(); mu.Lock() })); mu.Unlock() }`},
		{"terminal unlock argument", `func f() { mu.Lock(); mu.Unlock(func() int { mu.Unlock(); mu.Lock(); return 1 }()) }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, coptest.Run(t, NewLintDeferMutexUnlock(), "package p\n"+tc.src))
		})
	}
}

func TestDeferMutexUnlockReportsLockNotUnlock(t *testing.T) {
	t.Parallel()
	src := `package p
func f() {
	mu.Lock()
	work()
	mu.Unlock()
}`
	offenses := coptest.Run(t, NewLintDeferMutexUnlock(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, 3, offenses[0].Pos.Line)
	assert.Equal(t, 3, offenses[0].End.Line)
}

func TestDeferMutexUnlockNestedArgumentOperations(t *testing.T) {
	for _, body := range []string{
		`run(func() { (mu.Unlock)(); work(); (mu.Lock)() })`,
		`runMany([]func(){func() { mu.Unlock(); work(); mu.Lock() }})`,
		`run(func() { mu.Unlock(); mu.RLock() })`,
	} {
		src := `package p
import "sync"
var mu sync.RWMutex
func work() {}
func run(fn func()) { fn() }
func runMany(fns []func()) { for _, fn := range fns { fn() } }
func f() { mu.Lock(); ` + body + `; mu.Unlock() }
`
		assert.Empty(t, coptest.Run(t, NewLintDeferMutexUnlock(), src))
	}
}
