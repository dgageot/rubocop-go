package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestConstructorCommandExec(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"command", `import "os/exec"; func New() *exec.Cmd { return exec.Command("echo") }`, 1},
		{"context", `import ("context"; "os/exec"); func NewCmd(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, "echo") }`, 1},
		{"alias", `import process "os/exec"; func NewCmd() *process.Cmd { return (process.Command)("echo") }`, 1},
		{"dot import", `import . "os/exec"; func NewCmd() *Cmd { return Command("echo") }`, 1},
		{"start", `import "os/exec"; func NewCmd(cmd *exec.Cmd) error { return cmd.Start() }`, 1},
		{"run", `import "os/exec"; func NewCmd(cmd *exec.Cmd) error { return cmd.Run() }`, 1},
		{"output", `import "os/exec"; func NewCmd(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }`, 1},
		{"combined output", `import "os/exec"; func NewCmd(cmd *exec.Cmd) ([]byte, error) { return cmd.CombinedOutput() }`, 1},
		{"chained", `import "os/exec"; func NewCmd() error { return exec.Command("echo").Run() }`, 2},
		{"method expression", `import "os/exec"; func NewCmd(cmd *exec.Cmd) error { return (*exec.Cmd).Run(cmd) }`, 1},
		{"promoted method", `import "os/exec"; type Cmd struct { *exec.Cmd }; func NewCmd(cmd Cmd) error { return cmd.Run() }`, 1},
		{"type alias", `import "os/exec"; type Cmd = exec.Cmd; func NewCmd(cmd *Cmd) error { return cmd.Run() }`, 1},
		{"immediate closure", `import "os/exec"; func NewCmd() *exec.Cmd { return (func() *exec.Cmd { return exec.Command("echo") })() }`, 1},
		{"deferred closure", `import "os/exec"; func NewCmd(cmd *exec.Cmd) *exec.Cmd { defer func() { _ = cmd.Run() }(); return cmd }`, 1},
		{"go receiver", `import "os/exec"; func NewCmd() any { go exec.Command("echo").Run(); return nil }`, 1},
		{"go argument", `import "os/exec"; func NewCmd() any { go func(cmd *exec.Cmd) {}(exec.Command("echo")); return nil }`, 1},
		{"go method argument", `import "os/exec"; func NewCmd() any { go (*exec.Cmd).Run(exec.Command("echo")); return nil }`, 1},
		{"unevaluated size", `import ("os/exec"; "unsafe"); func NewCmd() uintptr { return unsafe.Sizeof(exec.Command("echo")) }`, 0},
		{"unevaluated go argument", `import ("os/exec"; "unsafe"); func consume(uintptr) {}; func NewCmd() any { go consume(unsafe.Sizeof(exec.Command("echo"))); return nil }`, 0},
		{"generic unevaluated size", `import ("os/exec"; "unsafe"); func NewCmd[T any]() uintptr { return unsafe.Sizeof(struct { X T; C *exec.Cmd }{C: exec.Command("echo")}) }`, 0},
		{"generic unevaluated alignment", `import ("os/exec"; "unsafe"); func NewCmd[T any]() uintptr { return unsafe.Alignof(struct { X T; C *exec.Cmd }{C: exec.Command("echo")}) }`, 0},
		{"unevaluated offset", `import ("os/exec"; "unsafe"); func NewCmd() uintptr { return unsafe.Offsetof(struct { C *exec.Cmd }{C: exec.Command("echo")}.C) }`, 0},
		{"constant len", `import ("os/exec"; "unsafe"); func NewCmd() int { return len([1]uintptr{unsafe.Sizeof(exec.Command("echo"))}) }`, 0},
		{"evaluated len", `import "os/exec"; func NewCmd() int { return len([1]*exec.Cmd{exec.Command("echo")}) }`, 1},
		{"stored closure", `import "os/exec"; func NewCmd() func() *exec.Cmd { f := func() *exec.Cmd { return exec.Command("echo") }; return f }`, 0},
		{"returned closure", `import "os/exec"; func NewCmd() func() *exec.Cmd { return func() *exec.Cmd { return exec.Command("echo") } }`, 0},
		{"goroutine body", `import "os/exec"; func NewCmd() any { go func() { _ = exec.Command("echo") }(); return nil }`, 0},
		{"goroutine execution", `import "os/exec"; func NewCmd(cmd *exec.Cmd) *exec.Cmd { go cmd.Run(); return cmd }`, 0},
		{"unrelated methods", `type Service struct{}; func (Service) Run() {}; func (Service) Start() {}; func NewService() Service { s := Service{}; s.Run(); s.Start(); return s }`, 0},
		{"shadowed exec", `func NewCmd() any { exec := struct{ Command func(string) any }{func(string) any { return nil }}; return exec.Command("echo") }`, 0},
		{"non constructor", `import "os/exec"; func Run() *exec.Cmd { return exec.Command("echo") }`, 0},
		{"lowercase suffix", `import "os/exec"; func Newest() *exec.Cmd { return exec.Command("echo") }`, 0},
		{"method", `import "os/exec"; type S struct{}; func (S) NewCmd() *exec.Cmd { return exec.Command("echo") }`, 0},
		{"no result", `import "os/exec"; func NewCmd() { _ = exec.Command("echo") }`, 0},
		{"indirect helper", `import "os/exec"; func helper() *exec.Cmd { return exec.Command("echo") }; func NewCmd() *exec.Cmd { return helper() }`, 0},
		{"other exec APIs", `import "os/exec"; func NewCmd(cmd *exec.Cmd) error { _, _ = exec.LookPath("echo"); return cmd.Wait() }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newConstructorCommandExecFile(), "package p\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/ConstructorCommandExec", offense.CopName)
				assert.Equal(t, cop.Error, offense.Severity)
				assert.Contains(t, offense.Message, "calls os/exec.")
			}
		})
	}
}

func TestConstructorCommandExecWithoutTypes(t *testing.T) {
	t.Parallel()
	assert.Len(t, coptest.Run(t, newConstructorCommandExecFile(), `package p
func NewCmd() any { return exec.Command("echo") }`), 1)
	assert.Empty(t, coptest.Run(t, newConstructorCommandExecFile(), `package p
func NewService(s Service) error { return s.Run() }`))
	assert.Empty(t, coptest.Run(t, newConstructorCommandExecFile(), `package p
func NewCmd() *Cmd`))
}
