package cops_test

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// run parses src and runs the cop on it, returning collected offenses.
func run(t *testing.T, c cop.Cop, filename, src string) []cop.Offense {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err)

	p := &cop.Pass{Cop: c, FileSet: fset, File: file}
	c.Check(p)
	return p.Offenses()
}

func TestLintOsExit_InHelper(t *testing.T) {
	src := `package sample

import "os"

func helper() {
	os.Exit(1)
}
`
	offenses := run(t, cops.NewLintOsExit(), "sample.go", src)

	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/OsExit", offenses[0].CopName)
	assert.Equal(t, 6, offenses[0].Pos.Line)
}

func TestLintOsExit_InMainIsAllowed(t *testing.T) {
	src := `package main

import "os"

func main() {
	os.Exit(0)
}
`
	offenses := run(t, cops.NewLintOsExit(), "main.go", src)
	assert.Empty(t, offenses)
}

func TestStyleErrorNaming_BadName(t *testing.T) {
	src := `package sample

func process() (int, error) { return 0, nil }

func caller() {
	_, e := process()
	_ = e
}
`
	offenses := run(t, cops.NewStyleErrorNaming(), "sample.go", src)

	require.Len(t, offenses, 1)
	assert.Equal(t, "Style/ErrorNaming", offenses[0].CopName)
	assert.Contains(t, offenses[0].Message, "'e'")
}

func TestStyleErrorNaming_GoodName(t *testing.T) {
	src := `package sample

func process() (int, error) { return 0, nil }

func caller() {
	_, err := process()
	_ = err
}
`
	offenses := run(t, cops.NewStyleErrorNaming(), "sample.go", src)
	assert.Empty(t, offenses)
}

func TestStyleEmptyFunc_EmptyBody(t *testing.T) {
	src := `package sample

func doNothing() {
}
`
	offenses := run(t, cops.NewStyleEmptyFunc(), "sample.go", src)

	require.Len(t, offenses, 1)
	assert.Equal(t, "Style/EmptyFunc", offenses[0].CopName)
	assert.Contains(t, offenses[0].Message, "doNothing")
}

func TestLintFmtPrint_InLibrary(t *testing.T) {
	src := `package mylib

import "fmt"

func Hello() {
	fmt.Println("debug")
	fmt.Printf("value: %d", 42)
}
`
	offenses := run(t, cops.NewLintFmtPrint(), "mylib.go", src)

	require.Len(t, offenses, 2)
	assert.Equal(t, "Lint/FmtPrint", offenses[0].CopName)
	assert.Contains(t, offenses[0].Message, "fmt.Println")
	assert.Contains(t, offenses[1].Message, "fmt.Printf")
}

func TestLintFmtPrint_InMainIsAllowed(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	offenses := run(t, cops.NewLintFmtPrint(), "main.go", src)
	assert.Empty(t, offenses)
}

func TestLintFmtPrint_FmtErrorfIsAllowed(t *testing.T) {
	src := `package mylib

import "fmt"

func Wrap() error {
	return fmt.Errorf("bad: %w", nil)
}
`
	offenses := run(t, cops.NewLintFmtPrint(), "mylib.go", src)
	assert.Empty(t, offenses)
}

func TestStyleEmptyFunc_NonEmptyBody(t *testing.T) {
	src := `package sample

func doSomething() {
	println("hello")
}
`
	offenses := run(t, cops.NewStyleEmptyFunc(), "sample.go", src)
	assert.Empty(t, offenses)
}
