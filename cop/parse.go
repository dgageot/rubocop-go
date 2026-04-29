package cop

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ParseSibling parses a single Go file located at relPath, resolved
// relative to the directory of the file under inspection. It is the
// canonical way to write a cross-file cop: anchor the rule on file A and
// inspect a sibling file B.
//
// Internally, ParseSibling allocates a fresh FileSet because the returned
// *ast.File is not meant to be reported on — node positions in it cannot be
// translated by the pass's FileSet. Use the returned AST purely to extract
// data; report on nodes that live in p.File.
//
//	hooksTypes, err := p.ParseSibling(\"../../hooks/types.go\")
func (p *Pass) ParseSibling(relPath string) (*ast.File, error) {
	dir := filepath.Dir(p.Filename())
	target := filepath.Clean(filepath.Join(dir, relPath))

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, target, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", target, err)
	}
	return f, nil
}

// ParseDirOptions controls Pass.ParseDir.
type ParseDirOptions struct {
	// SkipTests excludes files whose name ends in _test.go.
	SkipTests bool

	// SkipFiles is a set of base filenames (e.g. "builtins.go") that should
	// not be parsed. Useful when the dispatch file lives in the same
	// directory as the per-feature files and must be excluded from the
	// scan.
	SkipFiles []string
}

// ParseDir parses every .go file in the given directory (resolved relative
// to the file under inspection) and returns the resulting *ast.File values.
// Files that fail to parse are silently skipped — the cop receives whatever
// was readable, mirroring the runner's permissive policy on partial code.
//
//	files, err := p.ParseDir(\".\", cop.ParseDirOptions{SkipTests: true, SkipFiles: []string{\"builtins.go\"}})
func (p *Pass) ParseDir(dirRelPath string, opts ParseDirOptions) ([]*ast.File, error) {
	base := filepath.Dir(p.Filename())
	dir := filepath.Clean(filepath.Join(base, dirRelPath))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	skip := make(map[string]struct{}, len(opts.SkipFiles))
	for _, n := range opts.SkipFiles {
		skip[n] = struct{}{}
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if opts.SkipTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, drop := skip[name]; drop {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			continue
		}
		files = append(files, f)
	}
	return files, nil
}
