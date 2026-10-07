package cops_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
)

func TestLintFmtPrintResolvedCallees(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"parenthesized dot import", `import . "fmt";func f(){(Println)("debug")}`, 1},
		{"alias", `import format "fmt";func f(){format.Println("debug")}`, 1},
		{"dot import", `import . "fmt";func f(){Println("debug")}`, 1},
		{"shadowed fmt", `func f(){fmt:=struct{Println func(string)}{func(string){}};fmt.Println("debug")}`, 0},
		{"parenthesized", `import "fmt";func f(){(fmt.Printf)("debug")}`, 1},
		{"initializer", `import "fmt";var n,err=fmt.Println("debug")`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.RunProgram(t, prog.FromFile(cops.NewLintFmtPrint()), coptest.ProgramFiles{"p.go": "package p;" + tc.src}), tc.want)
		})
	}
}
