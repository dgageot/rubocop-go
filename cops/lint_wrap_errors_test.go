package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/coptest"
)

func TestWrapErrorsFlagsVerbOnError(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("oops: %v", err) }
`
	offenses := coptest.RunTyped(t, NewLintWrapErrors(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/WrapErrors", offenses[0].CopName)
}

func TestWrapErrorsAllowsWrapVerb(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("oops: %w", err) }
`
	assert.Empty(t, coptest.RunTyped(t, NewLintWrapErrors(), src))
}

// An escaped percent followed by the letter w ("%%w") is a literal, not a
// wrapping verb: a real unwrapped error in the same call must still be flagged.
func TestWrapErrorsFlagsEscapedPercentW(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("done 50%%w: %v", err) }
`
	offenses := coptest.RunTyped(t, NewLintWrapErrors(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/WrapErrors", offenses[0].CopName)
}

func TestWrapErrorsIgnoresNonErrorArgs(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(name string) error { return fmt.Errorf("bad name %q", name) }
`
	assert.Empty(t, coptest.RunTyped(t, NewLintWrapErrors(), src))
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
	assert.Empty(t, coptest.RunTyped(t, NewLintWrapErrors(), src))
}

// %w already present: even with a second %v error arg, the chain is intact
// for at least one error, so the call is not flagged.
func TestWrapErrorsAllowsMixedWhenWPresent(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f(a, b error) error { return fmt.Errorf("%w and %v", a, b) }
`
	assert.Empty(t, coptest.RunTyped(t, NewLintWrapErrors(), src))
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
		assert.Empty(t, coptest.RunTyped(t, NewLintWrapErrors(), src), format)
	}
}
