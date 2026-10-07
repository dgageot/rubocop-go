package cops_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestStyleErrorNaming_ReturnTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		offenses int
	}{
		{
			name: "boolean result",
			src: `func process() (int, bool) { return 0, true }
func caller() { _, ok := process(); _ = ok }`,
		},
		{
			name: "cancel function",
			src: `type CancelFunc func()
func process() (int, CancelFunc) { return 0, func() {} }
func caller() { _, cancel := process(); cancel() }`,
		},
		{
			name: "error alias bad name",
			src: `type Error = error
func process() (int, Error) { return 0, nil }
func caller() { _, e := process(); _ = e }`,
			offenses: 1,
		},
		{
			name: "error prefix",
			src: `func process() (int, error) { return 0, nil }
func caller() { _, errRead := process(); _ = errRead }`,
		},
		{
			name: "uppercase error prefix",
			src: `func process() (int, error) { return 0, nil }
func caller() { _, ErrRead := process(); _ = ErrRead }`,
		},
		{
			name: "discarded error",
			src: `func process() (int, error) { return 0, nil }
func caller() { n, _ := process(); _ = n }`,
		},
		{
			name: "reused error variable",
			src: `func process() (int, error) { return 0, nil }
func caller() { var e error; n, e := process(); _, _ = n, e }`,
			offenses: 1,
		},
		{
			name: "unresolved call",
			src:  `func caller() { _, cancel := missing(); _ = cancel }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := coptest.RunTyped(t, cops.NewStyleErrorNaming(), "package sample\n"+tc.src)
			assert.Len(t, offenses, tc.offenses)
		})
	}
}

func TestStyleErrorNaming_WithTimeout(t *testing.T) {
	t.Parallel()
	src := `package sample
import (
	"context"
	"testing"
	"time"
)
func test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_ = ctx
}
`
	c := cops.NewStyleErrorNaming()
	require.True(t, c.NeedsTypes())
	assert.Empty(t, coptest.RunTyped(t, c, src))
}

func TestStyleErrorNaming_WithoutTypes(t *testing.T) {
	t.Parallel()
	src := `package sample
func process() (int, error) { return 0, nil }
func caller() { _, e := process(); _ = e }
`
	assert.Empty(t, coptest.Run(t, cops.NewStyleErrorNaming(), src))
}
