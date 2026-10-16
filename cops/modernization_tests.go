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
	return p.Program.PackagesWithTests()
}
