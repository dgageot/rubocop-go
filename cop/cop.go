// Package cop defines the core types for writing custom cops.
//
// A cop is a rule that inspects Go source code and reports offenses.
// The recommended way to define one is to build a [Func]:
//
//	var LintOsExit = cop.New(cop.Meta{
//	    Name:        "Lint/OsExit",
//	    Description: "Avoid os.Exit outside of main()",
//	    Severity:    cop.Warning,
//	}, func(p *cop.Pass) {
//	    p.ForEachFunc(func(fn *ast.FuncDecl) { ... })
//	})
//
// To restrict a cop to a subset of files, attach a [CheckScope] — the
// runner skips the cop entirely on out-of-scope files:
//
//	var TUIViewPurity = &cop.Func{
//	    Meta:  cop.Meta{Name: "Lint/TUIViewPurity", ...},
//	    Scope: cop.UnderDir("pkg/tui"),
//	    Run:   func(p *cop.Pass) { ... },
//	}
//
// You can also implement [Cop] yourself if your cop needs to keep state
// across calls; in that case, embed [Meta] for the field-style metadata
// and provide your own Name/Description/Severity/Check methods.
package cop

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"sync"
)

// Severity represents the severity level of an offense.
type Severity int

const (
	Convention Severity = iota // style violation
	Warning                    // potential issue
	Error                      // definite bug
)

func (s Severity) String() string {
	switch s {
	case Convention:
		return "C"
	case Warning:
		return "W"
	case Error:
		return "E"
	default:
		return "?"
	}
}

// Color returns the ANSI color code for the severity.
func (s Severity) Color() string {
	switch s {
	case Convention:
		return "\033[36m" // cyan
	case Warning:
		return "\033[33m" // yellow
	case Error:
		return "\033[31m" // red
	default:
		return ""
	}
}

// Offense represents a single violation found by a cop.
type Offense struct {
	Pos      token.Position
	End      token.Position
	Message  string
	CopName  string
	Severity Severity
}

// Pass carries everything a Cop needs to inspect a single file.
//
// FileSet and File are always populated. Info and Package are populated only
// for cops that opt into type information by implementing the TypeAware
// interface; otherwise they are nil.
type Pass struct {
	Cop     Cop
	FileSet *token.FileSet
	File    *ast.File
	Info    *types.Info
	Package *types.Package

	// SeverityOverride, when non-nil, replaces the cop's default severity
	// on every offense reported through this pass. Set by the runner from
	// .rubocop-go.yml.
	SeverityOverride *Severity

	offenses []Offense
}

// Report records an offense covering the source span of the AST node n.
// The message is formatted with fmt.Sprintf semantics.
func (p *Pass) Report(n ast.Node, format string, args ...any) {
	p.ReportAt(n.Pos(), n.End(), format, args...)
}

// ReportAt records an offense covering the half-open [pos, end) range.
// Use it when you want a span narrower or wider than an ast.Node naturally
// covers.
func (p *Pass) ReportAt(pos, end token.Pos, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	o := NewOffenseAt(p.Cop, p.FileSet, pos, end, msg)
	if p.SeverityOverride != nil {
		o.Severity = *p.SeverityOverride
	}
	p.offenses = append(p.offenses, o)
}

// ReportMissing is a convenience for the recurring "X is missing entries
// for: a, b, c" diagnostic emitted by dispatch-table cops. names is sorted
// (stable, in place on a defensive copy) and joined with ", " before being
// substituted into format. The method is a no-op when names is empty, so
// callers can collect candidates unconditionally and let the helper decide
// whether to emit anything.
//
//	p.ReportMissing(anchor, "registry is missing entries for: %s", missing)
func (p *Pass) ReportMissing(anchor ast.Node, format string, names []string) {
	if len(names) == 0 {
		return
	}
	sorted := append([]string(nil), names...)
	slices.Sort(sorted)
	p.Report(anchor, format, strings.Join(sorted, ", "))
}

// Offenses returns the offenses accumulated so far on this pass.
func (p *Pass) Offenses() []Offense {
	return p.offenses
}

// Filename returns the path of the file being inspected.
func (p *Pass) Filename() string {
	return p.FileSet.Position(p.File.Package).Filename
}

// PackageName returns the declared package name of the file.
func (p *Pass) PackageName() string {
	return p.File.Name.Name
}

// IsMain reports whether the file declares package main.
func (p *Pass) IsMain() bool {
	return p.PackageName() == "main"
}

// IsTestFile reports whether the file's name ends with _test.go.
func (p *Pass) IsTestFile() bool {
	return strings.HasSuffix(p.Filename(), "_test.go")
}

// IsBlackBoxTest reports whether the file declares an external test package
// (a package name ending in _test). Such files live alongside production code
// but live in a separate package and may import what they please.
func (p *Pass) IsBlackBoxTest() bool {
	return strings.HasSuffix(p.PackageName(), "_test")
}

// Cop is the interface that all cops must implement.
type Cop interface {
	// Name returns the cop's qualified name (e.g. "Lint/OsExit").
	Name() string

	// Description returns a short human-readable description of what the cop checks.
	Description() string

	// Severity returns the default severity for offenses reported by this cop.
	Severity() Severity

	// Check inspects a file and reports offenses via p.Report.
	Check(p *Pass)
}

// Meta carries the static metadata of a cop. It is plain data with no
// methods of its own — wrap it in a [Func] (or your own struct that
// supplies the Cop interface methods) to obtain a runnable cop.
//
//	var LintOsExit = cop.New(cop.Meta{
//	    Name:        "Lint/OsExit",
//	    Description: "Avoid os.Exit outside of main()",
//	    Severity:    cop.Warning,
//	}, func(p *cop.Pass) { ... })
type Meta struct {
	Name        string
	Description string
	Severity    Severity
}

// Func is the standard way to build a cop: provide [Meta], an optional
// [CheckScope], and a check function. Most project-specific rules need
// nothing beyond a Func — define a struct only if your cop must keep
// state across files.
type Func struct {
	Meta
	// Scope, when non-nil, decides whether the cop runs on a given file.
	// Returning false from Scope short-circuits the cop entirely; Run
	// is not called and no offense can be produced.
	Scope CheckScope
	// Types, when true, opts the cop into type information. The runner
	// type-checks the package and populates p.Info / p.Package on the
	// Pass passed to Run. This is the [Func]-based equivalent of
	// implementing [TypeAware] on a custom struct.
	Types bool
	// Run is the check function. It is the production-side equivalent of
	// the Check method on Cop.
	Run func(*Pass)
}

// New is shorthand for an unscoped Func.
func New(meta Meta, run func(*Pass)) *Func {
	return &Func{Meta: meta, Run: run}
}

// Name implements [Cop].
func (f *Func) Name() string { return f.Meta.Name }

// Description implements [Cop].
func (f *Func) Description() string { return f.Meta.Description }

// Severity implements [Cop].
func (f *Func) Severity() Severity { return f.Meta.Severity }

// Check implements [Cop].
func (f *Func) Check(p *Pass) {
	if f.Run != nil {
		f.Run(p)
	}
}

// InScope implements [Scoped]. A nil Scope matches every file.
func (f *Func) InScope(p *Pass) bool {
	return f.Scope == nil || f.Scope(p)
}

// NeedsTypes implements [TypeAware].
func (f *Func) NeedsTypes() bool { return f.Types }

// Scoped is an optional interface a Cop can implement to skip the entire
// file before Check is called. The runner consults InScope first; if it
// returns false, Check is not invoked and the cop produces no offenses
// for that file.
//
// Use it (transitively, via [Func.Scope] and the helpers in scope.go) to
// declare scope filters once on the cop instead of repeating them at the
// top of every Check function.
type Scoped interface {
	InScope(*Pass) bool
}

// TypeAware is an optional interface that a Cop can implement to request
// type information. When a cop implements TypeAware and NeedsTypes returns
// true, the runner type-checks the package and populates p.Info and
// p.Package on the Pass passed to Check.
type TypeAware interface {
	NeedsTypes() bool
}

// NewOffense creates an offense for a given cop covering the source span of
// the AST node n.
func NewOffense(c Cop, fset *token.FileSet, n ast.Node, message string) Offense {
	return NewOffenseAt(c, fset, n.Pos(), n.End(), message)
}

// NewOffenseAt creates an offense covering an arbitrary [pos, end) range.
// Use it when the natural span of an ast.Node is wider or narrower than what
// you want to highlight.
func NewOffenseAt(c Cop, fset *token.FileSet, pos, end token.Pos, message string) Offense {
	return Offense{
		Pos:      fset.Position(pos),
		End:      fset.Position(end),
		Message:  message,
		CopName:  c.Name(),
		Severity: c.Severity(),
	}
}

var (
	mu       sync.Mutex
	registry []Cop
)

// Register adds a cop to the global registry. The bundled CLI in main.go
// uses this so that adding a cop to the cops/ package is enough to ship it.
//
// Embedders that build their own runner (see examples/embed) typically do
// not call Register at all — they pass an explicit slice of cops to
// runner.New and never touch the global. Use whichever style suits your
// program; the two can also coexist.
func Register(c Cop) {
	mu.Lock()
	defer mu.Unlock()
	registry = append(registry, c)
}

// All returns every cop registered through Register.
// Embedders maintaining their own slice should not need this.
func All() []Cop {
	mu.Lock()
	defer mu.Unlock()
	return append([]Cop(nil), registry...)
}
