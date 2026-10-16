package cops

import (
	"fmt"

	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/prog"
)

// The shared SSA program omits tests, including packages with no production files.
// Return healthy variants alongside any load error so unrelated checks can proceed.
func loadModernizationTests(p *prog.Pass) ([]*packages.Package, error) {
	pkgs, err := loadModernizationPackages(p)
	if err != nil {
		return nil, err
	}
	var healthy []*packages.Package
	for _, pkg := range pkgs {
		if pkg.IllTyped || len(pkg.Errors) != 0 {
			if err == nil {
				if len(pkg.Errors) != 0 {
					err = fmt.Errorf("loading %s: %s", pkg.PkgPath, pkg.Errors[0].Msg)
				} else {
					err = fmt.Errorf("loading %s: package is ill-typed", pkg.PkgPath)
				}
			}
			continue
		}
		healthy = append(healthy, pkg)
	}
	return healthy, err
}

// Retain healthy packages when callers can work around individual type errors.
func loadModernizationPackages(p *prog.Pass) ([]*packages.Package, error) {
	return p.Program.PackagesWithTests()
}
