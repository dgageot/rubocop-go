// Package runner orchestrates running cops against Go source files.
package runner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
)

const resetColor = "\033[0m"

// write is a helper that silences errcheck for fmt output.
func write(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// Runner runs cops against Go source files.
type Runner struct {
	Cops     []cop.Cop
	Config   *config.Config
	Reporter Reporter

	// needsTypes is true when at least one cop requests type information.
	needsTypes bool
}

// New creates a Runner with the given cops filtered by config. The default
// reporter is a TextReporter writing to out (or os.Stdout when out is nil);
// override it after construction by assigning to r.Reporter.
func New(cops []cop.Cop, cfg *config.Config, out io.Writer) *Runner {
	var enabled []cop.Cop
	for _, c := range cops {
		if cfg.IsEnabled(c.Name()) {
			enabled = append(enabled, c)
		}
	}

	r := &Runner{
		Cops:     enabled,
		Config:   cfg,
		Reporter: NewTextReporter(out),
	}

	for _, c := range enabled {
		if t, ok := c.(cop.TypeAware); ok && t.NeedsTypes() {
			r.needsTypes = true
			break
		}
	}

	return r
}

// Run inspects all Go files in the given paths and returns the total offense count.
func (r *Runner) Run(paths []string) (int, error) {
	files, err := collectGoFiles(paths)
	if err != nil {
		return 0, err
	}

	r.Reporter.Start(len(r.Cops))

	var allOffenses []cop.Offense
	filesInspected := 0
	fset := token.NewFileSet()

	if r.needsTypes {
		// Group files by directory and type-check per package so type-aware
		// cops can resolve symbols.
		byDir := groupByDir(files)

		for dir, dirFiles := range byDir {
			parsed, parsedPaths, ok := r.parseFiles(fset, dirFiles)
			if !ok {
				continue
			}

			info, pkg := typeCheck(fset, dir, parsed)

			for i, pf := range parsed {
				fileOffenses := r.runCops(fset, pf, info, pkg)
				filesInspected++
				r.Reporter.FileFinished(parsedPaths[i], fileOffenses)
				allOffenses = append(allOffenses, fileOffenses...)
			}
		}
	} else {
		for _, path := range files {
			f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseErr != nil {
				// Surface the parse failure as a reporter event with no offenses.
				r.Reporter.FileFinished(path, nil)
				continue
			}

			fileOffenses := r.runCops(fset, f, nil, nil)
			filesInspected++
			r.Reporter.FileFinished(path, fileOffenses)
			allOffenses = append(allOffenses, fileOffenses...)
		}
	}

	// Sort offenses by file, then line, then column.
	slices.SortFunc(allOffenses, func(a, b cop.Offense) int {
		if a.Pos.Filename != b.Pos.Filename {
			return strings.Compare(a.Pos.Filename, b.Pos.Filename)
		}
		if a.Pos.Line != b.Pos.Line {
			return a.Pos.Line - b.Pos.Line
		}
		return a.Pos.Column - b.Pos.Column
	})

	r.Reporter.Finish(allOffenses, filesInspected)

	return len(allOffenses), nil
}

// runCops invokes every enabled cop against the file and returns the merged offense list.
func (r *Runner) runCops(fset *token.FileSet, file *ast.File, info *types.Info, pkg *types.Package) []cop.Offense {
	sup := cop.ScanSuppressions(fset, file)

	var offenses []cop.Offense
	for _, c := range r.Cops {
		p := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
		if sev, ok := r.Config.SeverityFor(c.Name()); ok {
			p.SeverityOverride = &sev
		}
		c.Check(p)
		for _, o := range p.Offenses() {
			if sup.Suppresses(c.Name(), o.Pos.Line) {
				continue
			}
			offenses = append(offenses, o)
		}
	}
	return offenses
}

func (r *Runner) parseFiles(fset *token.FileSet, paths []string) ([]*ast.File, []string, bool) {
	var (
		parsed     []*ast.File
		parsedPath []string
	)
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			r.Reporter.FileFinished(p, nil)
			continue
		}
		parsed = append(parsed, f)
		parsedPath = append(parsedPath, p)
	}
	return parsed, parsedPath, len(parsed) > 0
}

// typeCheck performs type-checking on a set of parsed files from the same directory.
// It uses a permissive configuration: errors are ignored so that cops can still
// run on code with unresolved imports.
func typeCheck(fset *token.FileSet, dir string, files []*ast.File) (*types.Info, *types.Package) {
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	cfg := &types.Config{
		// Ignore import errors — we just want the type info we can get.
		Error: func(error) {},
	}

	// Use the directory name as the package path.
	pkg, _ := cfg.Check(dir, fset, files, info)

	return info, pkg
}

// groupByDir groups file paths by their parent directory.
func groupByDir(files []string) map[string][]string {
	m := make(map[string][]string)
	for _, f := range files {
		dir := filepath.Dir(f)
		m[dir] = append(m[dir], f)
	}
	return m
}

func maxSeverity(offenses []cop.Offense) cop.Severity {
	s := cop.Convention
	for _, o := range offenses {
		if o.Severity > s {
			s = o.Severity
		}
	}
	return s
}

func readLine(filename string, lineNum int) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return "", fmt.Errorf("line %d out of range", lineNum)
	}
	return lines[lineNum-1], nil
}

func collectGoFiles(paths []string) ([]string, error) {
	var files []string

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}

		if !info.IsDir() {
			if strings.HasSuffix(path, ".go") {
				files = append(files, path)
			}
			continue
		}

		err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && p != path && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(p, ".go") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", path, err)
		}
	}

	slices.Sort(files)

	return files, nil
}
