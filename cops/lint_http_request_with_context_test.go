package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestHTTPRequestWithContext(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"request without context", `func run() { _, _ = http.NewRequest("GET", "/", nil) }`, 1},
		{"caller context", `func run(ctx context.Context) { _, _ = http.NewRequestWithContext(ctx, "GET", "/", nil) }`, 0},
		{"context origin not checked", `func run() { _, _ = http.NewRequestWithContext(context.Background(), "GET", "/", nil) }`, 0},
		{"later WithContext still reported", `func run(ctx context.Context) { req, _ := http.NewRequest("GET", "/", nil); _ = req.WithContext(ctx) }`, 1},
		{"later Clone still reported", `func run(ctx context.Context) { req, _ := http.NewRequest("GET", "/", nil); _ = req.Clone(ctx) }`, 1},
		{"closure", `var run = func() { _, _ = http.NewRequest("GET", "/", nil) }`, 1},
		{"package initializer", `var req, err = http.NewRequest("GET", "/", nil)`, 1},
		{"multiple calls", `func run() { http.NewRequest("GET", "/", nil); http.NewRequest("POST", "/", nil) }`, 2},
		{"other http APIs excluded", `func run() { http.Get("/"); http.Post("/", "text/plain", nil) }`, 0},
		{"httptest excluded", `var req = httptest.NewRequest("GET", "/", nil)`, 0},
		{"import alias not resolved", `var req, err = web.NewRequest("GET", "/", nil)`, 0},
		{"function reference excluded", `var newRequest = http.NewRequest`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, NewLintHTTPRequestWithContext(), "package p\n"+tc.src), tc.want)
		})
	}
}

func TestHTTPRequestWithContextDiagnostic(t *testing.T) {
	t.Parallel()
	offenses := coptest.Run(t, NewLintHTTPRequestWithContext(), `package p
import "net/http"
func run() {
	_, _ = http.NewRequest("GET", "/", nil)
}
`)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/HTTPRequestWithContext", offenses[0].CopName)
	assert.Equal(t, cop.Error, offenses[0].Severity)
	assert.Equal(t, 4, offenses[0].Pos.Line)
	assert.Equal(t, 9, offenses[0].Pos.Column)
	assert.Greater(t, offenses[0].End.Column, offenses[0].Pos.Column)
	assert.Contains(t, offenses[0].Message, "http.NewRequestWithContext")
}
