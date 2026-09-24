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
