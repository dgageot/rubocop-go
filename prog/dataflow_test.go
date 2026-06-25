package prog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/ssa"

	"github.com/dgageot/rubocop-go/prog"
)

// loadProgram writes a single-file module to a temp dir and loads it.
func loadProgram(t *testing.T, src string) *prog.Program {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.test\n\ngo 1.22\n")
	write(t, filepath.Join(dir, "main.go"), src)
	p, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	return p
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// findCallArg returns the first argument of the first call to a function
// whose name matches calleeName, searching every function in the program.
func findCallArg(p *prog.Program, calleeName string, argIdx int) ssa.Value {
	for _, fn := range p.AllFunctions() {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(*ssa.Call)
				if !ok {
					continue
				}
				if callee := call.Common().StaticCallee(); callee != nil && callee.Name() == calleeName {
					if argIdx < len(call.Common().Args) {
						return call.Common().Args[argIdx]
					}
				}
			}
		}
	}
	return nil
}

// Origins must follow a value backwards across a parameter and a call
// boundary to the literal it originated from.
func TestOrigins_AcrossCallAndParameter(t *testing.T) {
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skip("no filesystem")
	}
	p := loadProgram(t, `package main

func produce() int { return 42 }

func forward(x int) int { return x }

func sink(v int) { _ = v }

func main() {
	sink(forward(produce()))
}
`)

	arg := findCallArg(p, "sink", 0)
	require.NotNil(t, arg, "could not find sink's argument")

	origins := p.Origins(arg, prog.TraceOptions{})
	require.NotEmpty(t, origins)

	// The single origin must be the constant 42 returned by produce(),
	// reached by looking through sink's arg -> forward's return -> forward's
	// parameter x -> the actual argument produce() -> produce's return 42.
	foundConst := false
	for _, o := range origins {
		if c, ok := o.(*ssa.Const); ok && c.Value != nil && c.Value.String() == "42" {
			foundConst = true
		}
	}
	require.True(t, foundConst, "expected the constant 42 as an origin, got %v", origins)
}

// Redirect must let the walk see through an otherwise-opaque call to a
// chosen argument.
func TestOrigins_Redirect(t *testing.T) {
	p := loadProgram(t, `package main

func wrap(x int) int { return identity(x) }

func identity(x int) int { return x }

func sink(v int) { _ = v }

func main() {
	sink(wrap(7))
}
`)

	arg := findCallArg(p, "sink", 0)
	require.NotNil(t, arg)

	// Redirect every call to its first argument, short-circuiting the walk
	// at the first call instead of looking into the body.
	origins := p.Origins(arg, prog.TraceOptions{
		Redirect: func(c *ssa.Call) (ssa.Value, bool) {
			if len(c.Common().Args) > 0 {
				return c.Common().Args[0], true
			}
			return nil, false
		},
	})

	foundConst := false
	for _, o := range origins {
		if c, ok := o.(*ssa.Const); ok && c.Value != nil && c.Value.String() == "7" {
			foundConst = true
		}
	}
	require.True(t, foundConst, "expected the constant 7 via redirect, got %v", origins)
}
