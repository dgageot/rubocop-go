# rubocop-go

A Go source code analyzer inspired by [RuboCop](https://rubocop.org/). The goal is to make it as easy as possible to implement custom checks (called "cops") for Go codebases.

## How it works

A cop is a piece of metadata (name, description, severity), an optional file
scope, and a function that inspects a Go file and reports offenses. The
recommended way to assemble one is `cop.New`:

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

### Whole-program cops

These run once over the entire loaded program rather than once per file,
so they can answer inter-procedural, cross-package questions:

| Cop | Description |
|-----|-------------|
| `Lint/ContextConnectivity` | Proves every `context.Context` derives from the program's root context |

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
    // Inspect p.File and call p.Report(node, message) or p.Reportf(node, format, args...) on offenses.
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
var TUIViewPurity = cop.New(cop.Meta{
    Name:        "Lint/TUIViewPurity",
    Description: "View() must not mutate the receiver",
    Severity:    cop.Warning,
}, func(p *cop.Pass) {
    /* ... */
}, cop.WithScope(cop.UnderDir("pkg/tui")))
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

For cops that need type information, use `cop.WithTypes()`:

```go
var LintCloneCompleteness = cop.New(cop.Meta{Name: "Lint/CloneCompleteness", ...},
    func(p *cop.Pass) { /* p.Info and p.Package are populated */ },
    cop.WithTypes(),
)
```

(Or, for a hand-rolled struct, implement the `cop.TypeAware` interface
with a `NeedsTypes() bool` method.)

### Whole-program, inter-procedural cops

Some questions cannot be answered one file — or even one package — at a
time. "Does every `context.Context` consumed anywhere in the program
derive from the single root context?" requires following values across
parameters, returns, and package boundaries. The `prog` package provides
the substrate for those rules.

`prog.Load` type-checks the whole program with `go/packages`, lowers it to
SSA (`go/ssa`), and builds a CHA call graph. A whole-program cop is a
`prog.Cop`: like a normal cop it carries `cop.Meta`, but its `Check` takes
a `*prog.Pass` exposing the entire `*prog.Program` instead of a single
file.

The core dataflow primitive is `Program.Origins`, an inter-procedural
backward tracer. Given an SSA value, it walks def-use chains backwards —
looking through calls (to their callees' returns), parameters (to the
actual arguments at every call site in the call graph), phi nodes, and the
copy-like instructions — and returns the set of source values that flow
into it. Two hooks tune the walk:

- `Stop` halts at a domain boundary and records the value as an origin.
- `Redirect` looks *through* an otherwise-opaque call to a chosen
  argument (e.g. `context.WithCancel(parent)` → `parent`).

```go
var MyProgramCop = prog.New(cop.Meta{
    Name:        "Lint/MyRule",
    Description: "...",
    Severity:    cop.Warning,
}, func(p *prog.Pass) {
    for _, fn := range p.Program.AllFunctions() {
        // inspect fn's SSA, call p.Program.Origins(v, opts),
        // and p.Reportf(pos, ...) on offenses.
    }
})
```

Run them by registering on the runner with `WithProgramCops`:

```go
r := runner.New(cops.All(), cfg, os.Stdout).
    WithProgramCops(cops.AllProgram())
```

Whole-program analysis is best-effort: if the program cannot be loaded
(e.g. outside a module) the runner prints a notice and skips the
program cops rather than failing. Offenses flow through the same
reporting, severity-override, and `//rubocop:disable` suppression
machinery as file cops.

`coptest.RunProgram` writes a multi-file program to a temp module, loads
it, and runs a whole-program cop against it — see
`cops/lint_context_connectivity_test.go` for cross-package examples.

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
- **Diagnostics** — `Pass.Report(node, message)`,
  `Pass.Reportf(node, fmt, ...)`, `Pass.ReportAt(pos, end, message)`,
  `Pass.ReportAtf(pos, end, fmt, ...)`,
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
