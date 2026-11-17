package cop

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FileMatches reports whether the file under inspection has the given
// repository-relative path. Match is by suffix (with a leading slash to
// avoid spurious matches inside another directory) or by exact equality —
// so a cop that wants to run on "pkg/runtime/event.go" works regardless
// of whether it sees an absolute or a relative filename.
//
//	if !p.FileMatches("pkg/runtime/event.go") { return }
func (p *Pass) FileMatches(repoRelPath string) bool {
	slash := filepath.ToSlash(p.Filename())
	return slash == repoRelPath || strings.HasSuffix(slash, "/"+repoRelPath)
}

// FileUnder reports whether the file under inspection lives anywhere inside
// the given repository-relative directory. The directory matches at any
// depth of the filename: "pkg/tui" also matches "vendor/x/pkg/tui/view.go".
// Use [Pass.ModuleRelPath] to match one directory of a module.
//
//	if !p.FileUnder("pkg/tui") { return }
func (p *Pass) FileUnder(repoRelDir string) bool {
	slash := filepath.ToSlash(p.Filename())
	dir := strings.Trim(repoRelDir, "/")
	return strings.Contains(slash, "/"+dir+"/") || strings.HasPrefix(slash, dir+"/")
}

// PathSegment returns the directory name that immediately follows parent in
// the file's path. For example, in "/repo/pkg/config/v3/types.go" with
// parent "pkg/config", it returns ("v3", true). The second result is false
// when parent does not appear in the file's path or the match lands on the
// final filename (e.g. "pkg/config/types.go" with parent "pkg/config").
//
//	dir, ok := p.PathSegment("pkg/config")
//	if !ok { return }
func (p *Pass) PathSegment(parent string) (string, bool) {
	// Prepend a slash so a match at the start of the path looks the same as
	// a match anywhere else.
	slash := "/" + filepath.ToSlash(p.Filename())
	needle := "/" + strings.Trim(parent, "/") + "/"

	_, rest, ok := strings.Cut(slash, needle)
	if !ok {
		return "", false
	}
	seg, _, ok := strings.Cut(rest, "/")
	if !ok {
		return "", false
	}
	return seg, true
}

// ModuleRelPath returns the slash-separated path of the file under inspection
// relative to the root of its module: the nearest directory, at or above the
// file, that holds a go.mod. The second result is false when the file is in
// no module.
//
// Unlike the filename, the result is the same wherever the runner starts and
// however its paths are spelled, so it is a stable anchor for a rule about
// one part of a module. A file of a nested module is relative to that module.
//
// The lookup reads the file system on each call. It resolves a relative
// filename from the working directory and does not resolve symbolic links in
// the filename.
//
//	rel, ok := p.ModuleRelPath() // "pkg/runtime/event.go", true
func (p *Pass) ModuleRelPath() (string, bool) {
	if p.hasModuleRelPath {
		return p.moduleRelPath, true
	}
	return relToModule(p.Filename())
}

// SetModuleRelPath states the module-relative path of a file that is not on
// disk, such as the synthetic source of a test. Set it before publishing the
// pass. See [Pass.ModuleRelPath].
func (p *Pass) SetModuleRelPath(rel string) {
	p.moduleRelPath = cleanModulePath(rel)
	p.hasModuleRelPath = true
}

func cleanModulePath(rel string) string {
	return path.Clean(filepath.ToSlash(rel))
}

// relToModule returns filename relative to the nearest directory, at or above
// it, that holds a go.mod.
func relToModule(filename string) (string, bool) {
	if filename == "" {
		return "", false
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return "", false
	}
	for dir := filepath.Dir(abs); ; {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && info.Mode().IsRegular() {
			rel, err := filepath.Rel(dir, abs)
			if err != nil {
				return "", false
			}
			return filepath.ToSlash(rel), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
