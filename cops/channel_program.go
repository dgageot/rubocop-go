package cops

import (
	"go/token"
	"go/types"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
)

// Channel checks also inspect test-only packages and partially resolved code.
func typedChannelProgram(c *cop.Func) *prog.Func {
	return prog.New(c.Meta, func(p *prog.Pass) {
		pkgs, err := p.Program.PackagesWithTests()
		if err != nil {
			p.Reportf(token.NoPos, "cannot inspect channel operations: %v", err)
			return
		}
		seen := make(map[string]bool)
		for _, pkg := range pkgs {
			for _, file := range pkg.Syntax {
				pass := &cop.Pass{Cop: c, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
				filename := p.Program.Fset.PositionFor(file.Package, false).Filename
				if seen[filename] || !c.InScope(pass) {
					continue
				}
				seen[filename] = true
				c.Check(pass)
				for _, offense := range pass.Offenses() {
					p.ReportOffense(offense)
				}
			}
		}
	})
}

func channelType(t types.Type) *types.Chan {
	if t == nil {
		return nil
	}
	ch, _ := types.Unalias(t).Underlying().(*types.Chan)
	return ch
}
