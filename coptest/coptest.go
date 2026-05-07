// Package coptest provides helpers for unit-testing cops.
//
// External users typically only need Run and RunTyped: they accept a Cop
// and a Go source string, parse (and optionally type-check) it, run the cop,
// and return the offenses produced. This avoids the parser/types boilerplate
// that every cop test would otherwise reinvent.
//
//	offenses := coptest.Run(t, cops.NewLintOsExit(), `package x; ...`)
//	require.Len(t, offenses, 1)
//	assert.Equal(t, "Lint/OsExit", offenses[0].CopName)
package coptest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/dgageot/rubocop-go/cop"
)

// Run parses src and runs c against it, returning the offenses it produced.
// The parsed file is given the synthetic name "sample.go".
func Run(t *testing.T, c cop.Cop, src string) []cop.Offense {
	t.Helper()
	return RunNamed(t, c, "sample.go", src)
}

// RunNamed is like Run but lets you choose the synthetic filename. Useful
// when a cop's logic depends on the path (for instance, package layout).
//
// Cops that implement [cop.Scoped] are honored — RunNamed returns no
// offenses when InScope reports false, mirroring the production runner.
func RunNamed(t *testing.T, c cop.Cop, filename, src string) []cop.Offense {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("coptest: parsing %s: %v", filename, err)
	}

	p := &cop.Pass{Cop: c, FileSet: fset, File: file}
	if s, ok := c.(cop.Scoped); ok && !s.InScope(p) {
		return nil
	}
	c.Check(p)
	return p.Offenses()
}

// RunTyped writes src to a temp file, type-checks it, and runs c against the
// result. Use it for cops that opt into type information via cop.TypeAware.
//
// Type-check errors (such as unresolved imports) are silently ignored so the
// cop still runs over partial type info.
//
// Cops that implement [cop.Scoped] are honored — RunTyped returns no
// offenses when InScope reports false, mirroring the production runner.
func RunTyped(t *testing.T, c cop.Cop, src string) []cop.Offense {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("coptest: writing temp file: %v", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("coptest: parsing: %v", err)
	}

	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	cfg := &types.Config{Error: func(error) {}}
	pkg, _ := cfg.Check(dir, fset, []*ast.File{file}, info)

	p := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
	if s, ok := c.(cop.Scoped); ok && !s.InScope(p) {
		return nil
	}
	c.Check(p)
	return p.Offenses()
}
