package cops_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
)

func TestLintOsExitRegressions(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"parenthesized dot import", `package p; import . "os";func f(){(Exit)(1)}`, 1},
		{"external declaration", `package p; func external()`, 0},
		{"import alias", `package p; import process "os"; func stop(){process.Exit(1)}`, 1},
		{"dot import", `package p; import . "os"; func stop(){Exit(1)}`, 1},
		{"shadowed identifier", `package p; func stop(){os:=struct{Exit func(int)}{func(int){}};os.Exit(1)}`, 0},
		{"initializer", `package p; import "os"; var _=func()int{os.Exit(1);return 0}()`, 1},
		{"nested main closure", `package main; import "os"; func main(){go func(){os.Exit(1)}();os.Exit(0)}`, 1},
		{"parenthesized callee", `package p; import "os"; func stop(){(os.Exit)(1)}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.RunProgram(t, prog.FromFile(cops.NewLintOsExit()), coptest.ProgramFiles{"p.go": tc.src}), tc.want)
		})
	}
}
