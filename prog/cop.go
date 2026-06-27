package prog

import (
	"go/token"

	"golang.org/x/tools/go/ssa"

	"github.com/dgageot/rubocop-go/cop"
)

// Pass carries everything a whole-program [Cop] needs. Unlike cop.Pass —
// which is scoped to a single file — a prog.Pass exposes the entire loaded
// [Program], so a cop can follow values across function and package
// boundaries.
//
// Offenses are reported through the same cop.Offense vocabulary the rest of
// rubocop-go uses, so the runner and reporters need no special casing.
type Pass struct {
	Cop     Cop
	Program *Program

	// SeverityOverride mirrors cop.Pass: when non-nil it replaces the
	// cop's default severity on every offense, as configured in
	// .rubocop-go.yml.
	SeverityOverride *cop.Severity

	offenses []cop.Offense
}

// Reportf records an offense at the given position. Values lowered from
// synthetic code may lack a position; Reportf still records the offense,
// anchored at the no-position sentinel, so nothing is silently dropped.
func (p *Pass) Reportf(pos token.Pos, format string, args ...any) {
	p.ReportAtf(pos, pos, format, args...)
}

// ReportAtf records an offense covering [pos, end).
func (p *Pass) ReportAtf(pos, end token.Pos, format string, args ...any) {
	o := cop.NewOffenseFor(p.Cop.Name(), p.Cop.Severity(), p.Program.Fset, pos, end, sprintf(format, args...))
	if p.SeverityOverride != nil {
		o.Severity = *p.SeverityOverride
	}
	p.offenses = append(p.offenses, o)
}

// Offenses returns the offenses accumulated so far.
func (p *Pass) Offenses() []cop.Offense {
	return p.offenses
}

// Cop is a rule that inspects the whole program rather than a single file.
// It shares the metadata vocabulary of cop.Cop (Name/Description/Severity)
// so configuration, severity overrides, and reporting are identical; only
// the Check signature differs, taking a whole-program [Pass].
type Cop interface {
	Name() string
	Description() string
	Severity() cop.Severity

	// Check inspects the whole program and reports offenses via p.Report.
	Check(p *Pass)
}

// Func is the standard way to build a whole-program cop: provide cop.Meta
// and a Run function, mirroring cop.Func.
type Func struct {
	cop.Meta

	Run func(*Pass)
}

// New is shorthand for a Func.
func New(meta cop.Meta, run func(*Pass)) *Func {
	return &Func{Meta: meta, Run: run}
}

// Name implements [Cop].
func (f *Func) Name() string { return f.Meta.Name }

// Description implements [Cop].
func (f *Func) Description() string { return f.Meta.Description }

// Severity implements [Cop].
func (f *Func) Severity() cop.Severity { return f.Meta.Severity }

// Check implements [Cop].
func (f *Func) Check(p *Pass) {
	if f.Run != nil {
		f.Run(p)
	}
}

// AllFunctions returns every SSA function with a body in the loaded
// initial packages, in a deterministic order, so cops iterate reproducibly.
func (p *Program) AllFunctions() []*ssa.Function {
	return p.allFunctions
}

func collectFunctions(fn *ssa.Function, seen map[*ssa.Function]bool) {
	if fn == nil || seen[fn] {
		return
	}
	seen[fn] = true
	var buf [10]*ssa.Value
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			for _, op := range instr.Operands(buf[:0]) {
				if op == nil {
					continue
				}
				if fn, ok := (*op).(*ssa.Function); ok {
					collectFunctions(fn, seen)
				}
			}
		}
	}
	for _, anon := range fn.AnonFuncs {
		collectFunctions(anon, seen)
	}
}
