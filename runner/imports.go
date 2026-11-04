package runner

import (
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// resolveImports uses the source directory's module and workspace settings.
// Keep the standard importer as a fallback for loose files and broken modules.
func resolveImports(dir string, files []*ast.File) types.Importer {
	resolved, _ := loadImports(dir, importPaths(files))
	return resolved
}

func importPaths(files []*ast.File) []string {
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
	return patterns
}

// The boolean reports loader-wide success, not whether every package is healthy.
func loadImports(dir string, patterns []string) (*resolvedImporter, bool) {
	resolved := &resolvedImporter{
		packages: make(map[string]*types.Package), healthy: make(map[string]bool),
		fallback: importer.Default(),
	}
	if len(patterns) == 0 {
		return resolved, true
	}

	loaded, err := packages.Load(&packages.Config{
		Dir:  dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedTypes,
	}, patterns...)
	if err != nil {
		return resolved, false
	}
	// NeedImports retains dependency errors without requiring full dependency
	// types. Those types may legitimately be incomplete when read from exports.
	unhealthy := make(map[*packages.Package]bool)
	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		bad := len(pkg.Errors) != 0 || pkg.IllTyped
		for _, dep := range pkg.Imports {
			bad = bad || unhealthy[dep]
		}
		unhealthy[pkg] = bad
	})
	for _, pkg := range loaded {
		if len(pkg.Errors) == 0 && pkg.Types != nil && pkg.Types.Complete() {
			// Local resolution can still use partial types, as before. Sharing
			// requires the entire reachable graph to be healthy.
			resolved.packages[pkg.PkgPath] = pkg.Types
			resolved.healthy[pkg.PkgPath] = !unhealthy[pkg]
		} else if buildExcludedPackage(pkg) {
			resolved.healthy[pkg.PkgPath] = true
		}
	}
	return resolved, true
}

// Inactive platform imports cannot contribute types to any package in this build.
func buildExcludedPackage(pkg *packages.Package) bool {
	if len(pkg.GoFiles) != 0 || len(pkg.Errors) == 0 {
		return false
	}
	for _, err := range pkg.Errors {
		if !strings.HasPrefix(err.Msg, "build constraints exclude all Go files in ") {
			return false
		}
	}
	return true
}

type resolvedImporter struct {
	packages map[string]*types.Package
	healthy  map[string]bool
	fallback types.Importer
}

func (i *resolvedImporter) Import(path string) (*types.Package, error) {
	if pkg := i.packages[path]; pkg != nil {
		return pkg, nil
	}
	return i.fallback.Import(path)
}

// importResolver shares one import graph per module during a run. Loose files
// and directory-dependent build configurations retain local resolution.
type importResolver struct {
	files    map[string][]string
	contexts map[string]string
	loaded   map[string]*resolvedImporter
	failed   map[string]bool
	paths    map[string][]string
	local    map[string]types.Importer
}

func newImportResolver(files []string) *importResolver {
	r := &importResolver{
		files: make(map[string][]string), contexts: make(map[string]string),
		loaded: make(map[string]*resolvedImporter), failed: make(map[string]bool),
		paths: make(map[string][]string), local: make(map[string]types.Importer),
	}
	if !canShareImports() {
		return r
	}
	for _, file := range files {
		dir := filepath.Dir(file)
		key, ok := r.contexts[dir]
		if !ok {
			key = importContext(dir)
			r.contexts[dir] = key
		}
		if key != "" {
			r.files[key] = append(r.files[key], file)
		}
	}
	return r
}

func canShareImports() bool {
	// Relative GOENV files can select different settings in sibling directories.
	if env := os.Getenv("GOENV"); env != "" && env != "off" && !filepath.IsAbs(env) {
		return false
	}
	if driver := os.Getenv("GOPACKAGESDRIVER"); driver != "" && driver != "off" {
		return false
	}
	if os.Getenv("GOPACKAGESDRIVER") != "off" {
		if _, err := exec.LookPath("gopackagesdriver"); err == nil {
			return false
		}
	}
	// go env also includes settings persisted with go env -w.
	output, err := exec.Command("go", "env", "-json", "GO111MODULE", "GOFLAGS").Output()
	if err != nil {
		return false
	}
	var env struct{ GO111MODULE, GOFLAGS string }
	return json.Unmarshal(output, &env) == nil && env.GO111MODULE != "off" && shareableGoFlags(env.GOFLAGS)
}

func shareableGoFlags(flags string) bool {
	// Only unquoted, directory-independent flags can share an import graph.
	if strings.ContainsAny(flags, "\"'") {
		return false
	}
	for flag := range strings.FieldsSeq(flags) {
		name, value, ok := strings.Cut(flag, "=")
		if !ok {
			return false
		}
		switch name {
		case "-mod", "--mod":
			if value != "vendor" && value != "readonly" {
				return false
			}
		case "-tags", "--tags":
		default:
			return false
		}
	}
	return true
}

func importContext(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	var module, workspace string
	for {
		if module == "" {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				module = dir
			}
		}
		if workspace == "" {
			if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
				workspace = dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if module == "" {
		return ""
	}
	return module + "\x00" + workspace
}

func (r *importResolver) resolve(dir string, files []*ast.File) types.Importer {
	key := r.contexts[dir]
	if key == "" {
		return resolveImports(dir, files)
	}
	if local := r.local[dir]; local != nil {
		return local
	}
	if r.loaded[key] == nil && !r.failed[key] {
		r.load(dir, key, files)
	}
	paths := slices.Concat(r.paths[dir], importPaths(files))
	if loaded := r.loaded[key]; loaded != nil {
		if !slices.ContainsFunc(paths, func(path string) bool { return !loaded.healthy[path] }) {
			return loaded
		}
	}
	// Fall back for the whole directory: mixing individual imports from separate
	// loads would break type identity. Cache unsuccessful local loads too.
	local, _ := loadImports(dir, paths)
	r.local[dir] = local
	return local
}

func (r *importResolver) load(dir, key string, files []*ast.File) {
	byDir := make(map[string][]*ast.File)
	var parsed []*ast.File
	fset := token.NewFileSet()
	for _, path := range r.files[key] {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err == nil {
			parent := filepath.Dir(path)
			byDir[parent] = append(byDir[parent], file)
			parsed = append(parsed, file)
		}
	}
	for parent, files := range byDir {
		r.paths[parent] = importPaths(files)
	}
	loaded, ok := loadImports(dir, importPaths(append(parsed, files...)))
	if !ok {
		r.failed[key] = true
		return
	}
	r.loaded[key] = loaded
}
