package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestNoContextField(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"named field", `type S struct { ctx context.Context }`, 1},
		{"exported field", `type S struct { Context context.Context }`, 1},
		{"embedded context", `type S struct { context.Context }`, 1},
		{"grouped fields", `type S struct { a, b context.Context }`, 1},
		{"multiple fields", `type S struct { a context.Context; b context.Context }`, 2},
		{"multiple declarations", `type (S struct { ctx context.Context }; T struct { ctx context.Context })`, 2},
		{"struct alias", `type S = struct { ctx context.Context }`, 1},
		{"empty struct", `type S struct {}`, 0},
		{"ordinary field", `type S struct { id string }`, 0},
		{"parameter", `func run(ctx context.Context) {}`, 0},
		{"pointer field excluded", `type S struct { ctx *context.Context }`, 0},
		{"container excluded", `type S struct { contexts []context.Context }`, 0},
		{"nested struct excluded", `type S struct { inner struct { ctx context.Context } }`, 0},
		{"local struct excluded", `func run() { type S struct { ctx context.Context } }`, 0},
		{"anonymous struct excluded", `var s struct { ctx context.Context }`, 0},
		{"import alias not resolved", `type S struct { ctx ctxpkg.Context }`, 0},
		{"type alias not resolved", `type Ctx = context.Context; type S struct { ctx Ctx }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, NewLintNoContextField(), "package p\n"+tc.src), tc.want)
		})
	}
}

func TestNoContextFieldDiagnostic(t *testing.T) {
	t.Parallel()
	offenses := coptest.Run(t, NewLintNoContextField(), `package p
import "context"
type S struct {
	ctx context.Context
}
`)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/NoContextField", offenses[0].CopName)
	assert.Equal(t, cop.Warning, offenses[0].Severity)
	assert.Equal(t, 4, offenses[0].Pos.Line)
	assert.Equal(t, 2, offenses[0].Pos.Column)
	assert.Equal(t, 21, offenses[0].End.Column)
	assert.Contains(t, offenses[0].Message, "pass it to each operation")
}
