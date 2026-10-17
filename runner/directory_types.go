package runner

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dgageot/rubocop-go/cop"
)

// WithDirectoryTypes supplies a best-effort type view of all same-package Go
// siblings, including tests and inactive files. It uses the standard importer,
// tolerates type errors, and does not establish the module Go version. Only
// Types, Defs and Uses are populated. Results are cached per runner FileSet.
// As with other file cops, suppression directives belong to the inspected file.
func WithDirectoryTypes(c *cop.Func) *cop.Func {
	type checkedPackage struct {
		files map[string]*ast.File
		info  *types.Info
		pkg   *types.Package
	}
	var mu sync.Mutex
	var fset *token.FileSet
	var cache map[string]checkedPackage
	var imports types.Importer
	wrapped := *c
	wrapped.Types = false
	wrapped.Run = func(p *cop.Pass) {
		mu.Lock()
		defer mu.Unlock()
		if fset != p.FileSet {
			fset = p.FileSet
			cache = make(map[string]checkedPackage)
			imports = importer.Default()
		}
		name, err := filepath.Abs(p.Filename())
		if err != nil {
			p.Reportf(p.File, "cannot resolve source path: %v", err)
			return
		}
		dir := filepath.Dir(name)
		key := dir + "/" + p.File.Name.Name
		checked, ok := cache[key]
		if !ok {
			entries, err := os.ReadDir(dir)
			if err != nil {
				p.Reportf(p.File, "cannot read package: %v", err)
				return
			}
			parsed := make(map[string]*ast.File)
			var files []*ast.File
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
				if err != nil || file.Name.Name != p.File.Name.Name {
					continue
				}
				parsed[path] = file
				files = append(files, file)
			}
			info := &types.Info{
				Types: make(map[ast.Expr]types.TypeAndValue),
				Defs:  make(map[*ast.Ident]types.Object),
				Uses:  make(map[*ast.Ident]types.Object),
			}
			// Platform variants may conflict; retain whichever calls resolve.
			cfg := types.Config{Importer: imports, Error: func(error) {}}
			typed, _ := cfg.Check(dir, fset, files, info)
			checked = checkedPackage{files: parsed, info: info, pkg: typed}
			cache[key] = checked
		}
		file := checked.files[name]
		if file == nil {
			return
		}
		pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: checked.info, Package: checked.pkg}
		c.Check(pass)
		for _, offense := range pass.Offenses() {
			p.ReportOffense(offense)
		}
	}
	return &wrapped
}
