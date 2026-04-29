package cop

import (
	"go/ast"
	"go/token"
	"strings"
)

// SuppressionPrefix is the comment prefix that disables one or more cops on
// a single line. We use our own prefix so that golangci-lint's nolintlint
// does not validate cop names it has never heard of.
const SuppressionPrefix = "//rubocop:disable"

// FileSuppressionPrefix disables one or more cops for the entire file.
const FileSuppressionPrefix = "//rubocop:disable-file"

// Suppressions records which cops are disabled where.
//
// A directive accepts a comma-separated list of cop names:
//
//	x = y    //rubocop:disable Lint/Foo, Lint/Bar
//	//rubocop:disable Lint/Foo
//	x = y
//	//rubocop:disable-file Lint/Foo
//
// Inline directives apply to the source line they end on.
// Full-line directives apply to the next non-blank line.
// File-level directives apply to every line in the file.
type Suppressions struct {
	perLine map[int]map[string]bool
	perFile map[string]bool
}

// ScanSuppressions inspects file's comment groups and returns a
// Suppressions snapshot. It is safe to call on a nil file (the result
// suppresses nothing).
func ScanSuppressions(fset *token.FileSet, file *ast.File) *Suppressions {
	s := &Suppressions{
		perLine: map[int]map[string]bool{},
		perFile: map[string]bool{},
	}
	if file == nil {
		return s
	}
	for _, group := range file.Comments {
		for _, c := range group.List {
			if names, ok := parseDirective(c.Text, FileSuppressionPrefix); ok {
				for _, n := range names {
					s.perFile[n] = true
				}
				continue
			}
			names, ok := parseDirective(c.Text, SuppressionPrefix)
			if !ok {
				continue
			}
			pos := fset.Position(c.Slash)
			end := fset.Position(c.End())
			// Inline trailing comment: applies to the line where the
			// comment ends.
			s.add(end.Line, names)
			// Full-line comment above: applies to the next line.
			s.add(pos.Line+1, names)
		}
	}
	return s
}

// Suppresses reports whether the named cop is silenced at the given line.
func (s *Suppressions) Suppresses(copName string, line int) bool {
	if s == nil {
		return false
	}
	if s.perFile[copName] {
		return true
	}
	if cops, ok := s.perLine[line]; ok && cops[copName] {
		return true
	}
	return false
}

func (s *Suppressions) add(line int, names []string) {
	cops, ok := s.perLine[line]
	if !ok {
		cops = map[string]bool{}
		s.perLine[line] = cops
	}
	for _, n := range names {
		cops[n] = true
	}
}

// parseDirective extracts the comma-separated cop names following the given
// prefix in comment. Returns ok==false when comment doesn't carry the
// directive.
func parseDirective(comment, prefix string) ([]string, bool) {
	rest, ok := strings.CutPrefix(comment, prefix)
	if !ok {
		return nil, false
	}
	if len(rest) > 0 && rest[0] != ' ' && rest[0] != '\t' {
		// e.g. "//rubocop:disable-file" must not match prefix "//rubocop:disable"
		// without a separator.
		return nil, false
	}
	rest = strings.TrimLeft(rest, " \t")
	// Drop any trailing " // explanation".
	if idx := strings.Index(rest, " //"); idx >= 0 {
		rest = rest[:idx]
	}
	var names []string
	for _, part := range strings.Split(rest, ",") {
		if name := strings.TrimSpace(part); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}
