package coptest

import (
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// ProgramFiles maps a repo-relative file path (e.g. "main.go" or
// "pkg/foo/foo.go") to its Go source. RunProgram writes them into a
// temporary module, loads the whole program, and runs a whole-program cop
// against it.
type ProgramFiles map[string]string

// RunProgram type-checks and lowers the given multi-file program to SSA,
// runs the whole-program cop c against it, and returns the offenses it
// produced.
//
// The files are written under a throwaway module named "example.test" so
// that intra-program imports resolve as "example.test/pkg/...". A go.mod is
// synthesized automatically unless the caller supplies one.
//
// RunProgram requires a working Go toolchain and module cache; it is meant
// for the handful of cops that genuinely need inter-procedural analysis.
func RunProgram(t *testing.T, c prog.Cop, files ProgramFiles) []cop.Offense {
	t.Helper()

	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		writeFile(t, filepath.Join(dir, "go.mod"), "module example.test\n\ngo 1.22\n")
	}
	for rel, src := range files {
		writeFile(t, filepath.Join(dir, rel), src)
	}

	program, err := prog.LoadDir(dir, "./...")
	if err != nil {
		t.Fatalf("coptest: loading program: %v", err)
	}

	p := &prog.Pass{Cop: c, Program: program}
	c.Check(p)
	return p.Offenses()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("coptest: mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("coptest: writing %s: %v", path, err)
	}
}

// OffenseLines returns the set of source lines an offense slice touches,
// keyed by filename base. Useful for asserting a whole-program cop fired on
// the expected lines without hard-coding column positions.
func OffenseLines(fset *token.FileSet, offenses []cop.Offense) map[string][]int {
	out := map[string][]int{}
	for _, o := range offenses {
		base := filepath.Base(o.Pos.Filename)
		out[base] = append(out[base], o.Pos.Line)
	}
	return out
}

// HaveGoToolchain reports whether a `go` binary is available on PATH. Tests
// that rely on RunProgram should skip when it returns false.
func HaveGoToolchain() bool {
	_, err := exec.LookPath("go")
	return err == nil
}
