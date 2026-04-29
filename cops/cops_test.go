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

// pass parses src and returns a *cop.Pass for it.
func pass(t *testing.T, filename, src string) *cop.Pass {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	require.NoError(t, err)

	return &cop.Pass{FileSet: fset, File: file}
}

func TestLintOsExit_InHelper(t *testing.T) {
	src := `package sample

import "os"

func helper() {
	os.Exit(1)
}
`
	c := &cops.LintOsExit{}
	offenses := c.Check(pass(t, "sample.go", src))

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
	c := &cops.LintOsExit{}
	offenses := c.Check(pass(t, "main.go", src))

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
	c := &cops.StyleErrorNaming{}
	offenses := c.Check(pass(t, "sample.go", src))

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
	c := &cops.StyleErrorNaming{}
	offenses := c.Check(pass(t, "sample.go", src))

	assert.Empty(t, offenses)
}

func TestStyleEmptyFunc_EmptyBody(t *testing.T) {
	src := `package sample

func doNothing() {
}
`
	c := &cops.StyleEmptyFunc{}
	offenses := c.Check(pass(t, "sample.go", src))

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
	c := &cops.LintFmtPrint{}
	offenses := c.Check(pass(t, "mylib.go", src))

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
	c := &cops.LintFmtPrint{}
	offenses := c.Check(pass(t, "main.go", src))

	assert.Empty(t, offenses)
}

func TestLintFmtPrint_FmtErrorfIsAllowed(t *testing.T) {
	src := `package mylib

import "fmt"

func Wrap() error {
	return fmt.Errorf("bad: %w", nil)
}
`
	c := &cops.LintFmtPrint{}
	offenses := c.Check(pass(t, "mylib.go", src))

	assert.Empty(t, offenses)
}

func TestStyleEmptyFunc_NonEmptyBody(t *testing.T) {
	src := `package sample

func doSomething() {
	println("hello")
}
`
	c := &cops.StyleEmptyFunc{}
	offenses := c.Check(pass(t, "sample.go", src))

	assert.Empty(t, offenses)
}
