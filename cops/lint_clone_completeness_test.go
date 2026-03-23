package cops_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/dgageot/rubocop-go/cops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseAndTypeCheck writes src to a temp file, parses and type-checks it,
// then returns everything the cop needs.
func parseAndTypeCheck(t *testing.T, src string) (*token.FileSet, *ast.File, *types.Info, *types.Package) {
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

	return fset, file, info, pkg
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
	fset, file, info, pkg := parseAndTypeCheck(t, src)

	c := &cops.LintCloneCompleteness{}
	offenses := c.CheckTyped(fset, file, info, pkg)

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
	fset, file, info, pkg := parseAndTypeCheck(t, src)

	c := &cops.LintCloneCompleteness{}
	offenses := c.CheckTyped(fset, file, info, pkg)

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
	fset, file, info, pkg := parseAndTypeCheck(t, src)

	c := &cops.LintCloneCompleteness{}
	offenses := c.CheckTyped(fset, file, info, pkg)

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
	fset, file, info, pkg := parseAndTypeCheck(t, src)

	c := &cops.LintCloneCompleteness{}
	offenses := c.CheckTyped(fset, file, info, pkg)

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
	fset, file, info, pkg := parseAndTypeCheck(t, src)

	c := &cops.LintCloneCompleteness{}
	offenses := c.CheckTyped(fset, file, info, pkg)

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
	fset, file, info, pkg := parseAndTypeCheck(t, src)

	c := &cops.LintCloneCompleteness{}
	offenses := c.CheckTyped(fset, file, info, pkg)

	assert.Empty(t, offenses)
}
