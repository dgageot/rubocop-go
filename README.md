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

The bundled CLI runs these five file cops and the whole-program cop below by
default. `cops.All()` and `cops.AllProgram()` return the same default cops.

| Cop | Description |
|-----|-------------|
| `Lint/OsExit` | Detects direct calls to `os.Exit` |
| `Lint/FmtPrint` | Detects direct calls to `fmt.Print*` |
| `Lint/CloneCompleteness` | Checks that clone methods copy all fields |
| `Style/ErrorNaming` | Enforces error variable naming conventions |
| `Style/EmptyFunc` | Detects empty function bodies |

### Shared opt-in cops

42 additional cops cover context usage, logging, library output, errors,
constructor lifecycles, stream synchronization, channel safety, and Go
modernization.
They are deliberately **not** included in `All()` or `AllProgram()`, and the
CLI does not run them by default.
Select them by name in [`.rubocop-go.yml`](#configuration), or use their named
constructors in an embedded runner.
See the [shared cop catalog](docs/shared-cops.md) for brief descriptions,
examples, minimum Go versions, and limitations.

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

The CLI reads `.rubocop-go.yml` from the current directory. Without this file,
the CLI runs the six default cops.

The `cops` map accepts the name of every shipped cop. Each entry can set these
keys:

- `enabled`: `true` runs the cop, and `false` disables it.
- `severity`: `convention`, `warning`, or `error` replaces the reported
  severity. The CLI ignores other values.

```yaml
cops:
  Lint/OsExit:
    severity: error   # Default cop with a severity override
  Style/EmptyFunc:
    enabled: false    # Disabled default cop
  Lint/WrapErrors:
    enabled: true     # Selected shared opt-in cop
```

Set `default_cops: false` to make the `cops` map an allowlist. The CLI then runs
only the cops that have `enabled: true`:

```yaml
default_cops: false

cops:
  Lint/NoStdoutInLibraries:
    enabled: true
  Lint/NoFatalOutsideMain:
    enabled: true
  Lint/OsExit:
    enabled: true
    severity: error
```

The CLI applies these rules to each shipped cop, in this order:

1. An explicit `enabled` value wins.
2. Otherwise, `default_cops: false` disables the cop.
3. Otherwise, the cop runs only if it is a default cop.

An omitted `default_cops` value behaves like `true`. This value keeps the six
default cops. It does not enable the shared opt-in cops. An entry without
`enabled`, such as a severity-only entry, does not select an opt-in cop. An
allowlist without enabled cops runs zero cops.

| Configuration | Cops that the CLI runs |
| --- | --- |
| No file, an empty file, or `default_cops: true` | The six default cops |
| A default cop with `enabled: false` | The other default cops |
| An opt-in cop with `enabled: true` | The default cops and that cop |
| An opt-in cop with only `severity` | The default cops |
| `default_cops: false` | Only the cops with `enabled: true` |

The CLI ignores names that are not shipped cops, as earlier versions did. Names
are case-sensitive, so `lint/osexit` is also an unknown name. An entry with an
unknown name does not select or disable a cop, and it does not change a
severity. The CLI does not report unknown names, and they do not change the
exit status. With `default_cops: false`, a file that contains only unknown
names runs zero cops.

Because of this behavior, one file can also configure cops that only a custom
embedded runner supplies. The tradeoff is that the CLI does not find typos. An
entry with a misspelled name has no effect. It does not disable, select, or
change the severity of the intended cop. Copy names from the
[built-in cops](#built-in-cops) table or the
[shared cop catalog](docs/shared-cops.md). The first output line,
`Inspecting Go files with N cop(s)`, shows the number of cops that the CLI
runs.

The CLI exits with status 1 for invalid YAML or a value of the wrong type,
also in an entry with an unknown name. As before, it exits with status 1 when
the analysis finds offenses.

The configuration selects complete cops. It cannot restrict a cop to some paths
or pass constructor options such as `cop.WithScope`. Use `//rubocop:disable`
comments, or build an embedded runner with named constructors.

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

### Configuration in embedded runners

`runner.New` and `WithProgramCops` filter the cops that you supply with
`Config.IsEnabled`. The runner does not use the CLI catalog. It accepts any cop
name, and it ignores configured names that do not match a supplied cop. A
supplied cop runs unless the configuration disables it. With
`default_cops: false`, only supplied cops with `enabled: true` run.
`config.DefaultConfig()` and a zero-value `config.Config` run every supplied
cop.

To select shipped cops the way the CLI does, use `cops.Catalog()` with
`Config.IsEnabledWithDefault`. Then append your own cops:

```go
var files []cop.Cop
var programs []prog.Cop
for _, e := range cops.Catalog() {
    if !cfg.IsEnabledWithDefault(e.Name, e.Default) {
        continue
    }
    if e.File != nil {
        files = append(files, e.File())
    } else {
        programs = append(programs, e.Program())
    }
}
files = append(files, MyCop)
r := runner.New(files, cfg, os.Stdout).WithProgramCops(programs)
```

Catalog constructors take no options. Call a named constructor to add options
such as `cop.WithScope`.

### Selecting execution modes

Keep file and whole-program cops in explicitly typed lists. Program cops share
one loaded program, and test-aware adapters share its lazy test-package cache:

```go
files := []cop.Cop{
    cops.NewLintSlogContextual(),
    runner.WithDirectoryTypes(cops.NewLintConstructorCommandExecFile()),
}
programs := []prog.Cop{
    prog.FromFileWithTests(cops.NewLintSlicesCloneFile()),
    cops.NewLintContextConnectivity(),
}
r := runner.New(files, config.DefaultConfig(), os.Stdout).WithProgramCops(programs)
```

`prog.FromFile` uses the production program's resolved files.
`prog.FromFileWithTests` includes internal/external tests and partially typed
packages, visiting the first in-scope variant of each physical filename. It is
not a replacement for modernization cops that require agreement across all
healthy variants.

`runner.WithDirectoryTypes` is a best-effort adapter for checks that must also
see inactive files. It checks all same-package Go siblings, including tests and
conflicting platform files, using the standard importer. It supplies partial
`Types`, `Defs`, and `Uses` information, not module-aware imports or a module Go
version. Use the normal runner or program adapters for checks requiring those.

File cops can define a pure `Candidate func(*ast.File) bool` prefilter, or use
`cop.WithCandidate`. Returning false must guarantee the full check cannot report
an offense. It runs before the check and directory preparation, independently
of policy `Scope`. It does not skip the normal runner's package loading.
The shared constructor-command and slog cops provide their own prefilters.

### Sharing production and test analysis

For read-only program cops that inspect both production and tests, opt into a
single type-checked snapshot:

```go
r := runner.New(files, config.DefaultConfig(), os.Stdout).
    WithProgramCops(programs).
    WithSharedProgramLoad()
```

Sharing is best-effort: discovery or combined-load failures retain the default
production load and lazy test loading. Otherwise the loader discovers production
roots, then type-checks their directories with
tests once. Production SSA excludes test variants; `PackagesWithTests` reuses
the loaded test view. File cops still run first with their existing all-files
and import-resolution behavior, including inactive files. This does not cache
results across runs or run cops concurrently.

Unlike the default lazy test load, test sources and errors are observed when
program analysis starts. Do not enable this for cops that modify source files,
package data, or the build environment between program checks. Use
`prog.LoadDirWithTests` for the same snapshot behavior without a runner.

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
the `cops/` package as a `NewMyCop()` constructor. Then add one entry, in name
order, to the slice that `cops.Catalog()` returns in `cops/register.go`:

```go
func Catalog() []Entry {
    return []Entry{
        // ... existing entries ...
        {Name: "Style/MyCop", File: func() cop.Cop { return NewMyCop() }},
    }
}
```

Use `Program: func() prog.Cop { ... }` for a whole-program cop. Register each
name once, in its canonical execution mode. An alternate `*File` constructor
for custom adapters does not get its own entry. Tests check that every other
exported constructor has exactly one entry.

Users select the new cop with `enabled: true` in `.rubocop-go.yml`. Set
`Default: true` only if every CLI user must run the cop without configuration.
`cops.All()` and `cops.AllProgram()` return the default entries, so a default
cop also reaches embedders that call them.

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

Use `prog.FromFile(c)` when a file-shaped cop needs the program's fully resolved
imports and types. The adapter honors file scope without removing analysis
inputs and preserves diagnostic spans and runner severity overrides.

For language-dependent recommendations, use `cop.WithMinGoVersion("go1.26")`;
for standard-library APIs, use `cop.WithMinStdlibVersion("go1.27")`. The checks
use target module/file requirements, not the linter toolchain; older or unknown
targets are skipped. Both guards are independent of `cop.WithScope`.

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
