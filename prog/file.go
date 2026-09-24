package prog

import "github.com/dgageot/rubocop-go/cop"

// FromFile runs a file cop over the program's resolved syntax and types.
// Scope limits candidate files without removing packages from the analysis.
func FromFile(c cop.Cop) *Func {
	return New(cop.Meta{Name: c.Name(), Description: c.Description(), Severity: c.Severity()}, func(p *Pass) {
		for _, pkg := range p.Program.Packages {
			for _, file := range pkg.Syntax {
				pass := &cop.Pass{Cop: c, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
				if scoped, ok := c.(cop.Scoped); ok && !scoped.InScope(pass) {
					continue
				}
				c.Check(pass)
				for _, offense := range pass.Offenses() {
					p.ReportOffense(offense)
				}
			}
		}
	})
}
