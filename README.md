# rubocop-go

A Go source code analyzer inspired by [RuboCop](https://rubocop.org/). The goal is to make it as easy as possible to implement custom checks (called "cops") for Go codebases.

## How it works

Each cop is a simple Go struct that implements the `cop.Cop` interface: give it a name, a description, a severity, and a `Check` function that inspects an AST file and reports offenses. That's it.

## Built-in cops

| Cop | Description |
|-----|-------------|
| `Lint/OsExit` | Detects direct calls to `os.Exit` |
| `Lint/FmtPrint` | Detects direct calls to `fmt.Print*` |
| `Lint/CloneCompleteness` | Checks that clone methods copy all fields |
| `Lint/ConfigVersionImport` | Checks config version imports |
| `Style/ErrorNaming` | Enforces error variable naming conventions |
| `Style/EmptyFunc` | Detects empty function bodies |

## Usage

```sh
# Analyze current directory
go run . .

# Analyze specific paths
go run . ./pkg ./cmd
```

## Configuration

Cops are configured via `.rubocop-go.yml`:

```yaml
cops:
  Lint/OsExit:
    enabled: true
  Style/ErrorNaming:
    enabled: true
  Style/EmptyFunc:
    enabled: true
```

## Writing a custom cop

There are two ways to ship a cop, depending on whether you want to use
rubocop-go as a CLI or embed it in your own program.

### As a library (recommended for project-specific cops)

Assemble your own slice of cops and pass them to `runner.New`. No globals,
no init order:

```go
package main

import (
	"os"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

func main() {
	mine := []cop.Cop{
		cops.NewLintOsExit(),
		&MyCop{Meta: cop.Meta{
			CopName:     "Style/MyCop",
			CopDesc:     "Checks something useful",
			CopSeverity: cop.Convention,
		}},
	}

	r := runner.New(mine, config.DefaultConfig(), os.Stdout)
	count, _ := r.Run([]string{"."})
	if count > 0 {
		os.Exit(1)
	}
}

type MyCop struct{ cop.Meta }

func (c *MyCop) Check(p *cop.Pass) {
	// Inspect p.File and call p.Report(node, format, args...) on offenses.
}
```

See `examples/embed` for a runnable version of this pattern.

### Plugged into the bundled CLI

If you want your cop to ship inside the bundled `rubocop-go` CLI, add it to
the `cops/` package and register it from an `init()`:

```go
func init() { cop.Register(NewMyCop()) }
```

Then `main.go` picks it up automatically via `cop.All()`.

### Type-aware cops

For cops that need type information, implement the optional `cop.TypeAware`
interface (a `NeedsTypes() bool` method that returns true). The runner will
then populate `p.Info` and `p.Package` on the `*cop.Pass`.

