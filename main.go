package main

import (
	"fmt"
	"os"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	_ "github.com/dgageot/rubocop-go/cops" // register built-in cops
	"github.com/dgageot/rubocop-go/runner"
)

func main() {
	paths := os.Args[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}

	cfg, err := config.Load(".rubocop-go.yml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Inspecting Go files with %d cop(s)\n", len(cop.All()))

	r := runner.New(cop.All(), cfg, os.Stdout)

	offenseCount, err := r.Run(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if offenseCount > 0 {
		os.Exit(1)
	}
}
