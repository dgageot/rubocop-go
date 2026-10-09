package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
)

const splitSeqFixture = `package p
import "strings"
func words(s, sep string) { for _, word := range strings.Split(s, sep) { _ = word } }`

func TestSplitSeq(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, api string
	}{
		{"split", `import "strings"; func f(s, sep string) { for _, word := range strings.Split(s, sep) { _ = word } }`, "SplitSeq"},
		{"split after", `import "strings"; func f(s, sep string) { for _, word := range strings.SplitAfter(s, sep) { _ = word } }`, "SplitAfterSeq"},
		{"alias import", `import text "strings"; func f(s string) { for _, word := range text.Split(s, ":") { _ = word } }`, "SplitSeq"},
		{"dot import", `import . "strings"; func f(s string) { for _, word := range SplitAfter(s, ":") { _ = word } }`, "SplitAfterSeq"},
		{"parentheses", `import "strings"; func f(s string) { for _, word := range ((strings.Split)((s), (":"))) { _ = word } }`, "SplitSeq"},
		{"bare range", `import "strings"; func f(s string) { for range strings.Split(s, ":") {} }`, "SplitSeq"},
		{"blank index only", `import "strings"; func f(s string) { for _ = range strings.Split(s, ":") {} }`, "SplitSeq"},
		{"blank value", `import "strings"; func f(s string) { for _, _ = range strings.Split(s, ":") {} }`, "SplitSeq"},
		{"assignment range", `import "strings"; func f(s string) { var word string; for _, word = range strings.Split(s, ":") { _ = word } }`, "SplitSeq"},
		{"indexed value target", `import "strings"; func f(s string) { dst := []string{""}; for _, dst[0] = range strings.Split(s, ":") {} }`, "SplitSeq"},
		{"effectful args evaluated once", `import "strings"; func f(read func() string) { for _, word := range strings.Split(read(), read()) { _ = word } }`, "SplitSeq"},
		{"reassigned input", `import "strings"; func f(s, sep string) { for _, word := range strings.Split(s, sep) { s = word; sep = word } }`, "SplitSeq"},
		{"empty input", `import "strings"; func f() { for _, word := range strings.Split("", ":") { _ = word } }`, "SplitSeq"},
		{"empty separator", `import "strings"; func f(s string) { for _, word := range strings.Split(s, "") { _ = word } }`, "SplitSeq"},
		{"named string conversion", `import "strings"; type Text string; func f(s Text) { for _, word := range strings.Split(string(s), ":") { _ = word } }`, "SplitSeq"},
		{"nested recovery unchanged", `import "strings"; func f(s string) { for _, word := range strings.Split(s, ":") { func() { _ = recover(); _ = word }() } }`, "SplitSeq"},
		{"shadowed recover", `import "strings"; func f(s string) { recover := func() {}; for range strings.Split(s, ":") { recover() } }`, "SplitSeq"},
		{"early exit", `import "strings"; func f(s string) string { for _, word := range strings.Split(s, ":") { return word }; return "" }`, "SplitSeq"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := runExtractedTypedCop(t, newSplitSeqFile(), "package p\n"+tc.src)
			require.Len(t, offenses, 1)
			assert.Equal(t, "Lint/SplitSeq", offenses[0].CopName)
			assert.Contains(t, offenses[0].Message, "strings."+tc.api)
			assert.Contains(t, offenses[0].Message, "input and separator evaluation")
			assert.Greater(t, offenses[0].End.Offset, offenses[0].Pos.Offset)
		})
	}
}

func TestSplitSeqIgnoresSemanticDifferences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
	}{
		{"index and value", `import "strings"; func f(s string) { for i, word := range strings.Split(s, ":") { _, _ = i, word } }`},
		{"index only", `import "strings"; func f(s string) { for i := range strings.Split(s, ":") { _ = i } }`},
		{"assignment index", `import "strings"; func f(s string) { var i int; var word string; for i, word = range strings.Split(s, ":") { _, _ = i, word } }`},
		{"stored slice", `import "strings"; func f(s string) { words := strings.Split(s, ":"); for _, word := range words { _ = word } }`},
		{"splitN", `import "strings"; func f(s string) { for _, word := range strings.SplitN(s, ":", 2) { _ = word } }`},
		{"splitAfterN", `import "strings"; func f(s string) { for _, word := range strings.SplitAfterN(s, ":", 2) { _ = word } }`},
		{"already lazy", `import "strings"; func f(s string) { for word := range strings.SplitSeq(s, ":") { _ = word } }`},
		{"bytes mutation", `import "bytes"; func f(s []byte) { for _, word := range bytes.Split(s, []byte(":")) { s[0] = ':'; _ = word } }`},
		{"bytes SplitAfter", `import "bytes"; func f(s []byte) { for _, word := range bytes.SplitAfter(s, []byte(":")) { _ = word } }`},
		{"shadowed package", `import "strings"; func f(s string) { strings := struct{ Split func(string, string) []string }{strings.Split}; for _, word := range strings.Split(s, ":") { _ = word } }`},
		{"unrelated method", `type Text struct{}; func (Text) Split(string, string) []string { return nil }; func f(s string, strings Text) { for _, word := range strings.Split(s, ":") { _ = word } }`},
		{"local function", `func Split(s, sep string) []string { return nil }; func f(s string) { for _, word := range Split(s, ":") { _ = word } }`},
		{"function alias", `import "strings"; func f(s string) { split := strings.Split; for _, word := range split(s, ":") { _ = word } }`},
		{"direct recover", `import "strings"; func f() { defer func() { for range strings.Split("x", ":") { recover() } }(); panic("x") }`},
		{"parenthesized recover", `import "strings"; func f() { defer func() { for range strings.Split("x", ":") { (recover)() } }(); panic("x") }`},
		{"recover assignment target", `import "strings"; func f() { defer func() { a := []string{""}; for _, a[recover().(int)] = range strings.Split("x", ":") {} }(); panic(0) }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, runExtractedTypedCop(t, newSplitSeqFile(), "package p\n"+tc.src))
		})
	}
}

func TestSplitSeqConfiguration(t *testing.T) {
	t.Parallel()
	fresh := newSplitSeqFile()
	scoped := newSplitSeqFile(cop.WithScope(cop.UnderDir("internal")))
	require.NotSame(t, fresh, scoped)
	assert.Nil(t, fresh.Scope)
	assert.NotNil(t, scoped.Scope)
	assert.Empty(t, runSliceModernizationVersion(t, scoped, splitSeqFixture, "go1.26", ""))
	assert.True(t, fresh.Types)
	assert.True(t, fresh.NeedsTypes())
	assert.Empty(t, coptest.Run(t, fresh, splitSeqFixture))
	assert.Empty(t, runExtractedTypedCop(t, fresh, "// Code generated by fixture; DO NOT EDIT.\n"+splitSeqFixture))
	fresh.Run(&cop.Pass{})
	for _, tc := range []struct {
		name, module, language string
		opts                   []cop.FuncOption
		want                   int
	}{
		{"old API", "go1.23", "", nil, 0},
		{"minimum", "go1.24", "", nil, 1},
		{"unknown", "", "", nil, 0},
		{"old language", "go1.24", "go1.22", nil, 0},
		{"new API older language", "go1.24", "go1.23", nil, 1},
		{"file enables API", "go1.23", "go1.24", nil, 1},
		{"cannot lower API", "go1.23", "", []cop.FuncOption{cop.WithMinStdlibVersion("go1.21")}, 0},
		{"cannot lower language", "go1.24", "go1.22", []cop.FuncOption{cop.WithMinGoVersion("go1.21")}, 0},
		{"raised API", "go1.24", "", []cop.FuncOption{cop.WithMinStdlibVersion("go1.25")}, 0},
		{"raised language", "go1.24", "go1.23", []cop.FuncOption{cop.WithMinGoVersion("go1.24")}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, runSliceModernizationVersion(t, newSplitSeqFile(tc.opts...), splitSeqFixture, tc.module, tc.language), tc.want)
		})
	}
}
