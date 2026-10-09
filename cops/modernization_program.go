package cops

import (
	"go/token"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// Include resolved production and test packages, deduplicating test variants.
func modernizationProgram(c *cop.Func) *prog.Func {
	return &prog.Func{
		Meta: c.Meta,
		Run: func(p *prog.Pass) {
			pkgs, err := loadModernizationPackages(p)
			if err != nil {
				p.Reportf(token.NoPos, "cannot inspect modernization candidates: %v", err)
				return
			}
			seen := make(map[string]bool)
			for _, pkg := range pkgs {
				if pkg.IllTyped {
					continue
				}
				for _, file := range pkg.Syntax {
					pass := &cop.Pass{Cop: c, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
					if seen[pass.Filename()] || !c.InScope(pass) {
						continue
					}
					seen[pass.Filename()] = true
					c.Check(pass)
					for _, offense := range pass.Offenses() {
						p.ReportOffense(offense)
					}
				}
			}
		},
	}
}
