package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestConstructorPurityConstructorSignatures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"New", `func New() int { go work(); return 0 }`, 1},
		{"New followed by uppercase", `func NewClient() *Client { go work(); return nil }`, 1},
		{"named result", `func NewClient() (client *Client) { go work(); return }`, 1},
		{"multiple results", `func NewClient() (*Client, error) { go work(); return nil, nil }`, 1},
		{"generic constructor", `func NewClient[T any]() *T { go work(); return nil }`, 1},
		{"lowercase suffix", `func Newest() int { go work(); return 0 }`, 0},
		{"lowercase prefix", `func newClient() int { go work(); return 0 }`, 0},
		{"underscore suffix", `func New_Client() int { go work(); return 0 }`, 0},
		{"digit suffix", `func New2() int { go work(); return 0 }`, 0},
		{"no result", `func NewClient() { go work() }`, 0},
		{"value receiver", `func (Client) New() int { go work(); return 0 }`, 0},
		{"pointer receiver", `func (*Client) NewClient() int { go work(); return 0 }`, 0},
		{"start method", `func (*Client) Start() { go work() }`, 0},
		{"bodyless constructor", `func NewClient() *Client`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offenses := coptest.Run(t, NewLintConstructorPurity(), "package p\n"+tc.src)
			assert.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/ConstructorPurity", offense.CopName)
				assert.Equal(t, cop.Error, offense.Severity)
			}
		})
	}
}

func TestConstructorPurityExecutionBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"plain initialization", `return 0`, 0},
		{"direct spawn", `go work(); return 0`, 1},
		{"conditional spawn", `if ready { go work() }; return 0`, 1},
		{"loop spawn", `for range 2 { go work() }; return 0`, 1},
		{"returned closure", `return func() { go work() }`, 0},
		{"stored closure", `start := func() { go work() }; return start`, 0},
		{"callback argument", `register(func() { go work() }); return 0`, 0},
		{"indirect helper", `startWorker(); return 0`, 0},
		{"immediate closure", `func() { go work() }(); return 0`, 1},
		{"parenthesized immediate closure", `(func() { go work() })(); return 0`, 1},
		{"nested immediate closures", `func() { func() { go work() }() }(); return 0`, 1},
		{"stored closure inside immediate closure", `func() { _ = func() { go work() } }(); return 0`, 0},
		{"immediate argument to immediate closure", `func(int) {}(func() int { go work(); return 0 }()); return 0`, 1},
		{"deferred closure executes before return", `defer func() { go work() }(); return 0`, 1},
		{"stored closure inside deferred closure", `defer func() { _ = func() { go work() } }(); return 0`, 0},
		{"deferred closure inside stored closure", `return func() { defer func() { go work() }() }`, 0},
		{"spawned closure is reported once", `go func() { go work() }(); return 0`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\nfunc NewClient() any { " + tc.body + " }"
			assert.Len(t, coptest.Run(t, NewLintConstructorPurity(), src), tc.want)
		})
	}
}

func TestConstructorPurityReportsEachSpawn(t *testing.T) {
	t.Parallel()
	src := `package p
func NewClient() int {
	go work()
	if ready {
		go work()
	}
	return 0
}`
	offenses := coptest.Run(t, NewLintConstructorPurity(), src)
	require.Len(t, offenses, 2)
	assert.Equal(t, 3, offenses[0].Pos.Line)
	assert.Equal(t, 5, offenses[1].Pos.Line)
	for _, offense := range offenses {
		assert.Contains(t, offense.Message, "constructor NewClient starts a goroutine")
		assert.Contains(t, offense.Message, "Start/Run")
	}
}
