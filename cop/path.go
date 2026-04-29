package cop

import (
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
// the given repository-relative directory.
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

	idx := strings.Index(slash, needle)
	if idx < 0 {
		return "", false
	}
	rest := slash[idx+len(needle):]
	i := strings.Index(rest, "/")
	if i < 0 {
		return "", false
	}
	return rest[:i], true
}
