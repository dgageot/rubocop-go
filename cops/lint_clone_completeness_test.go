package cops_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/coptest"
)

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
	offenses := coptest.RunTyped(t, cops.NewLintCloneCompleteness(), src)

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
	offenses := coptest.RunTyped(t, cops.NewLintCloneCompleteness(), src)
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
	offenses := coptest.RunTyped(t, cops.NewLintCloneCompleteness(), src)

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
	offenses := coptest.RunTyped(t, cops.NewLintCloneCompleteness(), src)

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
	offenses := coptest.RunTyped(t, cops.NewLintCloneCompleteness(), src)
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
	offenses := coptest.RunTyped(t, cops.NewLintCloneCompleteness(), src)
	assert.Empty(t, offenses)
}
