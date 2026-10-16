package runner_test

import (
	"go/ast"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

func TestRunnerResolvesImports(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{
			name: "stdlib aliases and methods",
			files: map[string]string{
				"go.mod": "module example.test\ngo 1.26\n",
				"p.go": `package p
import networking "net"
type Resolver = networking.Resolver
func f(r *Resolver) { _, _ = r.LookupHost(nil, "example.test") }
`,
			},
		},
		{
			name: "local module aliases and methods",
			files: map[string]string{
				"go.mod": "module example.test\ngo 1.26\n",
				"p.go": `package p
import "example.test/resolver"
func f(r *resolver.Resolver) { _, _ = r.LookupHost(nil, "example.test") }
`,
				"resolver/resolver.go": "package resolver\nimport \"net\"\ntype Resolver = net.Resolver\n",
			},
		},
		{
			name: "local replacement",
			files: map[string]string{
				"go.mod": "module example.test\ngo 1.26\nrequire resolver.test v0.0.0\nreplace resolver.test => ./dependency\n",
				"p.go": `package p
import "resolver.test"
func f(r *resolver.Resolver) { _, _ = r.LookupHost(nil, "example.test") }
`,
				"dependency/go.mod":      "module resolver.test\ngo 1.26\n",
				"dependency/resolver.go": "package resolver\nimport \"net\"\ntype Resolver = net.Resolver\n",
			},
		},
		{
			name: "unresolved import retains other types",
			files: map[string]string{
				"go.mod": "module example.test\ngo 1.26\n",
				"p.go": `package p
import (
 "net"
 _ "missing.invalid/dependency"
)
func f(r *net.Resolver) { _, _ = r.LookupHost(nil, "example.test") }
`,
			},
		},
		{
			name: "malformed module fallback",
			files: map[string]string{
				"go.mod": "module example.test\ngo invalid\n",
				"p.go": `package p
import "net"
func f(r *net.Resolver) { _, _ = r.LookupHost(nil, "example.test") }
`,
			},
		},
		{
			name: "loose file fallback",
			files: map[string]string{
				"p.go": `package p
import "net"
func f(r *net.Resolver) { _, _ = r.LookupHost(nil, "example.test") }
`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeModule(t, dir, tc.files)
			var methods []string
			probe := cop.New(cop.Meta{Name: "Test/ResolvedImports"}, func(p *cop.Pass) {
				ast.Inspect(p.File, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					fn, ok := p.Info.Uses[sel.Sel].(*types.Func)
					if ok {
						require.NotNil(t, p.Info.Selections[sel], "method selection must be available to cops")
						methods = append(methods, fn.Pkg().Path()+"."+fn.Name())
					}
					return true
				})
			}, cop.WithTypes())
			r := runner.New([]cop.Cop{probe}, config.DefaultConfig(), io.Discard)
			_, err := r.Run([]string{filepath.Join(dir, "p.go")})
			require.NoError(t, err)
			assert.Equal(t, []string{"net.LookupHost"}, methods)
		})
	}
}

func TestRunnerImportTypeIdentity(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"go.mod": "module example.test\ngo 1.26\n",
		"lib/lib.go": `package lib
import "net/url"
func URL() *url.URL { return nil }
`,
		"p.go": `package p
import (
 "net/url"
 "example.test/lib"
)
var value *url.URL = lib.URL()
`,
	})
	checked := false
	probe := cop.New(cop.Meta{Name: "Test/ImportIdentity"}, func(p *cop.Pass) {
		ast.Inspect(p.File, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if ok {
				require.True(t, types.Identical(p.Info.TypeOf(spec.Type), p.Info.TypeOf(spec.Values[0])), "direct and transitive imports must share type identity")
				checked = true
			}
			return true
		})
	}, cop.WithTypes())
	r := runner.New([]cop.Cop{probe}, config.DefaultConfig(), io.Discard)
	_, err := r.Run([]string{filepath.Join(dir, "p.go")})
	require.NoError(t, err)
	assert.True(t, checked)
}

func TestRunnerImportedTypesDriveCops(t *testing.T) {
	for _, tc := range []struct {
		name string
		cop  cop.Cop
		src  string
	}{
		{
			name: "resolver method",
			cop:  cops.NewLintConstructorNetworkIO(),
			src: `package p
import networking "net"
type Resolver = networking.Resolver
func New(r *Resolver) error { _, err := r.LookupHost(nil, "example.test"); return err }
`,
		},
		{
			name: "local resolver alias",
			cop:  cops.NewLintConstructorNetworkIO(),
			src: `package p
import "example.test/resolvers"
func New(r *resolvers.Resolver) error { _, err := r.LookupHost(nil, "example.test"); return err }
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeModule(t, dir, map[string]string{
				"go.mod":                 "module example.test\ngo 1.26\n",
				"p.go":                   tc.src,
				"resolvers/resolvers.go": "package resolvers\nimport \"net\"\ntype Resolver = net.Resolver\n",
			})
			r := runner.New([]cop.Cop{tc.cop}, config.DefaultConfig(), io.Discard)
			count, err := r.Run([]string{filepath.Join(dir, "p.go")})
			require.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	}
}

func TestFileCopsResolveStandardLibraryImports(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		check     cop.Cop
		want      int
	}{
		{"os dot import", `import . "os";func f(){Exit(1)}`, cops.NewLintOsExit(), 1},
		{"os parenthesized dot import", `import . "os";func f(){(Exit)(1)}`, cops.NewLintOsExit(), 1},
		{"os shadowed dot import", `import . "os";func f(){Exit:=func(int){};Exit(1);_=Args}`, cops.NewLintOsExit(), 0},
		{"fmt dot import", `import . "fmt";func f(){Println("debug")}`, cops.NewLintFmtPrint(), 1},
		{"fmt parenthesized dot import", `import . "fmt";func f(){(Println)("debug")}`, cops.NewLintFmtPrint(), 1},
		{"fmt shadowed dot import", `import . "fmt";func f(){Println:=func(string){};Println("debug");_=Sprintf}`, cops.NewLintFmtPrint(), 0},
		{"errorf dot import", `import . "fmt";func f(err error)error{return Errorf("%v",err)}`, cops.NewLintWrapErrors(), 1},
		{"errorf parenthesized dot import", `import . "fmt";func f(err error)error{return (Errorf)("%v",err)}`, cops.NewLintWrapErrors(), 1},
		{"errorf shadowed dot import", `import . "fmt";func f(err error)error{Errorf:=func(string,...any)error{return nil};_=Sprintf;return Errorf("%v",err)}`, cops.NewLintWrapErrors(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "p.go"), []byte("package p;"+tc.src), 0o600))
			count, err := runner.New([]cop.Cop{tc.check}, config.DefaultConfig(), io.Discard).Run([]string{dir})
			require.NoError(t, err)
			assert.Equal(t, tc.want, count)
		})
	}
}
