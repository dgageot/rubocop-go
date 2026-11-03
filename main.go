package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
)

// configPath is the configuration file that the CLI reads from the working directory.
const configPath = ".rubocop-go.yml"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns its exit status: 0 when the analysis finds
// no offenses, and 1 when it finds offenses or an error occurs.
func run(args []string, stdout, stderr io.Writer) int {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	fileCops, programCops := selectCops(cfg)
	r := runner.New(fileCops, cfg, stdout).
		WithProgramCops(programCops)

	offenseCount, err := r.Run(paths)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if offenseCount > 0 {
		return 1
	}
	return 0
}

// selectCops returns fresh instances of the catalog cops that cfg enables, in
// catalog order. Selection only looks up catalog names, so a configured name
// that is not in the catalog has no effect.
func selectCops(cfg *config.Config) ([]cop.Cop, []prog.Cop) {
	var (
		fileCops    []cop.Cop
		programCops []prog.Cop
	)
	for _, e := range cops.Catalog() {
		if !cfg.IsEnabledWithDefault(e.Name, e.Default) {
			continue
		}
		if e.File != nil {
			fileCops = append(fileCops, e.File())
		} else {
			programCops = append(programCops, e.Program())
		}
	}
	return fileCops, programCops
}
