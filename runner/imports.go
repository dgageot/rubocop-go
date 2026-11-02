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

func loadImports(dir string, patterns []string) (types.Importer, bool) {
	fallback := importer.Default()
	if len(patterns) == 0 {
		return fallback, true
	}

	loaded, err := packages.Load(&packages.Config{
		Dir:  dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedTypes,
	}, patterns...)
	if err != nil {
		return fallback, false
	}
	resolved := make(map[string]*types.Package)
	complete := true
	for _, pkg := range loaded {
		if len(pkg.Errors) == 0 && pkg.Types != nil && pkg.Types.Complete() {
			resolved[pkg.PkgPath] = pkg.Types
		} else if !buildExcludedPackage(pkg) {
			complete = false
		}
	}
	return &resolvedImporter{packages: resolved, fallback: fallback}, complete
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
	loaded   map[string]types.Importer
	failed   map[string]bool
}

func newImportResolver(files []string) *importResolver {
	r := &importResolver{
		files: make(map[string][]string), contexts: make(map[string]string),
		loaded: make(map[string]types.Importer), failed: make(map[string]bool),
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
	if key == "" || r.failed[key] {
		return resolveImports(dir, files)
	}
	if loaded := r.loaded[key]; loaded != nil {
		return loaded
	}
	var parsed []*ast.File
	fset := token.NewFileSet()
	for _, path := range r.files[key] {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err == nil {
			parsed = append(parsed, file)
		}
	}
	patterns := importPaths(append(parsed, files...))
	loaded, ok := loadImports(dir, patterns)
	if !ok {
		r.failed[key] = true
		return resolveImports(dir, files)
	}
	r.loaded[key] = loaded
	return loaded
}
