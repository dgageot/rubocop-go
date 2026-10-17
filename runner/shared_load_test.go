package runner_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
)

func TestSharedProgramLoadMatchesDefaultDiagnostics(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeModule(t, dir, map[string]string{
		"go.mod": "module example.test\n\ngo 1.26\n",
		"p.go": `package p
import("context";"strings";"os/exec")
func New() *exec.Cmd { return exec.Command("echo") }
func f(ctx context.Context, s string) { for range strings.Split(s, ",") {} }
`,
		"p_test.go": `package p
func clone(s []int) []int { return append([]int(nil), s...) }
`,
		"ext_test.go": `package p_test
func clone(s []int) []int { return append([]int(nil), s...) }
`,
		"only/only_test.go": `package only
func clone(s []int) []int { return append([]int(nil), s...) }
`,
		"inactive/disabled.go": `//go:build never_enabled_tag

package inactive
import "os/exec"
func New() *exec.Cmd { return exec.Command("echo") }
`,
		"suppressed/p.go": `package suppressed
import "strings"
func f(s string) { for range strings.Split(s, ",") {} } //rubocop:disable Lint/SplitSeq
`,
	})
	run := func(shared bool) string {
		var out bytes.Buffer
		r := runner.New([]cop.Cop{runner.WithDirectoryTypes(cops.NewLintConstructorCommandExecFile())}, config.DefaultConfig(), &out).
			WithProgramCops([]prog.Cop{cops.NewLintSplitSeq(), prog.FromFileWithTests(cops.NewLintSlicesCloneFile())})
		if shared {
			r.WithSharedProgramLoad()
		}
		r.Reporter = runner.NewJSONReporter(&out)
		count, err := r.Run([]string{"."})
		require.NoError(t, err)
		assert.Equal(t, 6, count, out.String())
		return out.String()
	}
	assert.JSONEq(t, run(false), run(true))
}

func TestSharedProgramLoadStartsAfterFileCops(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeModule(t, dir, map[string]string{
		"go.mod": "module fixture\n\ngo 1.26\n",
		"p.go":   "package p\n",
	})
	fileCop := cop.New(cop.Meta{Name: "Test/Edit"}, func(*cop.Pass) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "p.go"), []byte("package p\nvar Added int\n"), 0o600))
	})
	seen := false
	programCop := prog.FromFile(cop.New(cop.Meta{Name: "Test/Read"}, func(p *cop.Pass) {
		assert.NotNil(t, p.Package.Scope().Lookup("Added"))
		seen = true
	}))
	var out bytes.Buffer
	r := runner.New([]cop.Cop{fileCop}, config.DefaultConfig(), &out).WithProgramCops([]prog.Cop{programCop}).WithSharedProgramLoad()
	_, err := r.Run([]string{"."})
	require.NoError(t, err)
	assert.True(t, seen)
}
