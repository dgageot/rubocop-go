// Embed shows how to use rubocop-go as a library: assemble your own slice of
// cops and run them, without touching the global registry.
//
//	go run ./examples/embed [path...]
package main

import (
	"fmt"
	"go/ast"
	"os"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

// MyCustomCop is a project-specific rule defined inline as a cop.Func.
// No struct, no constructor — the meta + check live next to each other.
var MyCustomCop = cop.New(cop.Meta{
	Name:        "Style/SayHello",
	Description: "Functions named SayHello must take no arguments",
	Severity:    cop.Convention,
}, func(p *cop.Pass) {
	p.ForEachFunc(func(fn *ast.FuncDecl) {
		if fn.Name.Name != "SayHello" {
			return
		}
		if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
			p.Report(fn.Name, "SayHello must take no arguments")
		}
	})
})

func main() {
	// Build the set of cops to run yourself. Mix and match built-in cops with
	// your own without going through cop.Register / cop.All.
	mine := []cop.Cop{
		cops.NewLintOsExit(),
		cops.NewStyleErrorNaming(),
		MyCustomCop,
	}

	paths := os.Args[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}

	r := runner.New(mine, config.DefaultConfig(), os.Stdout)
	count, err := r.Run(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if count > 0 {
		os.Exit(1)
	}
}
