# rubocop-go

A Go source code analyzer inspired by [RuboCop](https://rubocop.org/). The goal is to make it as easy as possible to implement custom checks (called "cops") for Go codebases.

## How it works

Each cop is a simple Go struct that implements the `cop.Cop` interface: give it a name, a description, a severity, and a `Check` function that inspects an AST file and reports offenses. That's it.

For cops that need type information, implement the optional `cop.TypeAware`
interface (a `NeedsTypes() bool` method that returns true). The runner will
then populate `p.Info` and `p.Package` on the `*cop.Pass`.

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

1. Create a struct implementing `cop.Cop`
2. Register it in an `init()` function with `cop.Register`
3. Import its package from `cops/register.go`

```go
package cops

import (
	"github.com/dgageot/rubocop-go/cop"
)

func init() {
	cop.Register(MyCop{})
}

type MyCop struct{}

func (c MyCop) Name() string        { return "Style/MyCop" }
func (c MyCop) Description() string { return "Checks something useful" }
func (c MyCop) Severity() cop.Severity { return cop.Convention }

func (c MyCop) Check(p *cop.Pass) []cop.Offense {
	// Inspect the AST and return offenses
	return nil
}
```
