# rubocop-go

A Go source code analyzer inspired by [RuboCop](https://rubocop.org/). The goal is to make it as easy as possible to implement custom checks (called "cops") for Go codebases.

## How it works

A cop is a piece of metadata (name, description, severity), an optional file
scope, and a function that inspects a Go file and reports offenses. The
recommended way to assemble one is `cop.New` (or a `cop.Func` literal when
you need scope/types):

```go
var LintOsExit = cop.New(cop.Meta{
    Name:        "Lint/OsExit",
    Description: "Avoid os.Exit outside of main()",
    Severity:    cop.Warning,
}, func(p *cop.Pass) {
    p.ForEachFunc(func(fn *ast.FuncDecl) {
        // ... inspect and report ...
    })
})
```

## Built-in cops

| Cop | Description |
|-----|-------------|
| `Lint/OsExit` | Detects direct calls to `os.Exit` |
| `Lint/FmtPrint` | Detects direct calls to `fmt.Print*` |
| `Lint/CloneCompleteness` | Checks that clone methods copy all fields |
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
    "go/ast"
    "os"

    "github.com/dgageot/rubocop-go/config"
    "github.com/dgageot/rubocop-go/cop"
    "github.com/dgageot/rubocop-go/cops"
    "github.com/dgageot/rubocop-go/runner"
)

var MyCop = cop.New(cop.Meta{
    Name:        "Style/MyCop",
    Description: "Checks something useful",
    Severity:    cop.Convention,
}, func(p *cop.Pass) {
    // Inspect p.File and call p.Report(node, format, args...) on offenses.
})

func main() {
    mine := []cop.Cop{
        cops.NewLintOsExit(),
        MyCop,
    }

    r := runner.New(mine, config.DefaultConfig(), os.Stdout)
    count, _ := r.Run([]string{"."})
    if count > 0 {
        os.Exit(1)
    }
}
```

See `examples/embed` for a runnable version of this pattern.

### Restricting a cop to a subset of files

Most cops only apply to a specific area of the tree. Declare the scope
once on the cop and the runner will skip out-of-scope files for you —
no `if !p.FileMatches(...) { return }` boilerplate at the top of every
Check function:

```go
var TUIViewPurity = &cop.Func{
    Meta: cop.Meta{
        Name:        "Lint/TUIViewPurity",
        Description: "View() must not mutate the receiver",
        Severity:    cop.Warning,
    },
    Scope: cop.UnderDir("pkg/tui"),
    Run: func(p *cop.Pass) { /* ... */ },
}
```

The bundled scope helpers are:

| Helper | Matches |
|--------|---------|
| `cop.OnlyFile("pkg/runtime/event.go")` | one specific file |
| `cop.UnderDir("pkg/tui")` | every file under a directory |
| `cop.InPathSegment("pkg/config", pred)` | files whose path contains `parent/<seg>/...` and `pred(seg)` is true |
| `cop.NotBlackBoxTest()` | files whose package is not `<dir>_test` |
| `cop.And(a, b)` / `cop.Or(a, b)` / `cop.Not(a)` | logical composition |

### Plugged into the bundled CLI

If you want your cop to ship inside the bundled `rubocop-go` CLI, add it to
the `cops/` package and register it from an `init()`:

```go
func init() { cop.Register(NewMyCop()) }
```

Then `main.go` picks it up automatically via `cop.All()`.

### Type-aware cops

For cops that need type information, set `Types: true` on a `cop.Func`:

```go
var LintCloneCompleteness = &cop.Func{
    Meta:  cop.Meta{Name: "Lint/CloneCompleteness", ...},
    Types: true,
    Run:   func(p *cop.Pass) { /* p.Info and p.Package are populated */ },
}
```

(Or, for a hand-rolled struct, implement the `cop.TypeAware` interface
with a `NeedsTypes() bool` method.)

## Helper toolbox

Cops never have to reinvent common AST plumbing — the `cop` package ships
helpers for the recurring shapes:

- **AST walking** — `Pass.ForEachFunc`, `ForEachCall`, `ForEachAssign`,
  `ForEachStruct`, `ForEachStructField`, `ForEachMethodCall`,
  `ForEachConst`, `ForEachImport`.
- **Symbol queries** — `Pass.IdentSetFromCalls`, `SelectorReceivers`,
  `StringConsts`, `StringConstNodes`, `Pass.FuncDecl(name)`,
  `Pass.StructType(name)`, `Pass.FirstMethodCall(name)`,
  `Pass.PointerReceiverMethods(name)` / `Pass.ValueReceiverMethods(name)`.
- **Path scope** — `Pass.FileMatches`, `FileUnder`, `PathSegment`,
  `IsTestFile`, `IsBlackBoxTest` (plus the `cop.OnlyFile`/`UnderDir`/...
  scope helpers above).
- **Cross-file** — `Pass.ParseSibling(rel)` to read a sibling file,
  `Pass.ParseDir(rel, opts)` to scan a directory.
- **Diagnostics** — `Pass.Report(node, fmt, ...)`,
  `Pass.ReportAt(pos, end, fmt, ...)`,
  `Pass.ReportMissing(anchor, fmt, names)` for the recurring "X is
  missing entries for: a, b, c" pattern.
- **Match helpers** — `cop.IsCallTo`, `cop.CallTo` (returns the matched
  name), `IsSelector`, `MatchSelector`, `Receiver`, `FieldNames`,
  `ImportPath`, `IsNullaryFunc` / `IsNullarySig`.
- **Composite literals** — `cop.StringField(cl, key)`,
  `cop.BasicLitField(cl, key, kind)`, `cop.CompositeLitField(cl, key)`
  for the recurring "extract a field's value out of a struct literal".
- **Struct tags** — `cop.ParseTagOptions(tag, "json")` returns a
  `TagOptions` with `Has`/`HasAny`/`HasName`/`IsSkipped` for clean
  modifier checks. `cop.FieldTag(field)` unquotes a field's tag literal
  into a `reflect.StructTag`.
- **Context-scope analysis** — `cop.WalkFuncWithContextScope`,
  `SignatureHasContext`, `BodyDeclaresContext`, `IsContextType`,
  `IsContextProducer` for "prefer the *Context variant" rules.
- **Suppression** — `//rubocop:disable Lint/Foo` (single line),
  `//rubocop:disable-file Lint/Foo` (whole file).
