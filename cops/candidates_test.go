package cops

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
)

func TestConstructorCommandCandidate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`func New() int { return 0 }`, false},
		{`func run() { exec.Command("echo") }`, false},
		{`func (T) New() int { exec.Command("echo"); return 0 }`, false},
		{`func New() int { _ = exec.Command; return 0 }`, false},
		{`func New() int { exec.Command("echo"); return 0 }`, true},
		{`func NewThing() int { (exec.CommandContext)(ctx, "echo"); return 0 }`, true},
		{`func New() int { Command("echo"); return 0 }`, true},
		{`func New() int { cmd.Run(); return 0 }`, true},
		{`func New() int { (*exec.Cmd).Start(cmd); return 0 }`, true},
		{`func New() int { return cmd.Output() }`, true},
		{`func New() int { return cmd.CombinedOutput() }`, true},
		{`func New() int { return func() int { cmd.Run(); return 0 }() }`, true},
		{`func New() int { for range func(yield func(int)bool) { cmd.Start() } {}; return 0 }`, true},
		{`func New() int { return Command[int](0) }`, true},
		{`func New() int { return Command[int,string](0) }`, true},
		{`func New() int { return other.Run() }`, true},
	} {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			file, err := parser.ParseFile(token.NewFileSet(), "test.go", "package p\n"+tc.src, 0)
			require.NoError(t, err)
			assert.Equal(t, tc.want, constructorCommandCandidate(file))
		})
	}
}

func TestConstructorCommandCandidateKeepsSharedFindings(t *testing.T) {
	t.Parallel()
	files := coptest.ProgramFiles{
		"go.mod": "module example.test\n\ngo 1.27\n",
		"commands.go": `package p
import ex "os/exec"
type Cmd = ex.Cmd
func New(cmd *Cmd) *Cmd { _ = cmd.Run(); return cmd }
func NewContext() *Cmd { return (ex.Command)("echo") }
func NewMethod(cmd *Cmd) *Cmd { _ = (*ex.Cmd).Start(cmd); return cmd }
func NewClosure(cmd *Cmd) *Cmd { return func() *Cmd { _ = cmd.Output; _ = cmd.Start(); return cmd }() }
`,
		"plain.go": "package p\nfunc NewPlain() int { return 1 }\n",
	}
	check := prog.New(cop.Meta{Name: "Lint/TestCandidate"}, func(p *prog.Pass) {
		original := NewLintConstructorCommandExecFile()
		original.Candidate = nil
		filtered := NewLintConstructorCommandExecFile()
		for _, pkg := range p.Program.Packages {
			for _, file := range pkg.Syntax {
				before := &cop.Pass{Cop: original, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
				after := &cop.Pass{Cop: filtered, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
				original.Check(before)
				filtered.Check(after)
				assert.Equal(t, before.Offenses(), after.Offenses())
				for _, offense := range after.Offenses() {
					p.ReportOffense(offense)
				}
			}
		}
	})
	assert.Len(t, coptest.RunProgram(t, check, files), 4)
}

func TestSlogCandidateKeepsSharedFindings(t *testing.T) {
	t.Parallel()
	original := NewLintSlogContextual()
	original.Candidate = nil
	filtered := NewLintSlogContextual()
	for _, tc := range []struct {
		src  string
		want int
	}{
		{`func f(ctx context.Context) {}`, 0},
		{`func f(ctx context.Context) { slog.Info("message") }`, 1},
		{`func f(ctx context.Context) { (slog).Warn("message"); (slog.Error)("message") }`, 2},
		{`func f(ctx context.Context) { func() { slog.Debug("message") }() }`, 1},
		{`var callback = func(ctx context.Context) { slog.Info("message") }`, 1},
		{`func f() { slog.Info("message") }`, 0},
		{`func f(ctx context.Context) { logger.Info("message") }`, 0},
		{`func f(ctx context.Context) { slog.InfoContext(ctx, "message") }`, 0},
		{`func f(ctx context.Context) { slog.Info[int]("message") }`, 0},
		{`func f(ctx context.Context) { var slog logger; slog.Info("message") }`, 1},
	} {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			src := "package p\n" + tc.src
			before := coptest.Run(t, original, src)
			after := coptest.Run(t, filtered, src)
			assert.Equal(t, before, after)
			assert.Len(t, after, tc.want)
		})
	}
}
