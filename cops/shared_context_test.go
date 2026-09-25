package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestSharedContextAndFatalFactories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		new  func(...cop.FuncOption) *cop.Func
		src  string
	}{
		{"parameter", NewLintContextFirstParameter, `func run(id string, ctx context.Context) {}`},
		{"field", NewLintNoContextField, `type S struct { ctx context.Context }`},
		{"http", NewLintHTTPRequestWithContext, `var req, err = http.NewRequest("GET", "/", nil)`},
		{"fatal", NewLintNoFatalOutsideMain, `func run() { log.Fatal("boom") }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scoped := tc.new(cop.WithScope(cop.UnderDir("pkg/client")))
			fresh := tc.new()
			require.NotSame(t, scoped, fresh)
			assert.Nil(t, fresh.Scope)
			assert.False(t, fresh.NeedsTypes())
			src := "package p\n" + tc.src
			require.Len(t, coptest.RunNamed(t, scoped, "pkg/client/client.go", src), 1)
			assert.Empty(t, coptest.RunNamed(t, scoped, "pkg/other/client.go", src))
			assert.Empty(t, coptest.RunNamed(t, scoped, "pkg/client/client_test.go", src))
			assert.Empty(t, coptest.RunNamed(t, fresh, "client_test.go", src))
			assert.Empty(t, coptest.RunNamed(t, fresh, "client_test.go", "package p_test\n"+tc.src))
			for _, filename := range []string{"client.go", "pkg/other/client.go", "internal/testutil/client.go", "pkg/config/client.go"} {
				require.Len(t, coptest.RunNamed(t, fresh, filename, src), 1, filename)
			}
		})
	}
}
