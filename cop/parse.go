package cop

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
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
	return p.ParseFile(filepath.Join(dir, relPath))
}

// ParseFile parses a Go file at path using rubocop-go's fast syntactic
// parse mode. Relative paths are resolved from the process working
// directory, matching the runner's path handling.
func (p *Pass) ParseFile(path string) (*ast.File, error) {
	target := filepath.Clean(path)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, target, nil, parseMode)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", target, err)
	}
	return f, nil
}

var parseMode = parser.ParseComments | parser.SkipObjectResolution

// SiblingStringConsts parses a sibling file and returns its top-level string
// constants. It combines [Pass.ParseSibling] and [StringConstsIn] for the
// common cross-file registry-sync pattern.
func (p *Pass) SiblingStringConsts(relPath string, pred func(name string) bool) (map[string]string, error) {
	file, err := p.ParseSibling(relPath)
	if err != nil {
		return nil, err
	}
	return StringConstsIn(file, pred), nil
}

// DirStringConsts parses every matching Go file in dirRelPath and returns the
// union of their top-level string constants. Later files with the same
// constant name overwrite earlier values; callers that need duplicate
// detection should use ParseDir directly.
func (p *Pass) DirStringConsts(dirRelPath string, opts ParseDirOptions, pred func(name string) bool) (map[string]string, error) {
	files, err := p.ParseDir(dirRelPath, opts)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, file := range files {
		maps.Copy(out, StringConstsIn(file, pred))
	}
	return out, nil
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
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parseMode)
		if err != nil {
			continue
		}
		files = append(files, f)
	}
	return files, nil
}
