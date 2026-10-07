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

func TestLintCloneCompletenessRegressions(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"recursive embedding", `type Node struct{ *Node }; func(n *Node) Clone()*Node{return n}`, 1},
		{"mutual embedding", `type A struct{ *B }; type B struct{ *A }; func(a *A) Clone()*A{return a}`, 1},
		{"shallow literal", `type C struct{ Items []int }; func(c *C) Clone()*C{return &C{Items:c.Items}}`, 1},
		{"shallow assignment", `type C struct{ Items []int }; func(c *C) Clone()*C{d:=*c;d.Items=c.Items;return &d}`, 1},
		{"unrelated selector", `type C struct{ Items []int }; func(c *C) Clone()*C{x:=struct{Items int}{1};_=x.Items;return &C{}}`, 1},
		{"unrelated literal", `type C struct{ Items []int }; func(c *C) Clone()*C{_=struct{Items []int}{Items:[]int{1}};return &C{}}`, 1},
		{"private references", `type C struct{ items []int; labels map[string]int; value *int }; func(c *C) Clone()*C{return &C{}}`, 3},
		{"explicit copy", `type C struct{ items []int }; func(c *C) Clone()*C{d:=*c;d.items=make([]int,len(c.items));copy(d.items,c.items);return &d}`, 0},
		{"shallow embedded literal", `type Base struct{Items []int};type C struct{Base};func(c *C)Clone()*C{return &C{Base:c.Base}}`, 1},
		{"shallow embedded assignment", `type Base struct{Items []int};type C struct{Base};func(c *C)Clone()*C{d:=*c;d.Base=c.Base;return &d}`, 1},
		{"reslice", `type C struct{Items []int};func(c *C)Clone()*C{return &C{Items:c.Items[:]}}`, 1},
		{"address of receiver field", `type C struct{Value int;Ptr *int};func(c *C)Clone()*C{return &C{Ptr:&c.Value}}`, 1},
		{"distinct embeddings", `type Base struct{Items []int};type A struct{Base};type B struct{Base};type C struct{A;B};func(c *C)Clone()*C{d:=*c;d.A.Items=append([]int(nil),c.A.Items...);return &d}`, 1},
		{"positional literal", `type C struct{Items []int};func(c *C)Clone()*C{items:=make([]int,len(c.Items));copy(items,c.Items);return &C{items}}`, 0},
		{"tuple assignment", `type C struct{Items []int};func cloneItems(s []int)([]int,error){return append([]int(nil),s...),nil};func(c *C)Clone()*C{d:=*c;var err error;d.Items,err=cloneItems(c.Items);_=err;return &d}`, 0},
		{"nested literal", `type Base struct{Items []int};type C struct{Base};func(c *C)Clone()*C{return &C{Base:Base{Items:append([]int(nil),c.Items...)}}}`, 0},
		{"value fields", `type C struct{ count int }; func(c *C) Clone()*C{d:=*c;return &d}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.RunTyped(t, cops.NewLintCloneCompleteness(), "package sample\n"+tc.src), tc.want)
		})
	}
}
