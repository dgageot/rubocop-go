package cops

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dgageot/rubocop-go/coptest"
)

func TestNonblockingChannelReplacement(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, src string
		want      int
	}{
		{"local", `func f(c chan int) { select { case c <- 1: default: <-c; c <- 1 } }`, 1},
		{"local allocation", `func f() { c := make(chan int, 1); select { case c <- 1: default: <-c; c <- 1 } }`, 1},
		{"stable selector", `type watcher struct { changes chan string }; func (w *watcher) refresh(branch string) { select { case w.changes <- branch: default: <-w.changes; w.changes <- branch } }`, 1},
		{"nested selector", `type watcher struct { state struct { changes chan int } }; func f(w *watcher) { select { case w.state.changes <- 1: default: <-w.state.changes; w.state.changes <- 1 } }`, 1},
		{"assigned receive", `func f(c chan int) { select { case c <- 1: default: _ = <-c; c <- 1 } }`, 1},
		{"comma ok receive", `func f(c chan int) { select { case c <- 1: default: _, _ = <-c; c <- 1 } }`, 1},
		{"declared receive", `func f(c chan int) { select { case c <- 1: default: old := <-c; c <- old } }`, 1},
		{"receive reassigns channel", `type ch chan ch; func f(c ch) { select { case c <- c: default: c = <-c; c <- c } }`, 0},
		{"receive reassigns selector", `type w struct { c chan *w }; func f(a *w) { select { case a.c <- a: default: a = <-a.c; a.c <- a } }`, 0},
		{"parentheses", `func f(c chan int) { select { case (c) <- 1: default: <-((c)); (c) <- 1 } }`, 1},
		{"default first", `func f(c chan int) { select { default: <-c; c <- 1; case c <- 1: } }`, 1},
		{"shadowed unrelated local", `func f(c chan int) { { c := make(chan int, 1); select { case c <- 1: default: <-c; c <- 1 } } }`, 1},
		{"no replacement", `func f(c chan int) { select { case c <- 1: default: } }`, 0},
		{"no default", `func f(c, other chan int) { select { case c <- 1: case <-other: <-c; c <- 1 } }`, 0},
		{"nonblocking drain", `func f(c chan int) { select { case c <- 1: default: select { case <-c: default: }; c <- 1 } }`, 0},
		{"nonblocking resend", `func f(c chan int) { select { case c <- 1: default: <-c; select { case c <- 1: default: } } }`, 0},
		{"unrelated drain", `func f(c, other chan int) { select { case c <- 1: default: <-other; c <- 1 } }`, 0},
		{"unrelated resend", `func f(c, other chan int) { select { case c <- 1: default: <-c; other <- 1 } }`, 0},
		{"different selector base", `type w struct { c chan int }; func f(a, b *w) { select { case a.c <- 1: default: <-b.c; a.c <- 1 } }`, 0},
		{"different field", `type w struct { a, b chan int }; func f(w *w) { select { case w.a <- 1: default: <-w.b; w.a <- 1 } }`, 0},
		{"shadowed channel", `func f(c chan int, other chan chan int) { select { case c <- 1: default: c := <-other; c <- 1 } }`, 0},
		{"shadowed selector base", `type w struct { c chan *w }; func f(a *w) { select { case a.c <- a: default: a := <-a.c; a.c <- a } }`, 0},
		{"call expression", `func f(get func() chan int) { select { case get() <- 1: default: <-get(); get() <- 1 } }`, 0},
		{"mutating send value", `type w struct { c chan int }; func f(w *w, mutate func() int) { select { case w.c <- mutate(): default: <-w.c; w.c <- 1 } }`, 0},
		{"receiving send value", `func f(c chan int) { select { case c <- <-c: default: <-c; c <- 1 } }`, 0},
		{"indexed channel", `func f(cs []chan int) { select { case cs[0] <- 1: default: <-cs[0]; cs[0] <- 1 } }`, 0},
		{"package global", `var c chan int; func f() { select { case c <- 1: default: <-c; c <- 1 } }`, 0},
		{"intervening work", `func f(c chan int, work func()) { select { case c <- 1: default: <-c; work(); c <- 1 } }`, 0},
		{"additional select arm", `func f(c, done chan int) { select { case c <- 1: case <-done: default: <-c; c <- 1 } }`, 0},
	}
	files := coptest.ProgramFiles{"go.mod": "module example.test\n\ngo 1.26\n"}
	for i, tc := range tests {
		files[fmt.Sprintf("case%d/sample.go", i)] = "package p\n" + tc.src
	}
	offenses := coptest.RunProgram(t, NewLintNonblockingChannelReplacement(), files)
	got := make(map[string]int)
	for _, o := range offenses {
		assert.Equal(t, "Lint/NonblockingChannelReplacement", o.CopName)
		assert.Greater(t, o.End.Offset, o.Pos.Offset)
		assert.Contains(t, o.Message, "another receiver")
		got[filepath.Base(filepath.Dir(o.Pos.Filename))]++
	}
	for i, tc := range tests {
		assert.Equal(t, tc.want, got[fmt.Sprintf("case%d", i)], tc.name)
	}
}

func TestNonblockingChannelReplacementSuppressions(t *testing.T) {
	testChannelSafetySuppressions(t, NewLintNonblockingChannelReplacement(), `package sample
func f(c chan int) { select { case c <- 1: default: <-c; c <- 1 } } // offense
`)
}
