package cops

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapErrorsFlagsVerbOnError(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("oops: %v", err) }
`
	offenses := runExtractedTypedCop(t, NewLintWrapErrors(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/WrapErrors", offenses[0].CopName)
}

func TestWrapErrorsAllowsWrapVerb(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("oops: %w", err) }
`
	assert.Empty(t, runExtractedTypedCop(t, NewLintWrapErrors(), src))
}

// An escaped percent followed by the letter w ("%%w") is a literal, not a
// wrapping verb: a real unwrapped error in the same call must still be flagged.
func TestWrapErrorsFlagsEscapedPercentW(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("done 50%%w: %v", err) }
`
	offenses := runExtractedTypedCop(t, NewLintWrapErrors(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/WrapErrors", offenses[0].CopName)
}

func TestWrapErrorsIgnoresNonErrorArgs(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(name string) error { return fmt.Errorf("bad name %q", name) }
`
	assert.Empty(t, runExtractedTypedCop(t, NewLintWrapErrors(), src))
}

// A struct field named Error that is a string (a common API-response shape)
// must not be mistaken for an error value.
func TestWrapErrorsIgnoresStringErrorField(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
type resp struct{ Error string }
func f(e resp) error { return fmt.Errorf("server said: %s", e.Error) }
`
	assert.Empty(t, runExtractedTypedCop(t, NewLintWrapErrors(), src))
}

// %w already present: even with a second %v error arg, the chain is intact
// for at least one error, so the call is not flagged.
func TestWrapErrorsAllowsMixedWhenWPresent(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(a, b error) error { return fmt.Errorf("%w and %v", a, b) }
`
	assert.Empty(t, runExtractedTypedCop(t, NewLintWrapErrors(), src))
}

// A %w verb carrying flags, width, precision, or an argument index is still a
// wrap verb and must not be flagged.
func TestWrapErrorsAllowsWrapVerbWithModifiers(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"%[1]w", "%-10w", "%+w"} {
		src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("oops: ` + format + `", err) }
`
		assert.Empty(t, runExtractedTypedCop(t, NewLintWrapErrors(), src), format)
	}
}

func TestWrapErrorsBuiltinAlias(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{"type Error = error", "type First = error; type Error = First"} {
		src := `package p
import "fmt"
` + declaration + `
func f(err Error) error { return fmt.Errorf("oops: %v", err) }
`
		require.Len(t, runExtractedTypedCop(t, NewLintWrapErrors(), src), 1)
	}
}

func TestWrapErrorsFormatOperands(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, format, args string
		want               int
	}{
		{"percent width", "%5%w: %v", "err", 1},
		{"percent flags", "%+%w: %v", "err", 1},
		{"percent index", "%[1]%w: %v", "err", 1},
		{"percent indexed width", "%[1]3% %[1]v", "err", 1},
		{"percent indexed precision", "%[1].2% %[1]v", "err", 1},
		{"escaped percent", "%%w: %v", "err", 1},
		{"repeated escaped percent", "%%%%w: %v", "err", 1},
		{"escaped then wrapped", "%%%w", "err", 0},
		{"wrapping width precision", "%10.3w", "err", 0},
		{"wrapping dynamic width", "%*[2]w", "10, err", 0},
		{"wrapping dynamic precision", "%.[1]*[2]w", "3, err", 0},
		{"unused indexed error", "%[2]s", `err, "message"`, 0},
		{"type only", "type %T", "err", 0},
		{"pointer only", "%p", "err", 0},
		{"unused extra error", "message", "err", 0},
		{"nonerror operand", "%s", `"message", err`, 0},
		{"indexed error", "%[2]v", `"message", err`, 1},
		{"index resets next operand", "%[2]s: %v", `nil, "message", err`, 1},
		{"index resets to type only", "%[2]s %[1]T", `err, "message"`, 0},
		{"type then render", "%T %[1]s", "err", 1},
		{"dynamic width", "%*v", "10, err", 1},
		{"dynamic precision", "%.*s", "3, err", 1},
		{"dynamic width and precision", "%[2]*.[1]*[3]v", "3, 10, err", 1},
		{"width operand only", "%*s", `err, "message"`, 0},
		{"precision operand only", "%.*s", `err, "message"`, 0},
		{"missing indexed operand", "%[3]v", "err", 0},
		{"missing closing bracket", "%[1v", "err", 0},
		{"invalid index", "%[0]v", "err", 0},
		{"signed index", "%[+1]v", "err", 0},
		{"invalid index width", "%[1]2v", "err", 0},
		{"invalid index precision", "%[1].2v", "err", 0},
		{"missing verb", "%", "err", 0},
		{"unicode verb then render", "%☃ %v", "1, err", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf(` + strconv.Quote(tc.format) + `, ` + tc.args + `) }
`
			require.Len(t, runExtractedTypedCop(t, NewLintWrapErrors(), src), tc.want)
		})
	}
}

func TestWrapErrorsResolvedFunctionAndConstant(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{"renamed import", `import format "fmt"; func f(err error) error { return format.Errorf("%v", err) }`, 1},
		{"dot import", `import . "fmt"; func f(err error) error { return Errorf("%v", err) }`, 1},
		{"parenthesized callee", `import "fmt"; func f(err error) error { return (fmt.Errorf)("%v", err) }`, 1},
		{"unrelated method", `type formatter struct{}; func (formatter) Errorf(_ string, err error) error { return err }; func f(err error, fmt formatter) error { return fmt.Errorf("%v", err) }`, 0},
		{"unrelated function field", `func f(err error, fmt struct{ Errorf func(string, ...any) error }) error { return fmt.Errorf("%v", err) }`, 0},
		{"constant format", `import "fmt"; const format = "%v"; func f(err error) error { return fmt.Errorf(format, err) }`, 1},
		{"constant expression", `import "fmt"; const prefix = "failure: "; func f(err error) error { return fmt.Errorf(prefix+"%s", err) }`, 1},
		{"parenthesized format", `import "fmt"; func f(err error) error { return fmt.Errorf((("%v")), err) }`, 1},
		{"variable format", `import "fmt"; func f(format string, err error) error { return fmt.Errorf(format, err) }`, 0},
		{"variadic operands", `import "fmt"; func f(args []any) error { return fmt.Errorf("%v", args...) }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Len(t, runExtractedTypedCop(t, NewLintWrapErrors(), "package p; "+tc.source), tc.want)
		})
	}
}
