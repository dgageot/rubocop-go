// Embed shows how to use rubocop-go as a library: assemble your own slice of
// cops and run them, without touching the global registry.
//
//	go run ./examples/embed [path...]
package main

import (
	"fmt"
	"os"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

func main() {
	// Build the set of cops to run yourself. Mix and match built-in cops with
	// your own without going through cop.Register / cop.All.
	mine := []cop.Cop{
		cops.NewLintOsExit(),
		cops.NewStyleErrorNaming(),
		// Add custom cops here:
		// &mypkg.MyCustomCop{...},
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
