// Package cop defines the core types for writing custom cops.
//
// A cop is a rule that inspects Go source code and reports offenses.
// To write a custom cop:
//
//  1. Create a struct that implements the Cop interface
//  2. Register it with cop.Register in an init() function (optional)
//  3. Or pass it explicitly to runner.New
package cop

import (
	"go/ast"
	"go/token"
	"go/types"
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
	FileSet *token.FileSet
	File    *ast.File
	Info    *types.Info
	Package *types.Package
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

	// Check inspects a file and returns any offenses found.
	Check(p *Pass) []Offense
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

// Register adds a cop to the global registry.
// Typically called from init() functions.
func Register(c Cop) {
	mu.Lock()
	defer mu.Unlock()
	registry = append(registry, c)
}

// All returns all registered cops.
func All() []Cop {
	mu.Lock()
	defer mu.Unlock()
	return append([]Cop(nil), registry...)
}
