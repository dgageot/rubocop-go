package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestContextFirstParameter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"last parameter", `func run(id string, ctx context.Context) {}`, 1},
		{"middle parameter", `func run(id string, ctx context.Context, n int) {}`, 1},
		{"first parameter", `func run(ctx context.Context, id string) {}`, 0},
		{"only parameter", `func run(ctx context.Context) {}`, 0},
		{"no parameters", `func run() {}`, 0},
		{"no context", `func run(id string) {}`, 0},
		{"unnamed parameters", `func run(string, context.Context) {}`, 1},
		{"grouped parameters", `func run(a, b string, ctx context.Context) {}`, 1},
		{"grouped contexts", `func run(a, b context.Context) {}`, 0},
		{"first context determines order", `func run(ctx context.Context, id string, other context.Context) {}`, 0},
		{"reports first misplaced context only", `func run(id string, ctx context.Context, other context.Context) {}`, 1},
		{"method", `type S struct{}; func (*S) run(id string, ctx context.Context) {}`, 1},
		{"bodyless declaration", `func run(id string, ctx context.Context)`, 1},
		{"generic declaration", `func run[T any](id T, ctx context.Context) {}`, 1},
		{"result is not parameter", `func run(id string) context.Context { return nil }`, 0},
		{"function literal excluded", `var run = func(id string, ctx context.Context) {}`, 0},
		{"function type excluded", `type F func(string, context.Context)`, 0},
		{"interface excluded", `type I interface { Run(string, context.Context) }`, 0},
		{"import alias not resolved", `func run(id string, ctx ctxpkg.Context) {}`, 0},
		{"type alias not resolved", `type Ctx = context.Context; func run(id string, ctx Ctx) {}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, NewLintContextFirstParameter(), "package p\n"+tc.src), tc.want)
		})
	}
}

func TestContextFirstParameterDiagnostic(t *testing.T) {
	t.Parallel()
	offenses := coptest.Run(t, NewLintContextFirstParameter(), `package p
import "context"
func run(id string, ctx context.Context) {}
`)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/ContextFirstParameter", offenses[0].CopName)
	assert.Equal(t, cop.Convention, offenses[0].Severity)
	assert.Equal(t, 3, offenses[0].Pos.Line)
	assert.Equal(t, 21, offenses[0].Pos.Column)
	assert.Equal(t, 40, offenses[0].End.Column)
	assert.Contains(t, offenses[0].Message, "first parameter")
}
