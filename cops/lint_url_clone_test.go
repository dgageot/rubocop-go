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

const urlCloneFixture = `package p
import "net/url"
func clone(u *url.URL) *url.URL {
	if u == nil { return nil }
	copy := *u
	if u.User != nil {
		user := *u.User
		copy.User = &user
	}
	return &copy
}
`

func TestURLClone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"deep copy", urlCloneFixture},
		{"pointer copy", strings.ReplaceAll(strings.Replace(urlCloneFixture, "copy := *u", "copy := new(url.URL); *copy = *u", 1), "return &copy", "return copy")},
		{"new expression", strings.ReplaceAll(strings.Replace(urlCloneFixture, "copy := *u", "copy := new(*u)", 1), "return &copy", "return copy")},
		{"declarations", strings.ReplaceAll(strings.Replace(urlCloneFixture, "copy := *u", "var copy = *u", 1), "user := *u.User", "var user = *u.User")},
		{"copy user", strings.ReplaceAll(urlCloneFixture, "u.User", "copy.User")},
		{"reversed guards", strings.ReplaceAll(strings.ReplaceAll(urlCloneFixture, "u == nil", "nil == u"), "u.User != nil", "nil != u.User")},
		{"parentheses", strings.ReplaceAll(strings.Replace(urlCloneFixture, "u == nil", "(u) == (nil)", 1), ":= *u", ":= *(u)")},
		{"alias import", strings.ReplaceAll(strings.Replace(urlCloneFixture, `import "net/url"`, `import uri "net/url"`, 1), "url.", "uri.")},
		{"dot import", strings.ReplaceAll(strings.Replace(urlCloneFixture, `import "net/url"`, `import . "net/url"`, 1), "url.", "")},
		{"type alias", strings.ReplaceAll(urlCloneFixture, "*url.URL", "*Alias") + "\ntype Alias = url.URL\n"},
		{"closure", strings.Replace(urlCloneFixture, "func clone(u", "var clone = func(u", 1)},
		{"nil user branch", `package p
import "net/url"
func clone(u *url.URL) *url.URL {
	if u.User == nil { copy := *u; return &copy }
	return u
}`},
		{"nil user pointer copy", `package p
import "net/url"
func clone(u *url.URL) *url.URL {
	if u == nil { return nil }
	if nil == u.User { copy := new(url.URL); *copy = *u; return copy }
	return u
}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.src, "u.Clone()") {
				requireGo127(t)
			}
			t.Parallel()
			offenses := runURLClone(t, "sample.go", tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/URLClone", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "url.URL.Clone")
			assert.Contains(t, offenses[0].Message, "nil")
		})
	}
}

func TestURLCloneExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"shallow copy", `package p
import "net/url"
func clone(u *url.URL) *url.URL { if u == nil { return nil }; copy := *u; return &copy }`},
		{"missing nil guard", strings.Replace(urlCloneFixture, "if u == nil { return nil }", "", 1)},
		{"guard side effect", strings.Replace(urlCloneFixture, "return nil", `println("nil"); return nil`, 1)},
		{"guard initializer", strings.Replace(urlCloneFixture, "if u == nil", "if println(); u == nil", 1)},
		{"guard else", strings.Replace(urlCloneFixture, "return nil }", "return nil } else { println() }", 1)},
		{"nil source nonnil result", strings.Replace(urlCloneFixture, "return nil", "return &url.URL{}", 1)},
		{"user alias", strings.Replace(urlCloneFixture, "user := *u.User\n\t\tcopy.User = &user", "copy.User = u.User", 1)},
		{"wrong user", strings.ReplaceAll(strings.Replace(urlCloneFixture, "u *url.URL", "u, other *url.URL", 1), "user := *u.User", "user := *other.User")},
		{"user side effect", strings.Replace(urlCloneFixture, "user := *u.User", "println(); user := *u.User", 1)},
		{"user else", strings.Replace(urlCloneFixture, "copy.User = &user\n\t}", "copy.User = &user\n\t} else { copy.User = url.User(\"guest\") }", 1)},
		{"user guard equality", strings.Replace(urlCloneFixture, "u.User != nil", "u.User == nil", 1)},
		{"changed copy", strings.Replace(urlCloneFixture, "return &copy", `copy.Host = "other"; return &copy`, 1)},
		{"escaped copy", strings.Replace(urlCloneFixture, "if u.User", "_ = &copy; if u.User", 1)},
		{"modified source", strings.Replace(urlCloneFixture, "copy := *u", `u.User = nil; copy := *u`, 1)},
		{"return source", strings.Replace(urlCloneFixture, "return &copy", "_ = copy; return u", 1)},
		{"defined type", strings.ReplaceAll(urlCloneFixture, "*url.URL", "*URL") + "\ntype URL url.URL\n"},
		{"lookalike type", strings.ReplaceAll(strings.Replace(urlCloneFixture, `import "net/url"`, "type User struct { Name string }; type URL struct { User *User }", 1), "url.URL", "URL")},
		{"shadowed package", `package p
import "net/url"
func clone(u *url.URL) *url.URL {
	url := struct { URL func(*url.URL) *url.URL }{func(u *url.URL) *url.URL { return u }}
	return url.URL(u)
}`},
		{"shadowed new", `package p
import "net/url"
func clone(u *url.URL) *url.URL {
	new := func(u url.URL) *url.URL { return &u }
	if u == nil { return nil }
	copy := new(*u)
	if u.User != nil { user := *u.User; copy.User = &user }
	return copy
}`},
		{"nil user changed", `package p
import "net/url"
func clone(u *url.URL) *url.URL {
	if u.User == nil { u.User = url.User("name"); copy := *u; return &copy }
	return u
}`},
		{"nil user guard initializer", `package p
import "net/url"
func clone(u *url.URL) *url.URL {
	if println(); u.User == nil { copy := *u; return &copy }
	return u
}`},
		{"already clone", `package p
import "net/url"
func clone(u *url.URL) *url.URL { return u.Clone() }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.src, "u.Clone()") {
				requireGo127(t)
			}
			t.Parallel()
			assert.Empty(t, runURLClone(t, "sample.go", tc.src))
		})
	}
}

func TestURLCloneScope(t *testing.T) {
	t.Parallel()
	assert.Empty(t, runURLClone(t, "sample.go", "// Code generated by fixture; DO NOT EDIT.\n"+urlCloneFixture))
	assert.Len(t, runURLClone(t, "pkg/config/v3/sample.go", urlCloneFixture), 1)
	require.Len(t, runURLClone(t, "pkg/config/latest/sample.go", urlCloneFixture), 1)
}

func TestURLCloneProgram(t *testing.T) {
	requireGo127(t)
	t.Parallel()
	offenses := coptest.RunProgram(t, NewLintURLClone(), coptest.ProgramFiles{
		"go.mod": "module example.test\n\ngo 1.27\n", "clone.go": urlCloneFixture,
	})
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/URLClone", offenses[0].CopName)
	assert.Equal(t, 5, offenses[0].Pos.Line)
}

func runURLClone(t *testing.T, filename, src string) []cop.Offense {
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
	pass := &cop.Pass{Cop: newURLCloneFile(), FileSet: fset, File: file, Info: info, Package: pkg}
	newURLCloneFile().Check(pass)
	return pass.Offenses()
}
