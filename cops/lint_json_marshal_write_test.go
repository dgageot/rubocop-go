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

const jsonMarshalWriteFixture = `package p
import (
	"bytes"
	"encoding/json"
	"strings"
)
func encode(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(value); err != nil { return "", err }
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
`

func TestJSONMarshalWrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"default escaping", jsonMarshalWriteFixture},
		{"disable escaping", strings.Replace(jsonMarshalWriteFixture, "if err :=", "enc.SetEscapeHTML(false); if err :=", 1)},
		{"explicit escaping", strings.Replace(jsonMarshalWriteFixture, "if err :=", "enc.SetEscapeHTML(true); if err :=", 1)},
		{"dynamic escaping", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "value any", "value any, escape bool", 1), "if err :=", "enc.SetEscapeHTML(escape); if err :=", 1)},
		{"escaping and value evaluation", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "value any", "value func() any, escape func() bool", 1), "if err := enc.Encode(value)", "enc.SetEscapeHTML(escape()); if err := enc.Encode(value())", 1)},
		{"buffer literal", strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "buf := bytes.Buffer{}", 1)},
		{"buffer declaration literal", strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "var buf = bytes.Buffer{}", 1)},
		{"typed declarations", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "var buf bytes.Buffer = bytes.Buffer{}", 1), "enc :=", "var enc *json.Encoder =", 1)},
		{"encoder declaration", strings.Replace(jsonMarshalWriteFixture, "enc :=", "var enc =", 1)},
		{"encoder alias type", strings.Replace(jsonMarshalWriteFixture, "enc :=", "var enc Encoder =", 1) + "\ntype Encoder = *json.Encoder\n"},
		{"bool alias option", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "value any", "value any, escape Flag", 1), "if err :=", "enc.SetEscapeHTML(escape); if err :=", 1) + "\ntype Flag = bool\n"},
		{"reversed nil comparison", strings.Replace(jsonMarshalWriteFixture, "err != nil", "nil != err", 1)},
		{"parentheses", strings.NewReplacer("&buf", "&(buf)", "enc.Encode(value)", "(enc).Encode((value))", "err != nil", "(err) != (nil)", "buf.String()", "(buf).String()").Replace(jsonMarshalWriteFixture)},
		{"aliased imports", strings.NewReplacer(`"bytes"`, `b "bytes"`, `"encoding/json"`, `j "encoding/json"`, `"strings"`, `s "strings"`, "bytes.", "b.", "json.", "j.", "strings.", "s.").Replace(jsonMarshalWriteFixture)},
		{"dot import json", strings.NewReplacer(`"encoding/json"`, `. "encoding/json"`, "json.", "").Replace(jsonMarshalWriteFixture)},
		{"dot import bytes", strings.NewReplacer(`"bytes"`, `. "bytes"`, "bytes.", "").Replace(jsonMarshalWriteFixture)},
		{"dot import strings", strings.NewReplacer(`"strings"`, `. "strings"`, "strings.", "").Replace(jsonMarshalWriteFixture)},
		{"type alias", strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "var buf Buffer", 1) + "\ntype Buffer = bytes.Buffer\n"},
		{"constant newline", strings.Replace(jsonMarshalWriteFixture, `buf.String(), "\n"`, `buf.String(), newline`, 1) + "\nconst newline = \"\\n\"\n"},
		{"constant companion results", strings.NewReplacer("(string, error)", "(int, string, error)", `return "", err`, `return 0, "", err`, "return strings.TrimSuffix", "return 1, strings.TrimSuffix").Replace(jsonMarshalWriteFixture)},
		{"inert prefix", strings.Replace(jsonMarshalWriteFixture, "var buf", "const unused = 0; var buf", 1)},
		{"closure", strings.Replace(jsonMarshalWriteFixture, "func encode(", "var encode = func(", 1)},
		{"nested block", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "var buf", "{ var buf", 1), "\n}", "\n} }", 1)},
		{"switch clause", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "var buf", "switch { default: var buf", 1), "\n}", "\n} }", 1)},
		{"select clause", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "var buf", "select { default: var buf", 1), "\n}", "\n} }", 1)},
		{"shadowed unrelated locals", strings.Replace(jsonMarshalWriteFixture, "var buf", "{ buf := 1; enc := 2; _ = buf; _ = enc }; var buf", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runJSONMarshalWrite(t, "sample.go", tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/JSONMarshalWrite", offenses[0].CopName)
			assert.Equal(t, cop.Warning, offenses[0].Severity)
			for _, text := range []string{"consider jsonv2.MarshalWrite", "json.DefaultOptionsV1() followed by jsontext.EscapeForHTML(", "keep the local buffer and error returns", "discard partial JSON on error", "custom-marshaler behavior and call order"} {
				assert.Contains(t, offenses[0].Message, text)
			}
			if strings.Contains(tc.src, ".SetEscapeHTML(") {
				assert.Contains(t, offenses[0].Message, "jsontext.EscapeForHTML(savedEscapeHTML)")
				assert.Contains(t, offenses[0].Message, "evaluate and save SetEscapeHTML's argument before evaluating Encode's argument")
			} else {
				assert.Contains(t, offenses[0].Message, "jsontext.EscapeForHTML(true)")
			}
		})
	}
}

func TestJSONMarshalWriteExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"indentation", strings.Replace(jsonMarshalWriteFixture, "if err :=", `enc.SetIndent("", "  "); if err :=`, 1)},
		{"empty indentation", strings.Replace(jsonMarshalWriteFixture, "if err :=", `enc.SetIndent("", ""); if err :=`, 1)},
		{"repeated escaping", strings.Replace(jsonMarshalWriteFixture, "if err :=", "enc.SetEscapeHTML(true); enc.SetEscapeHTML(false); if err :=", 1)},
		{"multiple encodes", strings.Replace(jsonMarshalWriteFixture, "if err :=", `if err := enc.Encode(1); err != nil { return "", err }; if err :=`, 1)},
		{"stream", strings.Replace(jsonMarshalWriteFixture, "if err := enc.Encode(value); err != nil { return \"\", err }", `for _, item := range []any{value, 1} { if err := enc.Encode(item); err != nil { return "", err } }`, 1)},
		{"buffer write", strings.Replace(jsonMarshalWriteFixture, "enc :=", `buf.WriteString("prefix"); enc :=`, 1)},
		{"buffer observation", strings.Replace(jsonMarshalWriteFixture, "enc :=", "_ = buf.Len(); enc :=", 1)},
		{"buffer escape", strings.Replace(jsonMarshalWriteFixture, "enc :=", "_ = &buf; enc :=", 1)},
		{"buffer closure capture", strings.Replace(jsonMarshalWriteFixture, "enc :=", "defer func() { _ = buf.String() }(); enc :=", 1)},
		{"buffer escape from value", strings.Replace(jsonMarshalWriteFixture, "enc.Encode(value)", "enc.Encode(&buf)", 1)},
		{"buffer use in option", strings.Replace(jsonMarshalWriteFixture, "if err :=", "enc.SetEscapeHTML(buf.Len() == 0); if err :=", 1)},
		{"buffer alias in value callback", strings.Replace(jsonMarshalWriteFixture, "enc.Encode(value)", "enc.Encode(func() any { _ = &buf; return value }())", 1)},
		{"encoder alias", strings.Replace(jsonMarshalWriteFixture, "if err :=", "alias := enc; _ = alias; if err :=", 1)},
		{"encoder escape from value", strings.Replace(jsonMarshalWriteFixture, "enc.Encode(value)", "enc.Encode(enc)", 1)},
		{"encoder capture in option", strings.Replace(jsonMarshalWriteFixture, "if err :=", "enc.SetEscapeHTML(func() bool { _ = enc; return false }()); if err :=", 1)},
		{"intervening side effect", strings.Replace(jsonMarshalWriteFixture, "if err :=", "println(); if err :=", 1)},
		{"post encode side effect", strings.Replace(jsonMarshalWriteFixture, "return strings.TrimSuffix", "println(); return strings.TrimSuffix", 1)},
		{"late escaping", strings.Replace(jsonMarshalWriteFixture, "return strings.TrimSuffix", "enc.SetEscapeHTML(false); return strings.TrimSuffix", 1)},
		{"truncate", strings.Replace(jsonMarshalWriteFixture, "return strings.TrimSuffix", "buf.Truncate(buf.Len()-1); return strings.TrimSuffix", 1)},
		{"unused buffer returned", strings.Replace(jsonMarshalWriteFixture, "&buf", "&bytes.Buffer{}", 1)},
		{"error branch side effect", strings.Replace(jsonMarshalWriteFixture, `return "", err`, `println(); return "", err`, 1)},
		{"error branch partial output", strings.Replace(jsonMarshalWriteFixture, `return "", err`, "return buf.String(), err", 1)},
		{"error branch dynamic result", strings.Replace(jsonMarshalWriteFixture, `return "", err`, `return strings.ToUpper("failed"), err`, 1)},
		{"error wrapped", strings.Replace(jsonMarshalWriteFixture, `return "", err`, `return "", wrap(err)`, 1) + "\nfunc wrap(err error) error { return err }\n"},
		{"error discarded", strings.Replace(jsonMarshalWriteFixture, `return "", err`, `return "", nil`, 1)},
		{"error reassigned", strings.Replace(jsonMarshalWriteFixture, "if err :=", "var err error; if err =", 1)},
		{"wrong error condition", strings.Replace(jsonMarshalWriteFixture, "err != nil", "err == nil", 1)},
		{"else branch", strings.Replace(jsonMarshalWriteFixture, `return "", err }`, `return "", err } else { println() }`, 1)},
		{"named return", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "(string, error)", "(out string, failure error)", 1), `return "", err`, "return", 1)},
		{"untrimmed newline", strings.NewReplacer(`"strings"`, "", `return strings.TrimSuffix(buf.String(), "\n"), nil`, `return buf.String(), nil`).Replace(jsonMarshalWriteFixture)},
		{"trim different suffix", strings.Replace(jsonMarshalWriteFixture, `buf.String(), "\n"`, `buf.String(), "\r\n"`, 1)},
		{"trim multiple newlines", strings.Replace(jsonMarshalWriteFixture, "strings.TrimSuffix", "strings.TrimRight", 1)},
		{"computed suffix", strings.Replace(jsonMarshalWriteFixture, `buf.String(), "\n"`, `buf.String(), strings.Repeat("\n", 1)`, 1)},
		{"wrapped output", strings.Replace(jsonMarshalWriteFixture, `return strings.TrimSuffix(buf.String(), "\n"), nil`, `return "JSON:" + strings.TrimSuffix(buf.String(), "\n"), nil`, 1)},
		{"byte trim", `package p
import ("bytes"; "encoding/json")
func encode(value any) ([]byte, error) {
 var buf bytes.Buffer
 enc := json.NewEncoder(&buf)
 if err := enc.Encode(value); err != nil { return nil, err }
 return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}`},
		{"dynamic companion result", strings.NewReplacer("(string, error)", "(int, string, error)", `return "", err`, `return 0, "", err`, "return strings.TrimSuffix", "return len(strings.Repeat(\"x\", 1)), strings.TrimSuffix").Replace(jsonMarshalWriteFixture)},
		{"buffer parameter", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "value any", "value any, buf bytes.Buffer", 1), "var buf bytes.Buffer", "", 1)},
		{"global buffer", strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "", 1) + "\nvar buf bytes.Buffer\n"},
		{"captured buffer", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "enc :=", "return func() (string, error) { enc :=", 1), "\n}", "\n}() }", 1)},
		{"pointer buffer", strings.Replace(strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "buf := new(bytes.Buffer)", 1), "&buf", "buf", 1)},
		{"initialized buffer", strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", `buf := *bytes.NewBufferString("prefix")`, 1)},
		{"assigned buffer", strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "var buf bytes.Buffer; buf = bytes.Buffer{}", 1)},
		{"assigned encoder", strings.Replace(jsonMarshalWriteFixture, "enc :=", "var enc *json.Encoder; enc =", 1)},
		{"function alias option", `package p
import ("bytes"; "encoding/json"; "strings")
func encode(value any) (string, error) {
 var buf bytes.Buffer
 enc := json.NewEncoder(&buf)
 setEscape := enc.SetEscapeHTML
 setEscape(false)
 if err := enc.Encode(value); err != nil { return "", err }
 return strings.TrimSuffix(buf.String(), "\n"), nil
}`},
		{"encoder constructor alias", strings.Replace(jsonMarshalWriteFixture, "enc := json.NewEncoder", "newEncoder := json.NewEncoder; enc := newEncoder", 1)},
		{"encode method alias", strings.Replace(jsonMarshalWriteFixture, "if err := enc.Encode", "encode := enc.Encode; if err := encode", 1)},
		{"string method alias", strings.Replace(jsonMarshalWriteFixture, "return strings.TrimSuffix(buf.String()", "text := buf.String; return strings.TrimSuffix(text()", 1)},
		{"trim function alias", strings.Replace(jsonMarshalWriteFixture, "return strings.TrimSuffix", "trim := strings.TrimSuffix; return trim", 1)},
		{"shadowed json", strings.Replace(jsonMarshalWriteFixture, "var buf", "json := struct { NewEncoder func(*bytes.Buffer) *json.Encoder }{func(b *bytes.Buffer) *json.Encoder { println(); return json.NewEncoder(b) }}; var buf", 1)},
		{"shadowed strings", strings.Replace(jsonMarshalWriteFixture, "var buf", "strings := struct { TrimSuffix func(string, string) string }{strings.TrimPrefix}; var buf", 1)},
		{"lookalike buffer", strings.Replace(jsonMarshalWriteFixture, "var buf bytes.Buffer", "var buf Buffer", 1) + "\ntype Buffer struct { bytes.Buffer }\n"},
		{"lookalike encoder", strings.Replace(jsonMarshalWriteFixture, "json.NewEncoder(&buf)", "newEncoder(&buf)", 1) + "\ntype Encoder struct { *json.Encoder }; func newEncoder(b *bytes.Buffer) *Encoder { return &Encoder{json.NewEncoder(b)} }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, runJSONMarshalWrite(t, "sample.go", tc.src))
		})
	}
}

func TestJSONMarshalWriteScope(t *testing.T) {
	t.Parallel()
	assert.Empty(t, runJSONMarshalWrite(t, "sample.go", "// Code generated by fixture; DO NOT EDIT.\n"+jsonMarshalWriteFixture))
	assert.Len(t, runJSONMarshalWrite(t, "pkg/config/v3/sample.go", jsonMarshalWriteFixture), 1)
	require.Len(t, runJSONMarshalWrite(t, "pkg/config/latest/sample.go", jsonMarshalWriteFixture), 1)

	pass := &cop.Pass{Cop: newJSONMarshalWriteFile()}
	newJSONMarshalWriteFile().Check(pass)
	assert.Empty(t, pass.Offenses())
}

func TestJSONMarshalWriteProgram(t *testing.T) {
	requireGo127(t)
	t.Parallel()
	files := coptest.ProgramFiles{
		"go.mod":    "module example.test\n\ngo 1.27\n",
		"encode.go": strings.ReplaceAll(jsonMarshalWriteFixture, "var buf bytes.Buffer", "var buf Buffer"),
		"alias.go": `package p
import "bytes"
type Buffer = bytes.Buffer`,
		"generated/encode.go":         "// Code generated by fixture; DO NOT EDIT.\n" + jsonMarshalWriteFixture,
		"pkg/config/v3/encode.go":     jsonMarshalWriteFixture,
		"pkg/config/latest/encode.go": jsonMarshalWriteFixture,
	}
	files["encode.go"] = strings.Replace(files["encode.go"], `"bytes"`, "", 1)
	offenses := coptest.RunProgram(t, NewLintJSONMarshalWrite(), files)
	require.Len(t, offenses, 3)
	for _, offense := range offenses {
		assert.Equal(t, "Lint/JSONMarshalWrite", offense.CopName)
		assert.Equal(t, 9, offense.Pos.Line)
	}
}

func TestJSONMarshalWriteCustomMarshalerAdvisory(t *testing.T) {
	t.Parallel()
	src := strings.Replace(jsonMarshalWriteFixture, "value any", "value map[string]Value", 1) + `
type Value int
func (v Value) MarshalJSON() ([]byte, error) { println(v); return []byte("1"), nil }
`
	offenses := runJSONMarshalWrite(t, "sample.go", src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, "verify custom-marshaler behavior and call order before changing")
}

func runJSONMarshalWrite(t *testing.T, filename, src string) []cop.Offense {
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
	info.FileVersions = map[*ast.File]string{file: "go1.27"}
	pass := &cop.Pass{Cop: newJSONMarshalWriteFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newJSONMarshalWriteFile().Check(pass)
	return pass.Offenses()
}
