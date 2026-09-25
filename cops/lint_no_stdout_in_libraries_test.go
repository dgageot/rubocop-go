package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/coptest"
)

func TestNoStdoutInLibrariesFlagsFmtPrint(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f() { fmt.Printf("hello") }
`
	offenses := coptest.RunNamed(t, NewLintNoStdoutInLibraries(), "pkg/p/p.go", src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/NoStdoutInLibraries", offenses[0].CopName)
}

func TestNoStdoutInLibrariesFlagsExplicitStdout(t *testing.T) {
	t.Parallel()
	src := `package p
import (
	"fmt"
	"os"
)
func f() { fmt.Fprintln(os.Stdout, "hello") }
`
	assert.Len(t, coptest.RunNamed(t, NewLintNoStdoutInLibraries(), "pkg/p/p.go", src), 1)
}

func TestNoStdoutInLibrariesAllowsProvidedWriter(t *testing.T) {
	t.Parallel()
	src := `package p
import (
	"fmt"
	"io"
)
func f(out io.Writer) { fmt.Fprintln(out, "hello") }
`
	assert.Empty(t, coptest.RunNamed(t, NewLintNoStdoutInLibraries(), "pkg/p/p.go", src))
}

func TestNoStdoutInLibrariesAllowsTests(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func TestOutput() { fmt.Println("hello") }
`
	assert.Empty(t, coptest.RunNamed(t, NewLintNoStdoutInLibraries(), "pkg/p/p_test.go", src))
}

func TestNoStdoutInLibrariesAllowsMainPackages(t *testing.T) {
	t.Parallel()
	src := `package main
import "fmt"
func main() { fmt.Println("hello") }
`
	assert.Empty(t, coptest.RunNamed(t, NewLintNoStdoutInLibraries(), "pkg/gen/main.go", src))
}

func TestNoStdoutInLibrariesChecksCodeOutsidePkg(t *testing.T) {
	t.Parallel()
	src := `package p
import "fmt"
func f() { fmt.Println("hello") }
`
	assert.Len(t, coptest.RunNamed(t, NewLintNoStdoutInLibraries(), "cmd/root/root.go", src), 1)
}
