package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

const reflectFieldsFixture = `package p
import "reflect"
func fields(v reflect.Value) {
	for i := range v.NumField() {
		_ = v.Field(i).Interface()
		_ = v.Type().Field(i).Tag.Get("json")
	}
}
`

// The paired metadata/value loop immediately before #4406.
const reflectFieldsHooksFixture = `package p
import (
	"iter"
	"reflect"
	"strings"
)
type HookDefinitions []string
type HookMatcherConfig struct { Hooks HookDefinitions }
type HookMatcherConfigs []HookMatcherConfig
type HooksConfig struct {
	Start HookDefinitions
	Stop HookMatcherConfigs
}
func (h *HooksConfig) Events() iter.Seq2[string, HookMatcherConfigs] {
	return func(yield func(string, HookMatcherConfigs) bool) {
		if h == nil { return }
		v := reflect.ValueOf(h).Elem()
		for i := range v.NumField() {
			if v.Field(i).Len() == 0 { continue }
			name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
			var matchers HookMatcherConfigs
			switch hooks := v.Field(i).Interface().(type) {
			case HookDefinitions:
				matchers = HookMatcherConfigs{{Hooks: hooks}}
			case HookMatcherConfigs:
				matchers = hooks
			}
			if !yield(name, matchers) { return }
		}
	}
}
`

func TestReflectFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"paired fields", reflectFieldsFixture},
		{"pre 4406", reflectFieldsHooksFixture},
		{"alias import", strings.ReplaceAll(strings.Replace(reflectFieldsFixture, `import "reflect"`, `import refl "reflect"`, 1), "reflect.", "refl.")},
		{"dot import", strings.ReplaceAll(strings.Replace(reflectFieldsFixture, `import "reflect"`, `import . "reflect"`, 1), "reflect.", "")},
		{"type alias", strings.Replace(reflectFieldsFixture, "v reflect.Value", "v Value", 1) + "\ntype Value = reflect.Value\n"},
		{"parentheses", strings.ReplaceAll(strings.ReplaceAll(reflectFieldsFixture, "v.", "(v)."), "Field(i)", "Field((i))")},
		{"tag lookup", strings.Replace(reflectFieldsFixture, `_ = v.Type().Field(i).Tag.Get("json")`, `_, _ = v.Type().Field(i).Tag.Lookup("json")`, 1)},
		{"shadowed index", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "{ i := 42; _ = i }; _ = v.Field(i).Interface()", 1)},
		{"shadowed receiver", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "{ v := 42; v++; _ = v }; _ = v.Field(i).Interface()", 1)},
		{"noncapturing closure", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = func() { defer func() { _ = recover() }() }; _ = v.Field(i).Interface()", 1)},
		{"continue and break", `package p
import "reflect"
func fields(v reflect.Value) []string {
	var names []string
	for i := range v.NumField() {
		field := v.Field(i)
		metadata := v.Type().Field(i)
		if field.IsZero() { continue }
		names = append(names, metadata.Name)
		if len(names) == 2 { break }
	}
	return names
}`},
		{"early return", strings.Replace(reflectFieldsFixture, `_ = v.Type().Field(i).Tag.Get("json")`, `if v.Type().Field(i).Tag.Get("json") == "stop" { return }`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runReflectFields(t, "sample.go", tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/ReflectFields", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "range v.Fields()")
			assert.Contains(t, offenses[0].Message, "declaration order")
			assert.Contains(t, offenses[0].Message, "early stopping")
		})
	}
}

func TestReflectFieldsExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"value only upstream coverage", strings.Replace(reflectFieldsFixture, `_ = v.Type().Field(i).Tag.Get("json")`, "", 1)},
		{"metadata only", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "", 1)},
		{"existing index", strings.Replace(reflectFieldsFixture, "for i := range", "var i int; for i = range", 1)},
		{"three clause loop", strings.Replace(reflectFieldsFixture, "for i := range v.NumField()", "for i := 0; i < v.NumField(); i++", 1)},
		{"unrelated index use", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = i; _ = v.Field(i).Interface()", 1)},
		{"increment index", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "i++; _ = v.Field(i).Interface()", 1)},
		{"index address", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = &i; _ = v.Field(i).Interface()", 1)},
		{"different field index", strings.Replace(reflectFieldsFixture, "v.Type().Field(i)", "v.Type().Field(0)", 1)},
		{"repeated metadata", strings.Replace(reflectFieldsFixture, `_ = v.Type().Field(i).Tag.Get("json")`, `a, b := v.Type().Field(i), v.Type().Field(i); a.Index[0]++; _ = b.Index[0]`, 1)},
		{"metadata in nested loop", strings.Replace(reflectFieldsFixture, `_ = v.Type().Field(i).Tag.Get("json")`, `for range 2 { _ = v.Type().Field(i) }`, 1)},
		{"metadata in goto loop", strings.Replace(reflectFieldsFixture, `_ = v.Type().Field(i).Tag.Get("json")`, `again: _ = v.Type().Field(i); goto again`, 1)},
		{"different receiver", strings.Replace(strings.Replace(reflectFieldsFixture, "v reflect.Value", "v, other reflect.Value", 1), "v.Type()", "other.Type()", 1)},
		{"shadowed field receiver", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "{ v := reflect.Value{}; _ = v.Field(i).Interface() }", 1)},
		{"shadowed bound", `package p
import "reflect"
type value struct { reflect.Value }
func (value) NumField() int { return 1 }
func fields(v value) {
	for i := range v.NumField() { _ = v.Field(i); _ = v.Type().Field(i) }
}`},
		{"lookalike alias", `package p
import "reflect"
type Value = fake
type fake struct{}
func (fake) NumField() int { return 1 }
func (fake) Field(int) reflect.Value { return reflect.Value{} }
func (fake) Type() reflect.Type { return nil }
func fields(v Value) {
	for i := range v.NumField() { _ = v.Field(i); _ = v.Type().Field(i) }
}`},
		{"defined type", `package p
import "reflect"
type Value reflect.Value
func (v Value) NumField() int { return reflect.Value(v).NumField() }
func (v Value) Field(i int) reflect.Value { return reflect.Value(v).Field(i) }
func (v Value) Type() reflect.Type { return reflect.Value(v).Type() }
func fields(v Value) {
	for i := range v.NumField() { _ = v.Field(i); _ = v.Type().Field(i) }
}`},
		{"selector receiver", strings.ReplaceAll(strings.Replace(reflectFieldsFixture, "v reflect.Value", "state struct { v reflect.Value }", 1), "v.", "state.v.")},
		{"pointer receiver", strings.Replace(reflectFieldsFixture, "v reflect.Value", "v *reflect.Value", 1)},
		{"call receiver", strings.Replace(reflectFieldsFixture, "v.NumField()", "reflect.ValueOf(v).NumField()", 1)},
		{"receiver reassignment", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "v = reflect.Value{}; _ = v.Field(i).Interface()", 1)},
		{"receiver alias", strings.Replace(reflectFieldsFixture, "for i := range", "alias := v; _ = alias; for i := range", 1)},
		{"receiver address outside loop", strings.Replace(reflectFieldsFixture, "for i := range", "ptr := &v; _ = ptr; for i := range", 1)},
		{"receiver address in loop", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = &v; _ = v.Field(i).Interface()", 1)},
		{"receiver reflection mutation", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "v.SetZero(); _ = v.Field(i).Interface()", 1)},
		{"field reflection mutation", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "v.Field(i).SetZero(); _ = v.Field(i).Interface()", 1)},
		{"direct recover", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = recover(); _ = v.Field(i).Interface()", 1)},
		{"defer", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "defer v.Field(i).Interface(); _ = v.Field(i).Interface()", 1)},
		{"goroutine", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "go v.Field(i).Interface(); _ = v.Field(i).Interface()", 1)},
		{"index capture", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = func() int { return i }; _ = v.Field(i).Interface()", 1)},
		{"receiver capture", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = func() reflect.Value { return v }; _ = v.Field(i).Interface()", 1)},
		{"paired calls only in closure", strings.Replace(strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "_ = func() { _ = v.Field(i).Interface()", 1), `_ = v.Type().Field(i).Tag.Get("json")`, `_ = v.Type().Field(i).Tag.Get("json") }`, 1)},
		{"yield receiver capture", strings.Replace(reflectFieldsHooksFixture, "v := reflect.ValueOf(h).Elem()", "v := reflect.ValueOf(h).Elem(); yield = func(string, HookMatcherConfigs) bool { v = reflect.Value{}; return true }", 1)},
		{"capture before loop", strings.Replace(reflectFieldsFixture, "for i := range", "_ = func() { v = reflect.Value{} }; for i := range", 1)},
		{"captured receiver", strings.Replace(strings.Replace(reflectFieldsFixture, "func fields(v reflect.Value) {", "func fields(v reflect.Value) { _ = func() {", 1), "\n}\n", "\n} }\n", 1)},
		{"global receiver", strings.Replace(reflectFieldsFixture, "func fields(v reflect.Value)", "var v reflect.Value\nfunc fields()", 1)},
		{"unknown call", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "sideEffect(); _ = v.Field(i).Interface()", 1) + "\nfunc sideEffect() {}\n"},
		{"unknown method", strings.Replace(reflectFieldsFixture, "_ = v.Field(i).Interface()", "custom{}.Len(); _ = v.Field(i).Interface()", 1) + "\ntype custom struct{}\nfunc (custom) Len() int { return 0 }\n"},
		{"shadowed builtin", strings.Replace(strings.Replace(reflectFieldsFixture, "for i := range", "len := func() {}; for i := range", 1), "_ = v.Field(i).Interface()", "len(); _ = v.Field(i).Interface()", 1)},
		{"unknown callback", strings.Replace(strings.Replace(reflectFieldsFixture, "v reflect.Value", "v reflect.Value, yield func()", 1), "_ = v.Field(i).Interface()", "yield(); _ = v.Field(i).Interface()", 1)},
		{"already fields", `package p
import "reflect"
func fields(v reflect.Value) {
	for metadata, field := range v.Fields() { _ = metadata; _ = field }
}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, runReflectFields(t, "sample.go", tc.src))
		})
	}
}

func TestReflectFieldsReceiverName(t *testing.T) {
	t.Parallel()
	src := strings.ReplaceAll(strings.Replace(reflectFieldsFixture, "v reflect.Value", "value reflect.Value", 1), "v.", "value.")
	offenses := runReflectFields(t, "sample.go", src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, "range value.Fields()")
}

func TestReflectFieldsScope(t *testing.T) {
	t.Parallel()
	assert.Empty(t, runReflectFields(t, "sample.go", "// Code generated by fixture; DO NOT EDIT.\n"+reflectFieldsFixture))
	assert.Len(t, runReflectFields(t, "pkg/config/v3/sample.go", reflectFieldsFixture), 1)
	require.Len(t, runReflectFields(t, "pkg/config/latest/sample.go", reflectFieldsFixture), 1)
}

func TestReflectFieldsProgram(t *testing.T) {
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintReflectFields(), coptest.ProgramFiles{
		"go.mod": "module example.test\n\ngo 1.26\n", "fields.go": reflectFieldsHooksFixture,
	})
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/ReflectFields", offenses[0].CopName)
	assert.Equal(t, 18, offenses[0].Pos.Line)
}

func runReflectFields(t *testing.T, filename, src string) []cop.Offense {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	cfg := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
	pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	pass := &cop.Pass{Cop: newReflectFieldsFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newReflectFieldsFile().Check(pass)
	return pass.Offenses()
}
