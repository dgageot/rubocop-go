package config

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Exclusions holds the validated exclude patterns of one configuration. It
// does not change after [Config.Exclusions] returns it, so concurrent calls
// to [Exclusions.Excludes] are safe. A nil *Exclusions excludes nothing.
type Exclusions struct {
	workDir string
	root    string
	cops    map[string][]pattern
}

// pattern is one compiled exclude pattern.
type pattern struct {
	parts     []string // path components, one of them possibly "**"
	dirOnly   bool     // the pattern ends with "/"
	recursive bool     // parts contains "**"
}

// Exclusions returns the matchers of the exclude patterns that [Load] read.
// Call it once for each run, before any file is inspected.
//
// The patterns are relative to the directory of the file that Load read. A
// configuration that Load did not return has no patterns. workDir resolves
// the relative filenames given to [Exclusions.Excludes]. It must be absolute
// when the configuration has patterns. Without patterns, Exclusions ignores
// workDir.
//
// A pattern is a relative path that uses "/" as the separator:
//
//   - Each component matches one path element with the [path.Match] syntax:
//     "*", "?", and character classes such as "[a-z]".
//   - A "**" component matches zero or more path elements.
//   - A pattern that matches a directory also matches every file below it.
//     Thus "cmd" matches "cmd/tool/main.go".
//   - A pattern that ends with "/" matches only directories.
//   - Patterns are anchored. "*.go" matches "a.go" but not "pkg/a.go". Use
//     "**/*.go" to match at any depth.
//   - Empty components and "." components have no effect.
//
// Load returns an error for an empty pattern, an absolute pattern, a ".."
// component, a backslash, a "**" in a longer component, a pattern that names
// only the root directory, and invalid [path.Match] syntax. It checks these
// patterns also in the entries for unknown and disabled cops.
//
// Matching is lexical and case-sensitive. It does not read the file system or
// resolve symbolic links.
func (c *Config) Exclusions(workDir string) (*Exclusions, error) {
	if c == nil || len(c.exclude) == 0 {
		return &Exclusions{}, nil
	}
	if !filepath.IsAbs(workDir) {
		return nil, fmt.Errorf("resolving exclude patterns: working directory %q is not absolute", workDir)
	}
	return &Exclusions{workDir: filepath.Clean(workDir), root: c.dir, cops: c.exclude}, nil
}

// compile validates every exclude list in cop-name order, so the first error
// does not depend on map iteration order. It returns nil when no list has
// patterns.
func (f *excludeFile) compile() (map[string][]pattern, error) {
	var cops map[string][]pattern
	for _, name := range slices.Sorted(maps.Keys(f.Cops)) {
		for i, raw := range f.Cops[name].Exclude {
			p, err := compilePattern(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid config: cop %q: exclude[%d] %q: %w", name, i, raw, err)
			}
			if cops == nil {
				cops = make(map[string][]pattern)
			}
			cops[name] = append(cops[name], p)
		}
	}
	return cops, nil
}

func compilePattern(raw string) (pattern, error) {
	switch {
	case raw == "":
		return pattern{}, errors.New("the pattern is empty")
	case strings.Contains(raw, `\`):
		return pattern{}, errors.New(`the pattern contains "\"; use "/" to separate path components`)
	case strings.HasPrefix(raw, "/") || filepath.IsAbs(filepath.FromSlash(raw)) || filepath.VolumeName(filepath.FromSlash(raw)) != "":
		return pattern{}, errors.New("the pattern is absolute; use a path relative to the configuration directory")
	}

	p := pattern{dirOnly: strings.HasSuffix(raw, "/")}
	for part := range strings.SplitSeq(raw, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return pattern{}, errors.New(`the pattern contains a ".." component; it cannot select files outside the configuration directory`)
		case "**":
			p.recursive = true
		default:
			if _, err := path.Match(part, ""); err != nil {
				return pattern{}, fmt.Errorf("component %q: %w", part, err)
			}
			if hasDoubleStar(part) {
				return pattern{}, fmt.Errorf(`component %q: "**" must be a complete path component`, part)
			}
		}
		p.parts = append(p.parts, part)
	}
	if len(p.parts) == 0 {
		return pattern{}, errors.New(`the pattern names only the configuration directory; use "**" to select every file`)
	}
	return p, nil
}

// hasDoubleStar reports whether a valid path.Match component contains "**"
// outside a character class. The component has no backslash, so the first
// "]" after "[" closes the class.
func hasDoubleStar(part string) bool {
	inClass := false
	for i := range len(part) {
		switch {
		case inClass:
			inClass = part[i] != ']'
		case part[i] == '[':
			inClass = true
		case part[i] == '*' && i+1 < len(part) && part[i+1] == '*':
			return true
		}
	}
	return false
}

// Excludes reports whether an exclude pattern of the named cop matches
// filename. A relative filename is resolved from the working directory given
// to [Config.Exclusions]. An empty filename, and a file outside the pattern
// root, never match.
func (e *Exclusions) Excludes(copName, filename string) bool {
	if e == nil || filename == "" {
		return false
	}
	patterns := e.cops[copName]
	if len(patterns) == 0 {
		return false
	}
	parts, ok := e.relativeParts(filename)
	if !ok {
		return false
	}
	for _, p := range patterns {
		if p.matches(parts) {
			return true
		}
	}
	return false
}

// relativeParts returns the slash-separated components of filename relative
// to the pattern root, or false when filename is not below the root.
func (e *Exclusions) relativeParts(filename string) ([]string, bool) {
	name := filename
	if !filepath.IsAbs(name) {
		name = filepath.Join(e.workDir, name)
	}
	rel, err := filepath.Rel(e.root, name)
	if err != nil {
		return nil, false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, false
	}
	return strings.Split(rel, "/"), true
}

// matches reports whether p matches the path, or one of its parent
// directories. A directory-only pattern must match a parent directory.
func (p pattern) matches(parts []string) bool {
	limit := len(parts)
	if p.dirOnly {
		limit--
	}
	if !p.recursive {
		if len(p.parts) > limit {
			return false
		}
		for i, part := range p.parts {
			if ok, _ := path.Match(part, parts[i]); !ok {
				return false
			}
		}
		return true
	}

	// State (i, j) means that p.parts[:i] matched parts[:j]. Each state is
	// visited at most once, so the search is O(len(p.parts) * len(parts)).
	width := len(parts) + 1
	seen := make([]bool, (len(p.parts)+1)*width)
	var visit func(i, j int) bool
	visit = func(i, j int) bool {
		if seen[i*width+j] {
			return false
		}
		seen[i*width+j] = true
		switch {
		case i == len(p.parts):
			return j > 0 && j <= limit
		case p.parts[i] == "**":
			return visit(i+1, j) || (j < len(parts) && visit(i, j+1))
		case j < len(parts):
			if ok, _ := path.Match(p.parts[i], parts[j]); ok {
				return visit(i+1, j+1)
			}
		}
		return false
	}
	return visit(0, 0)
}
