package cops

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

func TestConstructorNetworkIOCalls(t *testing.T) {
	t.Parallel()
	for _, call := range []string{
		`net.Dial("tcp", "localhost:80")`,
		`net.DialTimeout("tcp", "localhost:80", time.Second)`,
		`net.Listen("tcp", ":80")`,
		`net.ListenPacket("udp", ":80")`,
		`net.ListenTCP("tcp", nil)`,
		`net.ListenUDP("udp", nil)`,
		`net.ListenUnix("unix", nil)`,
		`net.LookupHost("localhost")`,
		`net.LookupAddr("127.0.0.1")`,
		`net.LookupTXT("localhost")`,
		`http.Get("https://example.test")`,
		`http.Head("https://example.test")`,
		`http.Post("https://example.test", "text/plain", nil)`,
		`http.PostForm("https://example.test", nil)`,
	} {
		t.Run(call, func(t *testing.T) {
			src := "package p\nfunc NewClient() error {\n_, err := " + call + "\nreturn err\n}"
			offenses := coptest.Run(t, NewLintConstructorNetworkIO(), src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/ConstructorNetworkIO", offenses[0].CopName)
			assert.Equal(t, cop.Error, offenses[0].Severity)
			assert.Equal(t, 3, offenses[0].Pos.Line)
			assert.Contains(t, offenses[0].Message, "constructor NewClient calls")
			assert.Contains(t, offenses[0].Message, "Start/Connect")
		})
	}
}

func TestConstructorNetworkIOExecutionBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"New", `func New() any { return http.Get("https://example.test") }`, 1},
		{"Unicode constructor", `func NewÉ() any { return http.Get("https://example.test") }`, 1},
		{"named result", `func NewClient() (client any) { http.Get("https://example.test"); return }`, 1},
		{"non constructor", `func Connect() { http.Get("https://example.test") }`, 0},
		{"lowercase suffix", `func Newest() any { return http.Get("https://example.test") }`, 0},
		{"no result", `func NewClient() { http.Get("https://example.test") }`, 0},
		{"method", `func (*Client) New() any { return http.Get("https://example.test") }`, 0},
		{"bodyless constructor", `func NewClient() *Client`, 0},
		{"stored closure", `func NewClient() any { request := func() { http.Get("https://example.test") }; return request }`, 0},
		{"returned closure", `func NewClient() any { return func() { http.Get("https://example.test") } }`, 0},
		{"callback", `func NewClient() any { return register(func() { http.Get("https://example.test") }) }`, 0},
		{"immediate closure", `func NewClient() any { func() { http.Get("https://example.test") }(); return nil }`, 1},
		{"parenthesized immediate closure", `func NewClient() any { (func() { http.Get("https://example.test") })(); return nil }`, 1},
		{"converted immediate closure", `func NewClient() any { (func())(func() { http.Get("https://example.test") })(); return nil }`, 1},
		{"converted deferred closure", `func NewClient() any { defer (func())(func() { http.Get("https://example.test") })(); return nil }`, 1},
		{"converted returned closure", `func NewClient() any { return (func())(func() { http.Get("https://example.test") }) }`, 0},
		{"converted asynchronous closure", `func NewClient() any { go (func())(func() { http.Get("https://example.test") })(); return nil }`, 0},
		{"range function", `func NewClient() any { for range func(yield func() bool) { http.Get("https://example.test") } {}; return nil }`, 1},
		{"nested immediate closures", `func NewClient() any { func() { func() { http.Get("https://example.test") }() }(); return nil }`, 1},
		{"stored closure inside immediate closure", `func NewClient() any { func() { _ = func() { http.Get("https://example.test") } }(); return nil }`, 0},
		{"deferred closure", `func NewClient() any { defer func() { http.Get("https://example.test") }(); return nil }`, 1},
		{"stored closure inside deferred closure", `func NewClient() any { defer func() { _ = func() { http.Get("https://example.test") } }(); return nil }`, 0},
		{"deferred closure inside returned closure", `func NewClient() any { return func() { defer func() { http.Get("https://example.test") }() } }`, 0},
		{"goroutine argument", `func NewClient() any { go consume(http.Get("https://example.test")); return nil }`, 1},
		{"goroutine receiver", `func NewClient() any { go func() *Client { http.Get("https://example.test"); return nil }().Run(); return nil }`, 1},
		{"goroutine callee", `func NewClient() any { go func() func() { http.Get("https://example.test"); return func() {} }()(); return nil }`, 1},
		{"lazy goroutine argument", `func NewClient() any { go consume(func() { http.Get("https://example.test") }); return nil }`, 0},
		{"direct async request", `func NewClient() any { go http.Get("https://example.test"); return nil }`, 0},
		{"goroutine body", `func NewClient() any { go func() { http.Get("https://example.test") }(); return nil }`, 0},
		{"non IO helpers", `func NewClient() any { http.NewRequest("GET", "https://example.test", nil); return net.ParseIP("127.0.0.1") }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, coptest.Run(t, NewLintConstructorNetworkIO(), "package p\n"+tc.src), tc.want)
		})
	}
}

func TestConstructorNetworkIOResolvedCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "package aliases",
			src: `import (network "net"; web "net/http")
func NewClient() error {
	_, err := network.Dial("tcp", "localhost:80")
	_, _ = network.ListenPacket("udp", ":80")
	_, _ = network.LookupHost("localhost")
	_, _ = web.Get("https://example.test")
	return err
}`,
			want: []string{"net.Dial", "net.ListenPacket", "net.LookupHost", "http.Get"},
		},
		{
			name: "dot import",
			src: `import . "net/http"
func NewClient() error { _, err := Head("https://example.test"); return err }`,
			want: []string{"http.Head"},
		},
		{
			name: "resolver type alias and default resolver",
			src: `import ("context"; network "net")
type Resolver = network.Resolver
func NewClient(ctx context.Context, resolver *Resolver) error {
	_, err := resolver.LookupIPAddr(ctx, "localhost")
	_, _ = network.DefaultResolver.LookupNetIP(ctx, "ip", "localhost")
	return err
}`,
			want: []string{"net.LookupIPAddr", "net.LookupNetIP"},
		},
		{
			name: "dialer selected methods only",
			src: `import ("context"; "net")
func NewClient(ctx context.Context, dialer *net.Dialer) error {
	_, err := dialer.Dial("tcp", "localhost:80")
	_, _ = dialer.DialContext(ctx, "tcp", "localhost:80")
	return err
}`,
			want: []string{"net.Dial"},
		},
		{
			name: "HTTP client methods and promoted methods",
			src: `import "net/http"
type Client struct { *http.Client }
func NewClient(client Client) error {
	_, err := client.Get("https://example.test")
	_, _ = (*http.Client).Head(client.Client, "https://example.test")
	return err
}`,
			want: []string{"http.Get", "http.Head"},
		},
		{
			name: "HTTP headers are not network requests",
			src: `import "net/http"
func NewClient(header http.Header) string {
	_ = header.Get("Content-Type")
	return http.Header.Get(header, "Content-Type")
}`,
		},
		{
			name: "resolved shadowing never falls back to syntax",
			src: `type local struct{}
func (local) Get(string) string { return "offline" }
func (local) Dial(string, string) int { return 0 }
func NewClient(http, net local) string { _ = net.Dial("tcp", "offline"); return http.Get("Content-Type") }`,
		},
		{
			name: "parenthesized package and resolver calls",
			src: `import ("context"; "net"; "net/http")
func NewClient(r *net.Resolver) error {
	_, err := (http.Get)("https://example.test")
	_, _ = (r.LookupHost)(context.Background(), "example.test")
	return err
}`,
			want: []string{"http.Get", "net.LookupHost"},
		},
		{
			name: "Do and Accept are out of scope",
			src: `import ("net"; "net/http")
func NewClient(client *http.Client, request *http.Request, listener net.Listener) error {
	_, err := client.Do(request)
	_, _ = listener.Accept()
	return err
}`,
		},
		{
			name: "unrelated methods with network names",
			src: `type local struct{}
func (local) Dial() {}
func (local) LookupHost() {}
func (local) Get() {}
func NewClient() local { c := local{}; c.Dial(); c.LookupHost(); c.Get(); return c }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "sample.go", "package p\n"+tc.src, 0)
			require.NoError(t, err)
			info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
			config := types.Config{Importer: importer.Default(), GoVersion: "go1.26"}
			pkg, err := config.Check("example.test", fset, []*ast.File{file}, info)
			require.NoError(t, err)

			c := NewLintConstructorNetworkIO()
			require.True(t, c.NeedsTypes())
			p := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
			c.Check(p)
			offenses := p.Offenses()
			require.Len(t, offenses, len(tc.want))
			for i, want := range tc.want {
				assert.Contains(t, offenses[i].Message, "constructor NewClient calls "+want+";")
			}
		})
	}
}

func TestConstructorNetworkIOTypedExecution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"size", `import "unsafe"; func New() uintptr { return unsafe.Sizeof(func() int { http.Get("https://example.test"); return 0 }()) }`, 0},
		{"alignment", `import "unsafe"; func New() uintptr { return unsafe.Alignof(func() int { http.Get("https://example.test"); return 0 }()) }`, 0},
		{"offset", `import "unsafe"; func New() uintptr { return unsafe.Offsetof(func() struct{ X int } { http.Get("https://example.test"); return struct{ X int }{} }().X) }`, 0},
		{"generic size", `import u "unsafe"; func New[T any]() uintptr { return u.Sizeof(func() T { http.Get("https://example.test"); var x T; return x }()) }`, 0},
		{"generic alignment", `import . "unsafe"; func New[T any]() uintptr { return Alignof(func() T { http.Get("https://example.test"); var x T; return x }()) }`, 0},
		{"shadowed unsafe", `type local struct{}; func (local) Sizeof(int) int { return 0 }; func New(unsafe local) int { return unsafe.Sizeof(func() int { http.Get("https://example.test"); return 0 }()) }`, 1},
		{"named immediate conversion", `type Action func(); func New() int { Action(func() { http.Get("https://example.test") })(); return 0 }`, 1},
		{"named deferred conversion", `type Action func(); func New() int { defer Action(func() { http.Get("https://example.test") })(); return 0 }`, 1},
		{"named asynchronous conversion", `type Action func(); func New() int { go Action(func() { http.Get("https://example.test") })(); return 0 }`, 0},
		{"callback factory", `func wrap(f func()) func() { return f }; func New() int { wrap(func() { http.Get("https://example.test") })(); return 0 }`, 0},
		{"converted range", `type Iterator func(func() bool); func New() int { for range Iterator(func(yield func() bool) { http.Get("https://example.test") }) {}; return 0 }`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\nimport \"net/http\"\n" + tc.src
			assert.Len(t, runExtractedTypedCop(t, NewLintConstructorNetworkIO(), src), tc.want)
		})
	}
}
