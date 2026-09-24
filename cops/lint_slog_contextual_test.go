package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestSlogContextualLevels(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"Debug", "Info", "Warn", "Error"} {
		t.Run(level, func(t *testing.T) {
			src := `package p
import ("context"; "log/slog")
func f(ctx context.Context) {
	slog.` + level + `("message")
}`
			offenses := coptest.Run(t, NewLintSlogContextual(), src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/SlogContextual", offenses[0].CopName)
			assert.Equal(t, cop.Warning, offenses[0].Severity)
			assert.Equal(t, 4, offenses[0].Pos.Line)
			assert.Contains(t, offenses[0].Message, "slog."+level+"Context(ctx")
		})
	}
}

func TestSlogContextualContextSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"parameter", `func f(ctx context.Context) { slog.Info("message") }`, 1},
		{"named result", `func f() (ctx context.Context) { slog.Info("message"); return }`, 1},
		{"typed local", `func f() { var ctx context.Context; slog.Info("message"); _ = ctx }`, 1},
		{"initialized local", `func f() { var ctx = context.Background(); slog.Info("message"); _ = ctx }`, 1},
		{"cancel context", `func f() { ctx, cancel := context.WithCancel(context.Background()); defer cancel(); slog.Info("message"); _ = ctx }`, 1},
		{"request context", `func f(req *http.Request) { ctx := req.Context(); slog.Info("message"); _ = ctx }`, 1},
		{"no context", `func f() { slog.Info("message") }`, 0},
		{"blank parameter", `func f(_ context.Context) { slog.Info("message") }`, 0},
		{"discarded context", `func f() { _, cancel := context.WithCancel(context.Background()); defer cancel(); slog.Info("message") }`, 0},
		{"contextual helpers", `func f(ctx context.Context) {
	slog.DebugContext(ctx, "message"); slog.InfoContext(ctx, "message")
	slog.WarnContext(ctx, "message"); slog.ErrorContext(ctx, "message")
	slog.Log(ctx, slog.LevelInfo, "message"); slog.LogAttrs(ctx, slog.LevelInfo, "message")
}`, 0},
		{"logger methods are out of scope", `func f(ctx context.Context, logger *slog.Logger) {
	logger.Debug("message"); logger.Info("message"); logger.Warn("message"); logger.Error("message")
	slog.Default().Info("message")
}`, 0},
		{"bodyless function", `func f(context.Context)`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\nimport (\"context\"; \"log/slog\"; \"net/http\")\n" + tc.src
			assert.Len(t, coptest.Run(t, NewLintSlogContextual(), src), tc.want)
		})
	}
}

func TestSlogContextualClosures(t *testing.T) {
	t.Parallel()
	src := `package p
import ("context"; "log/slog")
func withContext(ctx context.Context) {
	defer func() { slog.Error("deferred") }()
	_ = func() { slog.Warn("captured") }
}
func withoutContext() {
	slog.Info("outer")
	_ = func() {
		ctx := context.Background()
		slog.Info("inner")
		_ = ctx
	}
	_ = func() { slog.Info("sibling") }
}`
	offenses := coptest.Run(t, NewLintSlogContextual(), src)
	require.Len(t, offenses, 3)
	assert.Equal(t, 4, offenses[0].Pos.Line)
	assert.Equal(t, 5, offenses[1].Pos.Line)
	assert.Equal(t, 11, offenses[2].Pos.Line)
}

func TestSharedAdvisoryFactoriesScopesAndFreshInstances(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		new  func(...cop.FuncOption) *cop.Func
		src  string
	}{
		{"slog", NewLintSlogContextual, `func f(ctx context.Context) { slog.Info("message") }`},
		{"purity", NewLintConstructorPurity, `func New() int { go work(); return 0 }`},
		{"network", NewLintConstructorNetworkIO, `func New() error { _, err := net.Dial("tcp", "localhost:80"); return err }`},
		{"mutex", NewLintDeferMutexUnlock, `func f() { mu.Lock(); work(); mu.Unlock() }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\n" + tc.src
			scoped := tc.new(cop.WithScope(cop.And(cop.UnderDir("pkg/client"), cop.Not(func(p *cop.Pass) bool {
				return p.IsTestFile()
			}))))
			fresh := tc.new()
			require.NotSame(t, scoped, fresh)
			require.Len(t, coptest.RunNamed(t, scoped, "pkg/client/client.go", src), 1)
			assert.Empty(t, coptest.RunNamed(t, scoped, "pkg/other/client.go", src))
			assert.Empty(t, coptest.RunNamed(t, scoped, "pkg/client/client_test.go", src))
			require.Len(t, coptest.RunNamed(t, fresh, "pkg/other/client_test.go", src), 1)
		})
	}
}
