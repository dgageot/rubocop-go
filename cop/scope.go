package cop

// CheckScope is a predicate over a Pass that decides whether a cop's Check
// function should run on the file. Returning false skips the file. Use it
// as the Scope of a [Func] (or, more generally, as the InScope hook of any
// [Scoped] cop).
//
// The package provides a small set of building blocks that compose with
// And, Or, and Not. Most cops need exactly one of OnlyFile, UnderDir, or
// InPathSegment; keep custom predicates rare so that scope rules stay
// declarative and skim-readable.
type CheckScope = func(*Pass) bool

// OnlyFile matches when the file under inspection has the given
// repository-relative path. See [Pass.FileMatches] for the matching rules.
//
//	Scope: cop.OnlyFile("pkg/runtime/event.go"),
func OnlyFile(repoRelPath string) CheckScope {
	return func(p *Pass) bool { return p.FileMatches(repoRelPath) }
}

// UnderDir matches when the file lives anywhere inside the given
// repository-relative directory. See [Pass.FileUnder].
//
//	Scope: cop.UnderDir("pkg/tui"),
func UnderDir(repoRelDir string) CheckScope {
	return func(p *Pass) bool { return p.FileUnder(repoRelDir) }
}

// InPathSegment matches when parent appears in the file's path and the
// directory immediately following parent passes pred. A nil pred matches
// any segment, which is the common case ("anywhere under pkg/config/<X>/
// for any X").
//
//	Scope: cop.InPathSegment("pkg/config", nil),                    // any vN, latest, types, ...
//	Scope: cop.InPathSegment("pkg/config", func(s string) bool {    // only latest
//	    return s == "latest"
//	}),
func InPathSegment(parent string, pred func(segment string) bool) CheckScope {
	return func(p *Pass) bool {
		seg, ok := p.PathSegment(parent)
		if !ok {
			return false
		}
		return pred == nil || pred(seg)
	}
}

// NotBlackBoxTest matches files whose package is not "<dir>_test". It is
// the canonical way to exclude the external test package without writing
// a guard at the top of every Check function.
//
//	Scope: cop.And(cop.UnderDir("pkg/config"), cop.NotBlackBoxTest()),
func NotBlackBoxTest() CheckScope {
	return func(p *Pass) bool { return !p.IsBlackBoxTest() }
}

// And returns a CheckScope that matches when every scope matches. An empty
// And matches every file, which is consistent with the identity element
// of logical conjunction.
func And(scopes ...CheckScope) CheckScope {
	return func(p *Pass) bool {
		for _, s := range scopes {
			if !s(p) {
				return false
			}
		}
		return true
	}
}

// Or returns a CheckScope that matches when any scope matches. An empty
// Or matches no file, mirroring the identity element of logical
// disjunction.
func Or(scopes ...CheckScope) CheckScope {
	return func(p *Pass) bool {
		for _, s := range scopes {
			if s(p) {
				return true
			}
		}
		return false
	}
}

// Not negates a CheckScope.
func Not(s CheckScope) CheckScope {
	return func(p *Pass) bool { return !s(p) }
}
