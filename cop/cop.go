// Package cop defines the core types for writing custom cops.
//
// A cop is a rule that inspects Go source code and reports offenses.
// To write a custom cop:
//
//  1. Create a struct that implements the Cop interface
//  2. Register it with cop.Register in an init() function
//  3. Import the package from cops/register.go
package cop

import (
	"go/ast"
	"go/token"
	"go/types"
	"sync"
)

// Severity represents the severity level of an offense.
type Severity int

const (
	Convention Severity = iota // style violation
	Warning                   // potential issue
	Error                     // definite bug
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

// Cop is the interface that all cops must implement.
type Cop interface {
	// Name returns the cop's qualified name (e.g. "Lint/OsExit").
	Name() string

	// Description returns a short human-readable description of what the cop checks.
	Description() string

	// Severity returns the default severity for offenses reported by this cop.
	Severity() Severity

	// Check inspects an AST file and returns any offenses found.
	Check(fset *token.FileSet, file *ast.File) []Offense
}

// TypeAwareCop is an optional interface for cops that need type information.
// When a cop implements this interface, the runner will perform type-checking
// and call CheckTyped instead of Check.
type TypeAwareCop interface {
	Cop

	// CheckTyped inspects an AST file with full type information and returns any offenses found.
	CheckTyped(fset *token.FileSet, file *ast.File, info *types.Info, pkg *types.Package) []Offense
}

// NewOffense creates an offense for a given cop.
func NewOffense(c Cop, fset *token.FileSet, pos token.Pos, end token.Pos, message string) Offense {
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
