package cops

import (
	"go/token"
	"slices"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// Check every healthy package variant, reporting only candidates safe in all of them.
func modernizationProgram(c *cop.Func) *prog.Func {
	return &prog.Func{
		Meta: c.Meta,
		Run: func(p *prog.Pass) {
			pkgs, err := loadModernizationPackages(p)
			if err != nil {
				p.Reportf(token.NoPos, "cannot inspect modernization candidates: %v", err)
				return
			}
			var filenames []string
			offensesByFile := make(map[string][]cop.Offense)
			for _, pkg := range pkgs {
				if pkg.IllTyped {
					continue
				}
				for _, file := range pkg.Syntax {
					pass := &cop.Pass{Cop: c, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
					filename := p.Program.Fset.PositionFor(file.Package, false).Filename
					if !c.InScope(pass) {
						continue
					}
					c.Check(pass)
					previous, seen := offensesByFile[filename]
					if !seen {
						filenames = append(filenames, filename)
						offensesByFile[filename] = pass.Offenses()
						continue
					}
					// Test-only methods can change which rewrites are safe in either direction.
					offensesByFile[filename] = slices.DeleteFunc(previous, func(offense cop.Offense) bool {
						return !slices.Contains(pass.Offenses(), offense)
					})
				}
			}
			for _, filename := range filenames {
				for _, offense := range offensesByFile[filename] {
					p.ReportOffense(offense)
				}
			}
		},
	}
}
