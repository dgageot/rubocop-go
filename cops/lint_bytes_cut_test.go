package cops

import (
	"bytes"
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

var bytesCutCops = []struct {
	name, slice string
	newFile     func(...cop.FuncOption) *cop.Func
}{
	{"Prefix", "s[len(p):]", newCutPrefixFile},
	{"Suffix", "s[:len(s)-len(p)]", newCutSuffixFile},
}

func TestBytesCut(t *testing.T) {
	t.Parallel()
	for _, direction := range bytesCutCops {
		t.Run(direction.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name, body string
			}{
				{"trim", `if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`},
				{"slice", `if bytes.HasCUT(s, p) { return SLICE }`},
				{"parenthesized slice", `if (bytes.HasCUT((s), (p))) { return ` + strings.NewReplacer("s", "(s)", "p", "(p)").Replace(direction.slice) + ` }`},
				{"assignment", `if bytes.HasCUT(s, p) { s = bytes.TrimCUT(s, p) }`},
				{"slice assignment", `if bytes.HasCUT(s, p) { s = SLICE }`},
				{"declaration", `if bytes.HasCUT(s, p) { rest := bytes.TrimCUT(s, p); return rest }`},
				{"var declaration", `if bytes.HasCUT(s, p) { var rest []byte = SLICE; return rest }`},
				{"shadowing declaration", `if bytes.HasCUT(s, p) { s := bytes.TrimCUT(s, p); return s }`},
				{"parentheses", `if (bytes.HasCUT((s), (p))) { return (bytes.TrimCUT((s), (p))) }`},
				{"local source", `local := s; if bytes.HasCUT(local, p) { return bytes.TrimCUT(local, p) }`},
				{"shared backing array", `p = s; if bytes.HasCUT(s, p) { return SLICE }`},
				{"nil delimiter", `p = nil; if bytes.HasCUT(s, p) { return SLICE }`},
				{"nil source", `s = nil; if bytes.HasCUT(s, p) { return SLICE }`},
				{"same identifier", `p = s; if bytes.HasCUT(s, s) { return bytes.TrimCUT(s, s) }`},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					src := bytesCutSource(direction.name, direction.slice, tc.body)
					offenses := runBytesCut(t, direction.newFile(), src, "go1.26")
					require.Len(t, offenses, 1)
					assert.Equal(t, "Lint/Cut"+direction.name, offenses[0].CopName)
					assert.Contains(t, offenses[0].Message, "bytes.Cut"+direction.name)
					assert.Contains(t, offenses[0].Message, "capacity, nilness, and aliasing")
					assert.Greater(t, offenses[0].End.Offset, offenses[0].Pos.Offset)
				})
			}
			for _, alias := range []string{"buffer", "."} {
				t.Run("import "+alias, func(t *testing.T) {
					t.Parallel()
					src := bytesCutSource(direction.name, direction.slice, `if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`)
					src = strings.Replace(src, `import "bytes"`, `import `+alias+` "bytes"`, 1)
					qualifier := alias + "."
					if alias == "." {
						qualifier = ""
					}
					assert.Len(t, runBytesCut(t, direction.newFile(), strings.ReplaceAll(src, "bytes.", qualifier), "go1.26"), 1)
				})
			}
		})
	}
}

func TestBytesCutExclusions(t *testing.T) {
	t.Parallel()
	for _, direction := range bytesCutCops {
		t.Run(direction.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name, body string
			}{
				{"check only", `if bytes.HasCUT(s, p) { return s }`},
				{"trim only", `return bytes.TrimCUT(s, p)`},
				{"already cut", `if rest, ok := bytes.CutCUT(s, p); ok { return rest }`},
				{"different input", `if bytes.HasCUT(s, p) { return bytes.TrimCUT(p, p) }`},
				{"different delimiter", `if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, s) }`},
				{"function input", `read := func() []byte { return s }; if bytes.HasCUT(read(), p) { return bytes.TrimCUT(read(), p) }`},
				{"function delimiter", `read := func() []byte { return p }; if bytes.HasCUT(s, read()) { return bytes.TrimCUT(s, read()) }`},
				{"literal delimiter", `if bytes.HasCUT(s, []byte("x")) { return bytes.TrimCUT(s, []byte("x")) }`},
				{"field input", `v := struct{ data []byte }{s}; if bytes.HasCUT(v.data, p) { return bytes.TrimCUT(v.data, p) }`},
				{"field delimiter", `v := struct{ data []byte }{p}; if bytes.HasCUT(s, v.data) { return bytes.TrimCUT(s, v.data) }`},
				{"index input", `v := [][]byte{s}; if bytes.HasCUT(v[0], p) { return bytes.TrimCUT(v[0], p) }`},
				{"index delimiter", `v := [][]byte{p}; if bytes.HasCUT(s, v[0]) { return bytes.TrimCUT(s, v[0]) }`},
				{"dereference input", `v := &s; if bytes.HasCUT(*v, p) { return bytes.TrimCUT(*v, p) }`},
				{"sliced input", `if bytes.HasCUT(s[:], p) { return bytes.TrimCUT(s[:], p) }`},
				{"sliced delimiter", `if bytes.HasCUT(s, p[:]) { return bytes.TrimCUT(s, p[:]) }`},
				{"compound and", `if bytes.HasCUT(s, p) && len(s) > 0 { return bytes.TrimCUT(s, p) }`},
				{"compound or", `if bytes.HasCUT(s, p) || len(s) > 0 { return bytes.TrimCUT(s, p) }`},
				{"effectful condition", `change := func() bool { s[0]++; return true }; if bytes.HasCUT(s, p) && change() { return bytes.TrimCUT(s, p) }`},
				{"initializer", `if s := s; bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`},
				{"effectful initializer", `if println(s); bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`},
				{"else", `if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) } else { return s }`},
				{"negative branch", `if !bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`},
				{"guard", `if !bytes.HasCUT(s, p) { return s }; return bytes.TrimCUT(s, p)`},
				{"switch", `switch { case bytes.HasCUT(s, p): return bytes.TrimCUT(s, p) }`},
				{"intervening call", `if bytes.HasCUT(s, p) { println(s); return bytes.TrimCUT(s, p) }`},
				{"intervening init", `if bytes.HasCUT(s, p) { n := len(s); _ = n; return bytes.TrimCUT(s, p) }`},
				{"source mutation", `if bytes.HasCUT(s, p) { s[0]++; return bytes.TrimCUT(s, p) }`},
				{"delimiter mutation", `if bytes.HasCUT(s, p) { p[0]++; return bytes.TrimCUT(s, p) }`},
				{"source alias mutation", `alias := s; if bytes.HasCUT(s, p) { alias[0]++; return bytes.TrimCUT(s, p) }`},
				{"delimiter alias mutation", `alias := p; if bytes.HasCUT(s, p) { alias[0]++; return bytes.TrimCUT(s, p) }`},
				{"source reassignment", `if bytes.HasCUT(s, p) { s = p; return bytes.TrimCUT(s, p) }`},
				{"delimiter reassignment", `if bytes.HasCUT(s, p) { p = s; return bytes.TrimCUT(s, p) }`},
				{"shadowed source", `if bytes.HasCUT(s, p) { s := p; return bytes.TrimCUT(s, p) }`},
				{"shadowed delimiter", `if bytes.HasCUT(s, p) { p := s; return bytes.TrimCUT(s, p) }`},
				{"wrapper", `if bytes.HasCUT(s, p) { return bytes.TrimSpace(bytes.TrimCUT(s, p)) }`},
				{"effectful wrapper", `change := func(v []byte) []byte { s[0]++; return v }; if bytes.HasCUT(s, p) { return change(bytes.TrimCUT(s, p)) }`},
				{"effectful earlier argument", `change := func() []byte { s[0]++; return s }; if bytes.HasCUT(s, p) { return bytes.Join([][]byte{change(), bytes.TrimCUT(s, p)}, nil) }`},
				{"effectful assignment target", `v := [][]byte{s}; index := func() int { s[0]++; return 0 }; if bytes.HasCUT(s, p) { v[index()] = bytes.TrimCUT(s, p) }`},
				{"field assignment target", `v := struct{ data []byte }{}; if bytes.HasCUT(s, p) { v.data = bytes.TrimCUT(s, p) }; return v.data`},
				{"multiple assignment", `if bytes.HasCUT(s, p) { s, p = bytes.TrimCUT(s, p), s }`},
				{"blank assignment", `if bytes.HasCUT(s, p) { _ = bytes.TrimCUT(s, p) }`},
				{"closure", `if bytes.HasCUT(s, p) { return func() []byte { return bytes.TrimCUT(s, p) }() }`},
				{"defer", `if bytes.HasCUT(s, p) { defer func() { _ = bytes.TrimCUT(s, p) }() }`},
				{"conditional removal", `if bytes.HasCUT(s, p) { if len(s) > 0 { return bytes.TrimCUT(s, p) } }`},
				{"shadowed package", `bytes := struct{ HasCUT func([]byte, []byte) bool; TrimCUT func([]byte, []byte) []byte }{bytes.HasCUT, bytes.TrimCUT}; if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`},
				{"shadowed len", `len := func([]byte) int { return 0 }; if bytes.HasCUT(s, p) { return SLICE }`},
				{"multi-result arguments", `pair := func() ([]byte, []byte) { return s, p }; if bytes.HasCUT(pair()) { return bytes.TrimCUT(pair()) }`},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					assert.Empty(t, runBytesCut(t, direction.newFile(), bytesCutSource(direction.name, direction.slice, tc.body), "go1.26"))
				})
			}
			for _, tc := range []struct {
				name, declaration, body string
			}{
				{"global source", `var global []byte`, `if bytes.HasCUT(global, p) { return bytes.TrimCUT(global, p) }`},
				{"global delimiter", `var global []byte`, `if bytes.HasCUT(s, global) { return bytes.TrimCUT(s, global) }`},
				{"unrelated functions", `func has([]byte, []byte) bool { return true }; func trim(s, p []byte) []byte { return s }`, `if has(s, p) { return trim(s, p) }; _ = bytes.HasCUT`},
				{"unrelated methods", `type other struct{}; func (other) HasCUT([]byte, []byte) bool { return true }; func (other) TrimCUT(s, p []byte) []byte { return s }`, `v := other{}; if v.HasCUT(s, p) { return v.TrimCUT(s, p) }; _ = bytes.HasCUT`},
				{"unrelated trim", `func trim(s, p []byte) []byte { return s }`, `if bytes.HasCUT(s, p) { return trim(s, p) }`},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					src := bytesCutSource(direction.name, direction.slice, tc.body) + "\n" + strings.ReplaceAll(tc.declaration, "CUT", direction.name)
					assert.Empty(t, runBytesCut(t, direction.newFile(), src, "go1.26"))
				})
			}
		})
	}
}

func TestBytesCutSliceExclusions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, direction, slice string
	}{
		{"prefix full slice", "Prefix", `s[len(p):len(s):len(s)]`},
		{"prefix capacity preserved full slice", "Prefix", `s[len(p):len(s):cap(s)]`},
		{"prefix bounded slice", "Prefix", `s[len(p):len(s)]`},
		{"prefix wrong length", "Prefix", `s[len(s):]`},
		{"prefix literal offset", "Prefix", `s[1:]`},
		{"suffix full slice", "Suffix", `s[:len(s)-len(p):len(s)-len(p)]`},
		{"suffix capacity preserved full slice", "Suffix", `s[:len(s)-len(p):cap(s)]`},
		{"suffix zero lower bound", "Suffix", `s[0:len(s)-len(p)]`},
		{"suffix nonzero lower bound", "Suffix", `s[1:len(s)-len(p)]`},
		{"suffix wrong source length", "Suffix", `s[:len(p)-len(p)]`},
		{"suffix wrong delimiter length", "Suffix", `s[:len(s)-len(s)]`},
		{"suffix literal offset", "Suffix", `s[:len(s)-1]`},
		{"suffix missing high", "Suffix", `s[:]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newCutPrefixFile()
			if tc.direction == "Suffix" {
				c = newCutSuffixFile()
			}
			src := bytesCutSource(tc.direction, tc.slice, `if bytes.HasCUT(s, p) { return SLICE }`)
			assert.Empty(t, runBytesCut(t, c, src, "go1.26"))
		})
	}
}

func TestBytesCutSliceTypes(t *testing.T) {
	t.Parallel()
	for _, direction := range bytesCutCops {
		for _, tc := range []struct {
			name, declaration, sourceType, delimiterType string
			want                                         int
		}{
			{"named source", `type Data []byte`, "Data", "[]byte", 0},
			{"named delimiter", `type Data []byte`, "[]byte", "Data", 0},
			{"both named", `type Data []byte`, "Data", "Data", 0},
			{"alias to named", `type Named []byte; type Data = Named`, "Data", "[]byte", 0},
			{"source alias", `type Data = []byte`, "Data", "[]byte", 1},
			{"delimiter alias", `type Data = []byte`, "[]byte", "Data", 1},
			{"both aliases", `type Data = []byte`, "Data", "Data", 1},
			{"byte alias", `type Element = uint8; type Data = []Element`, "Data", "Data", 1},
		} {
			for _, removal := range []string{"bytes.TrimCUT(s, p)", "SLICE"} {
				t.Run(direction.name+"/"+tc.name+"/"+removal, func(t *testing.T) {
					t.Parallel()
					src := bytesCutSource(direction.name, direction.slice, `if bytes.HasCUT(s, p) { return `+removal+` }`)
					src = strings.Replace(src, "func f(s, p []byte)", "func f(s "+tc.sourceType+", p "+tc.delimiterType+")", 1)
					src += "\n" + tc.declaration
					assert.Len(t, runBytesCut(t, direction.newFile(), src, "go1.26"), tc.want)
				})
			}
		}
	}
}

func TestBytesCutReturnExclusions(t *testing.T) {
	t.Parallel()
	for _, direction := range bytesCutCops {
		for _, tc := range []struct {
			name, signature, body string
		}{
			{"bare return", `(rest []byte)`, `if bytes.HasCUT(s, p) { return }; return s`},
			{"constant companion", `([]byte, bool)`, `if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p), true }; return s, false`},
			{"effectful companion", `([]byte, int)`, `change := func() int { s[0]++; return 0 }; if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p), change() }; return s, 0`},
			{"earlier result call", `(int, []byte)`, `change := func() int { p[0]++; return 0 }; if bytes.HasCUT(s, p) { return change(), bytes.TrimCUT(s, p) }; return 0, s`},
		} {
			t.Run(direction.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				src := `package p; import "bytes"; func f(s, p []byte) ` + tc.signature + ` { ` + strings.ReplaceAll(tc.body, "CUT", direction.name) + ` }`
				assert.Empty(t, runBytesCut(t, direction.newFile(), src, "go1.26"))
			})
		}
	}
}

func TestBytesCutConfiguration(t *testing.T) {
	t.Parallel()
	for _, direction := range bytesCutCops {
		t.Run(direction.name, func(t *testing.T) {
			t.Parallel()
			src := bytesCutSource(direction.name, direction.slice, `if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`)
			c := direction.newFile()
			assert.True(t, c.Types)
			assert.Equal(t, "go1.20", c.MinStdlibVersion)
			c.Run(&cop.Pass{})
			assert.Empty(t, runBytesCut(t, c, "// Code generated by fixture. DO NOT EDIT.\n"+src, "go1.26"))
			for _, tc := range []struct {
				name, version string
				opts          []cop.FuncOption
				want          int
			}{
				{"old", "go1.19", nil, 0},
				{"minimum", "go1.20", nil, 1},
				{"unknown", "", nil, 0},
				{"cannot lower", "go1.19", []cop.FuncOption{cop.WithMinStdlibVersion("go1.18")}, 0},
				{"raised stdlib", "go1.20", []cop.FuncOption{cop.WithMinStdlibVersion("go1.21")}, 0},
				{"raised language", "go1.20", []cop.FuncOption{cop.WithMinGoVersion("go1.21")}, 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					assert.Len(t, runBytesCut(t, direction.newFile(tc.opts...), src, tc.version), tc.want)
				})
			}
		})
	}
}

func TestBytesCutProgram(t *testing.T) {
	t.Parallel()
	for _, direction := range bytesCutCops {
		t.Run(direction.name, func(t *testing.T) {
			t.Parallel()
			src := bytesCutSource(direction.name, direction.slice, `if bytes.HasCUT(s, p) { return bytes.TrimCUT(s, p) }`)
			c := NewLintCutPrefix()
			if direction.name == "Suffix" {
				c = NewLintCutSuffix()
			}
			offenses := coptest.RunProgram(t, c, coptest.ProgramFiles{
				"sample.go":              src,
				"generated/generated.go": "// Code generated by fixture. DO NOT EDIT.\n" + src,
			})
			require.Len(t, offenses, 1)
			assert.Contains(t, offenses[0].Message, "bytes.Cut"+direction.name)
		})
	}
}

func TestBytesCutPreservesSliceSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		s, p      []byte
		wantFound bool
	}{
		{"both nil", nil, nil, true},
		{"nil source empty delimiter", nil, []byte{}, true},
		{"non-nil empty source", make([]byte, 0, 8), nil, true},
		{"empty delimiter", []byte("xyz"), nil, true},
		{"complete removal", []byte("xyz"), []byte("xyz"), true},
		{"overlapping delimiter", []byte("xxx"), []byte("x"), true},
		{"no match", []byte("xyz"), []byte("zxy"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, direction := range []struct {
				name string
				has  func([]byte, []byte) bool
				trim func([]byte, []byte) []byte
				cut  func([]byte, []byte) ([]byte, bool)
			}{
				{"prefix", bytes.HasPrefix, bytes.TrimPrefix, bytes.CutPrefix},
				{"suffix", bytes.HasSuffix, bytes.TrimSuffix, bytes.CutSuffix},
			} {
				t.Run(direction.name, func(t *testing.T) {
					original := tc.s
					if direction.has(tc.s, tc.p) {
						original = direction.trim(tc.s, tc.p)
					}
					modern, found := direction.cut(tc.s, tc.p)
					assert.Equal(t, tc.wantFound, found)
					assert.Equal(t, original, modern)
					assert.Equal(t, original == nil, modern == nil)
					assert.Equal(t, cap(original), cap(modern))
					if cap(original) > 0 {
						assert.Same(t, &original[:cap(original)][0], &modern[:cap(modern)][0], "backing-array alias must be preserved")
					}
				})
			}
		})
	}
}

func bytesCutSource(direction, slice, body string) string {
	body = strings.NewReplacer("CUT", direction, "SLICE", slice).Replace(body)
	return "package p\nimport \"bytes\"\nfunc f(s, p []byte) []byte { " + body + "; return s }"
}

func runBytesCut(t *testing.T, c *cop.Func, src, version string) []cop.Offense {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.ParseComments)
	require.NoError(t, err)
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), FileVersions: make(map[*ast.File]string),
	}
	cfg := types.Config{Importer: importer.Default(), GoVersion: version}
	pkg, err := cfg.Check("fixture", fset, []*ast.File{file}, info)
	require.NoError(t, err)
	pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
	c.Check(pass)
	return pass.Offenses()
}
