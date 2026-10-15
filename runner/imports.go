package runner

import (
	"go/ast"
	"go/importer"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/packages"
)

// resolveImports uses the source directory's module and workspace settings.
// Keep the standard importer as a fallback for loose files and broken modules.
func resolveImports(dir string, files []*ast.File) types.Importer {
	fallback := importer.Default()
	paths := make(map[string]bool)
	var patterns []string
	for _, file := range files {
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err == nil && path != "C" && !paths[path] {
				paths[path] = true
				patterns = append(patterns, path)
			}
		}
	}
	if len(patterns) == 0 {
		return fallback
	}

	loaded, err := packages.Load(&packages.Config{
		Dir:  dir,
		Mode: packages.NeedName | packages.NeedTypes,
	}, patterns...)
	if err != nil {
		return fallback
	}
	resolved := make(map[string]*types.Package)
	for _, pkg := range loaded {
		if len(pkg.Errors) == 0 && pkg.Types != nil && pkg.Types.Complete() {
			resolved[pkg.PkgPath] = pkg.Types
		}
	}
	return &resolvedImporter{packages: resolved, fallback: fallback}
}

type resolvedImporter struct {
	packages map[string]*types.Package
	fallback types.Importer
}

func (i *resolvedImporter) Import(path string) (*types.Package, error) {
	if pkg := i.packages[path]; pkg != nil {
		return pkg, nil
	}
	return i.fallback.Import(path)
}
