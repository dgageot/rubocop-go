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
	Cops   []cop.Cop
	Config *config.Config
	Out    io.Writer

	// needsTypes is true when at least one cop requests type information.
	needsTypes bool
}

// New creates a Runner with the given cops filtered by config.
func New(cops []cop.Cop, cfg *config.Config, out io.Writer) *Runner {
	var enabled []cop.Cop
	for _, c := range cops {
		if cfg.IsEnabled(c.Name()) {
			enabled = append(enabled, c)
		}
	}

	r := &Runner{
		Cops:   enabled,
		Config: cfg,
		Out:    out,
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

	var allOffenses []cop.Offense
	filesInspected := 0

	fset := token.NewFileSet()

	if r.needsTypes {
		// Group files by directory and type-check per package so type-aware
		// cops can resolve symbols.
		byDir := groupByDir(files)

		for dir, dirFiles := range byDir {
			parsed, ok := r.parseFiles(fset, dirFiles)
			if !ok {
				continue
			}

			info, pkg := typeCheck(fset, dir, parsed)

			for _, pf := range parsed {
				fileOffenses := r.runCops(fset, pf, info, pkg)

				filesInspected++
				r.printProgress(fileOffenses)
				allOffenses = append(allOffenses, fileOffenses...)
			}
		}
	} else {
		for _, path := range files {
			f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseErr != nil {
				write(r.Out, "%sE%s", cop.Error.Color(), resetColor)
				continue
			}

			fileOffenses := r.runCops(fset, f, nil, nil)

			filesInspected++
			r.printProgress(fileOffenses)
			allOffenses = append(allOffenses, fileOffenses...)
		}
	}

	write(r.Out, "\n")

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

	if len(allOffenses) > 0 {
		write(r.Out, "\nOffenses:\n\n")

		for _, o := range allOffenses {
			r.printOffense(o)
		}

		write(r.Out, "\n")
	}

	// Summary line.
	if len(allOffenses) == 0 {
		write(r.Out, "%d file(s) inspected, \033[32mno offenses\033[0m detected\n", filesInspected)
	} else {
		write(r.Out, "%d file(s) inspected, %s%d offense(s)%s detected\n",
			filesInspected, maxSeverity(allOffenses).Color(), len(allOffenses), resetColor)
	}

	return len(allOffenses), nil
}

// runCops invokes every enabled cop against the file and returns the merged offense list.
func (r *Runner) runCops(fset *token.FileSet, file *ast.File, info *types.Info, pkg *types.Package) []cop.Offense {
	var offenses []cop.Offense
	for _, c := range r.Cops {
		p := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: info, Package: pkg}
		if sev, ok := r.Config.SeverityFor(c.Name()); ok {
			p.SeverityOverride = &sev
		}
		c.Check(p)
		offenses = append(offenses, p.Offenses()...)
	}
	return offenses
}

func (r *Runner) printProgress(offenses []cop.Offense) {
	if len(offenses) > 0 {
		severity := maxSeverity(offenses)
		write(r.Out, "%s%s%s", severity.Color(), severity.String(), resetColor)
	} else {
		write(r.Out, ".")
	}
}

func (r *Runner) parseFiles(fset *token.FileSet, paths []string) ([]*ast.File, bool) {
	var parsed []*ast.File
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			write(r.Out, "%sE%s", cop.Error.Color(), resetColor)
			continue
		}
		parsed = append(parsed, f)
	}
	return parsed, len(parsed) > 0
}

func (r *Runner) printOffense(o cop.Offense) {
	write(r.Out, "%s:%d:%d: %s%s%s: %s%s%s: %s\n",
		o.Pos.Filename, o.Pos.Line, o.Pos.Column,
		o.Severity.Color(), o.Severity.String(), resetColor,
		o.Severity.Color(), o.CopName, resetColor,
		o.Message,
	)

	// Print source context.
	line, err := readLine(o.Pos.Filename, o.Pos.Line)
	if err == nil {
		write(r.Out, "%s\n", line)
		underline := strings.Repeat(" ", o.Pos.Column-1)
		length := o.End.Column - o.Pos.Column
		if length <= 0 {
			length = 1
		}
		write(r.Out, "%s%s%s%s\n", underline, o.Severity.Color(), strings.Repeat("^", length), resetColor)
	}
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
