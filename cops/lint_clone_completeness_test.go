package cops_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// typedPass writes src to a temp file, parses and type-checks it, then
// returns a *cop.Pass populated with type information.
func typedPass(t *testing.T, src string) *cop.Pass {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	require.NoError(t, err)

	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	cfg := &types.Config{Error: func(error) {}}
	pkg, _ := cfg.Check(dir, fset, []*ast.File{file}, info)

	return &cop.Pass{FileSet: fset, File: file, Info: info, Package: pkg}
}

func TestLintCloneCompleteness_MissingField(t *testing.T) {
	src := `package sample

type Config struct {
	Name   string
	Items  []string
	Labels map[string]string
}

func (c *Config) Clone() *Config {
	return &Config{
		Name: c.Name,
		// Items and Labels are NOT copied
	}
}
`
	c := &cops.LintCloneCompleteness{}
	offenses := c.Check(typedPass(t, src))

	require.Len(t, offenses, 2)
	assert.Contains(t, offenses[0].Message, "Items")
	assert.Contains(t, offenses[1].Message, "Labels")
}

func TestLintCloneCompleteness_AllFieldsCopied(t *testing.T) {
	src := `package sample

import "slices"
import "maps"

type Config struct {
	Name   string
	Items  []string
	Labels map[string]string
}

func (c *Config) Clone() *Config {
	return &Config{
		Name:   c.Name,
		Items:  slices.Clone(c.Items),
		Labels: maps.Clone(c.Labels),
	}
}
`
	c := &cops.LintCloneCompleteness{}
	offenses := c.Check(typedPass(t, src))

	assert.Empty(t, offenses)
}

func TestLintCloneCompleteness_PointerField(t *testing.T) {
	src := `package sample

type Inner struct {
	Value int
}

type Outer struct {
	Name  string
	Inner *Inner
}

func (o *Outer) Clone() *Outer {
	return &Outer{
		Name: o.Name,
		// Inner is not copied
	}
}
`
	c := &cops.LintCloneCompleteness{}
	offenses := c.Check(typedPass(t, src))

	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, "Inner")
}

func TestLintCloneCompleteness_EmbeddedStruct(t *testing.T) {
	src := `package sample

type Base struct {
	Tags []string
}

type Extended struct {
	Base
	Name string
}

func (e *Extended) Clone() *Extended {
	return &Extended{
		Name: e.Name,
		// Tags from embedded Base not copied
	}
}
`
	c := &cops.LintCloneCompleteness{}
	offenses := c.Check(typedPass(t, src))

	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, "Tags")
}

func TestLintCloneCompleteness_NoCloneMethod(t *testing.T) {
	src := `package sample

type Config struct {
	Items []string
}

func (c *Config) String() string {
	return "config"
}
`
	c := &cops.LintCloneCompleteness{}
	offenses := c.Check(typedPass(t, src))

	assert.Empty(t, offenses)
}

func TestLintCloneCompleteness_OnlyValueFields(t *testing.T) {
	src := `package sample

type Point struct {
	X int
	Y int
}

func (p *Point) Clone() *Point {
	return &Point{X: p.X, Y: p.Y}
}
`
	c := &cops.LintCloneCompleteness{}
	offenses := c.Check(typedPass(t, src))

	assert.Empty(t, offenses)
}
