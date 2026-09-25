package cops

import (
	"fmt"

	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/prog"
)

// The shared SSA program omits tests, including packages with no production files.
func loadModernizationTests(p *prog.Pass) ([]*packages.Package, error) {
	pkgs, err := loadModernizationPackages(p)
	if err != nil {
		return nil, err
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) != 0 {
			return nil, fmt.Errorf("loading %s: %s", pkg.PkgPath, pkg.Errors[0].Msg)
		}
	}
	return pkgs, nil
}

// Retain healthy packages when callers can work around individual type errors.
func loadModernizationPackages(p *prog.Pass) ([]*packages.Package, error) {
	var dirs []string
	seen := make(map[string]bool)
	for _, pkg := range p.Program.Packages {
		if pkg.Dir != "" && !seen[pkg.Dir] {
			dirs = append(dirs, pkg.Dir)
			seen[pkg.Dir] = true
		}
	}
	if len(dirs) == 0 {
		return nil, nil
	}
	cfg := &packages.Config{
		Mode: packages.NeedModule | packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Dir:  dirs[0], Fset: p.Program.Fset, Tests: true,
	}
	pkgs, err := packages.Load(cfg, dirs...)
	if err != nil {
		return nil, err
	}
	return pkgs, nil
}
