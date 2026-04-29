package cop_test

import (
	"go/ast"
	"reflect"
	"strings"
	"testing"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pathProbe is a tiny cop used to exercise the path helpers on a *cop.Pass.
type pathProbe struct {
	cop.Meta
	check func(*cop.Pass)
}

func (p *pathProbe) Check(pass *cop.Pass) { p.check(pass) }

func newProbe(check func(*cop.Pass)) *pathProbe {
	return &pathProbe{
		Meta:  cop.Meta{CopName: "Test/Probe", CopDesc: "probe", CopSeverity: cop.Convention},
		check: check,
	}
}

func TestFileMatches(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		match    string
		want     bool
	}{
		{"exact suffix", "/repo/pkg/runtime/event.go", "pkg/runtime/event.go", true},
		{"exact equality (no leading slash)", "pkg/runtime/event.go", "pkg/runtime/event.go", true},
		{"wrong suffix is not enough", "/repo/x/pkgaruntime/event.go", "pkg/runtime/event.go", false},
		{"different file", "/repo/pkg/runtime/client.go", "pkg/runtime/event.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			probe := newProbe(func(p *cop.Pass) { got = p.FileMatches(tc.match) })
			coptest.RunNamed(t, probe, tc.filename, "package x")
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFileUnder(t *testing.T) {
	cases := []struct {
		filename string
		under    string
		want     bool
	}{
		{"/repo/pkg/tui/dialog.go", "pkg/tui", true},
		{"/repo/pkg/tui/sub/dialog.go", "pkg/tui", true},
		{"pkg/tui/dialog.go", "pkg/tui", true},
		{"/repo/pkg/runtime/event.go", "pkg/tui", false},
		{"pkg/runtime/event.go", "pkg/tui", false},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			var got bool
			probe := newProbe(func(p *cop.Pass) { got = p.FileUnder(tc.under) })
			coptest.RunNamed(t, probe, tc.filename, "package x")
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPathSegment(t *testing.T) {
	cases := []struct {
		filename string
		parent   string
		want     string
		ok       bool
	}{
		{"/repo/pkg/config/v3/types.go", "pkg/config", "v3", true},
		{"pkg/config/latest/types.go", "pkg/config", "latest", true},
		{"/repo/pkg/config/types.go", "pkg/config", "", false}, // file, not subdir
		{"/repo/pkg/runtime/event.go", "pkg/config", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			var (
				got string
				ok  bool
			)
			probe := newProbe(func(p *cop.Pass) { got, ok = p.PathSegment(tc.parent) })
			coptest.RunNamed(t, probe, tc.filename, "package x")
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func TestStringConstsAndMatching(t *testing.T) {
	src := `package x

const (
	A = "alpha"
	B = "beta"
	C = 42
)

const D = "delta"

const Untyped = iota
`
	var (
		all     map[string]string
		filtered map[string]string
	)
	probe := newProbe(func(p *cop.Pass) {
		all = p.StringConsts()
		filtered = p.StringConstsMatching(func(name string) bool { return strings.HasPrefix(name, "A") || name == "D" })
	})
	coptest.Run(t, probe, src)

	assert.Equal(t, map[string]string{"A": "alpha", "B": "beta", "D": "delta"}, all)
	assert.Equal(t, map[string]string{"A": "alpha", "D": "delta"}, filtered)
}

func TestForEachStructAndForEachStructField(t *testing.T) {
	src := `package x

type Outer struct {
	Name string ` + "`json:\"name\"`" + `
	Tags []string ` + "`json:\"tags,omitempty\"`" + `
}

type Inner struct {
	X int
}

func notAStruct() {}
`
	var structNames []string
	var fieldNames []string
	var jsonTags []string
	probe := newProbe(func(p *cop.Pass) {
		p.ForEachStruct(func(ts *ast.TypeSpec, _ *ast.StructType) {
			structNames = append(structNames, ts.Name.Name)
		})
		p.ForEachStructField(func(ts *ast.TypeSpec, f *ast.Field, tag reflect.StructTag) {
			for _, n := range f.Names {
				fieldNames = append(fieldNames, ts.Name.Name+"."+n.Name)
			}
			if v, ok := tag.Lookup("json"); ok {
				jsonTags = append(jsonTags, v)
			}
		})
	})
	coptest.Run(t, probe, src)

	assert.Equal(t, []string{"Outer", "Inner"}, structNames)
	assert.Equal(t, []string{"Outer.Name", "Outer.Tags", "Inner.X"}, fieldNames)
	assert.Equal(t, []string{"name", "tags,omitempty"}, jsonTags)
}

func TestForEachMethodCallAndIdentSetFromCalls(t *testing.T) {
	src := `package x

type R struct{}

func (r *R) Register(name any) {}

const Foo = "foo"
const Bar = "bar"

func wire() {
	r := &R{}
	r.Register(Foo)
	r.Register(Bar)
	r.Register("inline-string")
}
`
	var methodCalls int
	probe := newProbe(func(p *cop.Pass) {
		p.ForEachMethodCall("Register", func(_ *ast.CallExpr) { methodCalls++ })
	})
	coptest.Run(t, probe, src)
	assert.Equal(t, 3, methodCalls)

	var idents map[string]bool
	probe2 := newProbe(func(p *cop.Pass) {
		idents = p.IdentSetFromCalls("Register", 0)
	})
	coptest.Run(t, probe2, src)
	assert.Equal(t, map[string]bool{"Foo": true, "Bar": true}, idents)
}

func TestSelectorReceivers(t *testing.T) {
	src := `package x

func wire() {
	v0.Register(parsers)
	v1.Register(parsers)
	latest.Register(parsers)
	x.OtherMethod("ignored")
}
`
	var got map[string]bool
	probe := newProbe(func(p *cop.Pass) { got = p.SelectorReceivers("Register") })
	coptest.Run(t, probe, src)
	assert.Equal(t, map[string]bool{"v0": true, "v1": true, "latest": true}, got)
}

func TestIsSelectorAndMatchSelector(t *testing.T) {
	src := `package x

type T struct{ a int }

func (t *T) m() {
	t.a = 1
}
`
	var (
		matched     bool
		mx, msel    string
		mok         bool
	)
	probe := newProbe(func(p *cop.Pass) {
		p.ForEachAssign(func(a *ast.AssignStmt) {
			matched = cop.IsSelector(a.Lhs[0], "t", "a")
			mx, msel, mok = cop.MatchSelector(a.Lhs[0])
		})
	})
	coptest.Run(t, probe, src)

	assert.True(t, matched)
	assert.Equal(t, "t", mx)
	assert.Equal(t, "a", msel)
	assert.True(t, mok)
}

func TestReceiver(t *testing.T) {
	src := `package x

type T struct{}

func (T) byVal()           {}
func (t *T) byPtr()         {}
func plain()                {}
`
	var infos []cop.ReceiverInfo
	probe := newProbe(func(p *cop.Pass) {
		p.ForEachFunc(func(fn *ast.FuncDecl) {
			r, ok := cop.Receiver(fn)
			if ok {
				infos = append(infos, r)
			}
		})
	})
	coptest.Run(t, probe, src)

	require.Len(t, infos, 2)
	assert.Equal(t, cop.ReceiverInfo{Name: "", TypeName: "T", IsPointer: false}, infos[0])
	assert.Equal(t, cop.ReceiverInfo{Name: "t", TypeName: "T", IsPointer: true}, infos[1])
}
