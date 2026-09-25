package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestNoFatalOutsideMain(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Fatal", "Fatalf", "Fatalln"} {
		t.Run(name, func(t *testing.T) {
			src := "package p\nimport \"log\"\nfunc run() { log." + name + "(\"boom\") }"
			offenses := coptest.Run(t, NewLintNoFatalOutsideMain(), src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/NoFatalOutsideMain", offenses[0].CopName)
			assert.Equal(t, cop.Error, offenses[0].Severity)
			assert.Equal(t, 3, offenses[0].Pos.Line)
			assert.Greater(t, offenses[0].End.Column, offenses[0].Pos.Column)
			assert.Contains(t, offenses[0].Message, "outside package main")
		})
	}
}

func TestNoFatalOutsideMainBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"main function", `package main; func main() { log.Fatal("boom") }`, 0},
		{"main package helper", `package main; func run() { log.Fatal("boom") }`, 0},
		{"main package init", `package main; func init() { log.Fatal("boom") }`, 0},
		{"library init", `package p; func init() { log.Fatal("boom") }`, 1},
		{"library function named main", `package p; func main() { log.Fatal("boom") }`, 1},
		{"method", `package p; type S struct{}; func (S) run() { log.Fatal("boom") }`, 1},
		{"closure", `package p; var run = func() { log.Fatal("boom") }`, 1},
		{"deferred call", `package p; func run() { defer log.Fatal("boom") }`, 1},
		{"goroutine", `package p; func run() { go log.Fatal("boom") }`, 1},
		{"other termination APIs excluded", `package p; func run() { os.Exit(1); log.Panic("boom"); panic("boom") }`, 0},
		{"ordinary logging", `package p; func run() { log.Print("ok"); log.Printf("ok"); log.Println("ok") }`, 0},
		{"logger methods excluded", `package p; func run(logger *log.Logger) { logger.Fatal("boom"); log.Default().Fatal("boom") }`, 0},
		{"import alias not resolved", `package p; import logging "log"; func run() { logging.Fatal("boom") }`, 0},
		{"function reference excluded", `package p; var fatal = log.Fatal`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, NewLintNoFatalOutsideMain(), tc.src), tc.want)
		})
	}
}
