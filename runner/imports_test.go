package runner_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

func TestFileCopsResolveStandardLibraryImports(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		check     cop.Cop
		want      int
	}{
		{"os dot import", `import . "os";func f(){Exit(1)}`, cops.NewLintOsExit(), 1},
		{"os parenthesized dot import", `import . "os";func f(){(Exit)(1)}`, cops.NewLintOsExit(), 1},
		{"os shadowed dot import", `import . "os";func f(){Exit:=func(int){};Exit(1);_=Args}`, cops.NewLintOsExit(), 0},
		{"fmt dot import", `import . "fmt";func f(){Println("debug")}`, cops.NewLintFmtPrint(), 1},
		{"fmt parenthesized dot import", `import . "fmt";func f(){(Println)("debug")}`, cops.NewLintFmtPrint(), 1},
		{"fmt shadowed dot import", `import . "fmt";func f(){Println:=func(string){};Println("debug");_=Sprintf}`, cops.NewLintFmtPrint(), 0},
		{"errorf dot import", `import . "fmt";func f(err error)error{return Errorf("%v",err)}`, cops.NewLintWrapErrors(), 1},
		{"errorf parenthesized dot import", `import . "fmt";func f(err error)error{return (Errorf)("%v",err)}`, cops.NewLintWrapErrors(), 1},
		{"errorf shadowed dot import", `import . "fmt";func f(err error)error{Errorf:=func(string,...any)error{return nil};_=Sprintf;return Errorf("%v",err)}`, cops.NewLintWrapErrors(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "p.go"), []byte("package p;"+tc.src), 0o600))
			count, err := runner.New([]cop.Cop{tc.check}, config.DefaultConfig(), io.Discard).Run([]string{dir})
			require.NoError(t, err)
			assert.Equal(t, tc.want, count)
		})
	}
}
