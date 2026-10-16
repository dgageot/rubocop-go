package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorStringMatchingFlagsContains(t *testing.T) {
	t.Parallel()
	src := `package p
import "strings"
func f(err error) bool { return strings.Contains(err.Error(), "queue full") }
`
	offenses := runExtractedTypedCop(t, NewLintErrorStringMatching(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/ErrorStringMatching", offenses[0].CopName)
	assert.Contains(t, offenses[0].Message, "errors.Is/errors.As")
}

func TestErrorStringMatchingFlagsPrefixAndSuffix(t *testing.T) {
	t.Parallel()
	for _, function := range []string{"HasPrefix", "HasSuffix"} {
		src := `package p
import "strings"
func f(err error) bool { return strings.` + function + `(err.Error(), "failure") }
`
		require.Len(t, runExtractedTypedCop(t, NewLintErrorStringMatching(), src), 1, function)
	}
}

func TestErrorStringMatchingFlagsConcreteErrorType(t *testing.T) {
	t.Parallel()
	src := `package p
import "strings"
type customError struct{}
func (*customError) Error() string { return "failure" }
func f(err *customError) bool { return strings.Contains(err.Error(), "failure") }
`
	require.Len(t, runExtractedTypedCop(t, NewLintErrorStringMatching(), src), 1)
}

func TestErrorStringMatchingAllowsOrdinaryErrorRendering(t *testing.T) {
	t.Parallel()
	src := `package p
func f(err error) string { return err.Error() }
`
	assert.Empty(t, runExtractedTypedCop(t, NewLintErrorStringMatching(), src))
}

func TestErrorStringMatchingAllowsNonErrorErrorMethod(t *testing.T) {
	t.Parallel()
	src := `package p
import "strings"
type response struct{}
func (response) Error(int) string { return "failure" }
func f(r response) bool { return strings.Contains(r.Error(1), "failure") }
`
	assert.Empty(t, runExtractedTypedCop(t, NewLintErrorStringMatching(), src))
}

func TestErrorStringMatchingAllowsStrings(t *testing.T) {
	t.Parallel()
	src := `package p
import "strings"
func f(message string) bool { return strings.Contains(message, "failure") }
`
	assert.Empty(t, runExtractedTypedCop(t, NewLintErrorStringMatching(), src))
}

func TestErrorStringMatchingResolvedCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{"renamed import", `import text "strings"; func f(err error) bool { return text.Contains(err.Error(), "failure") }`, 1},
		{"dot import", `import . "strings"; func f(err error) bool { return HasPrefix(err.Error(), "failure") }`, 1},
		{"unrelated method", `type matcher struct{}; func (matcher) Contains(a, b string) bool { return a == b }; func f(err error, strings matcher) bool { return strings.Contains(err.Error(), "failure") }`, 0},
		{"unrelated method result", `type matcher struct{}; func (matcher) Contains(a, b string) string { return a + b }; func f(err error, strings matcher) string { return strings.Contains(err.Error(), "failure") }`, 0},
		{"unrelated function field", `func f(err error, strings struct{ Contains func(string, string) bool }) bool { return strings.Contains(err.Error(), "failure") }`, 0},
		{"parenthesized API", `import "strings"; func f(err error) bool { return (strings.HasSuffix)(err.Error(), "failure") }`, 1},
		{"parenthesized error call", `import "strings"; func f(err error) bool { return strings.Contains(((err.Error())), "failure") }`, 1},
		{"parenthesized error method", `import "strings"; func f(err error) bool { return strings.Contains(((err.Error))(), "failure") }`, 1},
		{"implicit pointer receiver", `import "strings"; type E struct{}; func (*E) Error() string { return "failure" }; func f(err E) bool { return strings.Contains(err.Error(), "failure") }`, 1},
		{"addressable field", `import "strings"; type E struct{}; func (*E) Error() string { return "failure" }; type S struct{ err E }; func f(s *S) bool { return strings.Contains(s.err.Error(), "failure") }`, 1},
		{"promoted pointer receiver", `import "strings"; type E struct{}; func (*E) Error() string { return "failure" }; type S struct{ E }; func f(s S) bool { return strings.Contains(s.Error(), "failure") }`, 1},
		{"generic receiver", `import "strings"; func f[T error](err T) bool { return strings.Contains(err.Error(), "failure") }`, 1},
		{"variadic nonerror method", `import "strings"; type E struct{}; func (E) Error(...int) string { return "failure" }; func f(err E) bool { return strings.Contains(err.Error(), "failure") }`, 0},
		{"function field not method", `import "strings"; type E struct{ Error func() string }; func f(err E) bool { return strings.Contains(err.Error(), "failure") }`, 0},
		{"method expression", `import "strings"; type E struct{}; func (E) Error() string { return "failure" }; func f(err E) bool { return strings.Contains(E.Error(err), "failure") }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Len(t, runExtractedTypedCop(t, NewLintErrorStringMatching(), "package p; "+tc.source), tc.want)
		})
	}
}
