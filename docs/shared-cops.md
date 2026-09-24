# Shared opt-in cops

These cops are available through `cops.NewLint…` constructors. They are not
included in `All()` or `AllProgram()`: consumers explicitly select their policies.
The catalog descriptions match each cop's `Meta.Description`.

| Cop | Description | Target Go |
| --- | --- | --- |
| `Lint/SlogContextual` | Use contextual slog calls when a context is available. | — |
| `Lint/ConstructorPurity` | Avoid starting goroutines in constructors. | — |
| `Lint/ConstructorNetworkIO` | Avoid network I/O in constructors. | — |
| `Lint/WrapErrors` | Preserve error chains when formatting errors. | — |
| `Lint/ErrorStringMatching` | Prefer structured error checks over matching error text. | — |
| `Lint/DeferMutexUnlock` | Prefer deferred unlocking for terminal critical sections. | — |
| `Lint/NewExpr` | Replace address-only temporaries with new expressions. | 1.26+ |
| `Lint/PointerHelper` | Replace AWS scalar pointer helpers with new expressions. | 1.26+ |
| `Lint/ReflectFields` | Use reflection iterators for paired field metadata and values. | 1.26+ |
| `Lint/StdlibUUID` | Prefer standard-library UUIDs for compatible string-producing operations. | 1.27+ |
| `Lint/URLClone` | Use URL.Clone for equivalent manual deep copies. | 1.27+ |
| `Lint/JSONMarshalWrite` | Consider json.MarshalWrite instead of encoding and trimming a newline. | 1.27+ |
| `Lint/BenchmarkLoop` | Consider b.Loop for simple benchmark loops. | 1.24+ |
| `Lint/SplitTrimJoin` | Use strings.CutLast to remove trailing segments without splitting. | 1.27+ |
| `Lint/FieldsSeq` | Use strings.FieldsSeq when fields are only iterated once. | 1.24+ |
| `Lint/StreamCloseSafety` | Flag potentially unsynchronized field access between Close and Next/Recv. | — |

## Embedding

File cops return `*cop.Func`; program cops return `*prog.Func`. Keep program
cops program-backed: they need resolved imports or cross-file information.

```go
fileCops := []cop.Cop{
    cops.NewLintSlogContextual(),
    cops.NewLintWrapErrors(),
    cops.NewLintNewExpr(),
}
programCops := []prog.Cop{
    cops.NewLintFieldsSeq(),
    cops.NewLintBenchmarkLoop(),
    cops.NewLintStreamCloseSafety(),
}
r := runner.New(fileCops, config.DefaultConfig(), os.Stdout).
    WithProgramCops(programCops)
```

Every constructor returns a fresh instance. All except `StreamCloseSafety`
accept `cop.FuncOption` values, including `cop.WithScope`. Scope restricts
candidate files without discarding analysis inputs:

```go
check := cops.NewLintURLClone(
    cop.WithScope(cop.Not(cop.UnderDir("legacy"))),
)
```

Project-specific exclusions belong in the consumer. No configuration directories
or module paths are implicitly exempt. Several modernization cops skip generated
code; `FieldsSeq` and `SplitTrimJoin` retain their broader coverage. Add a scope
if your project excludes generated files from those checks too.

Version requirements use the target module and file build constraints, never the
linter's toolchain. Language features use the effective file language version;
standard-library APIs use the newer of the module and file requirements. Iterator
recommendations check both API availability and range-over-function syntax.
Unknown or older targets are skipped. Options cannot lower built-in minimums;
`cop.WithMinGoVersion` and `cop.WithMinStdlibVersion` may raise them. Run analysis
with a toolchain that supports the target project. The full fixture suite needs
Go 1.27+; Go 1.26 still runs portable pattern tests, skipping only fixtures that
use newer APIs or load Go 1.27 modules.

## Examples and limitations

These cops report suggestions, not automatic fixes.

### Logging, errors, and lifecycle

- **SlogContextual:** `slog.Info("started")` → `slog.InfoContext(ctx, "started")`
  when a context is available. Checks top-level helpers, not logger methods.
- **ConstructorPurity:** move `go worker()` from `NewClient` to an explicit
  lifecycle method. Checks direct construction-time statements, including IIFEs,
  not side effects hidden in helpers. Intentionally lazy closures are excluded.
- **ConstructorNetworkIO:** move `net.Dial(...)` from a constructor to an explicit
  operation. Covers selected `net`/`net/http` APIs, not every network method.
  Network I/O and background work in constructors can be intentional; opt in
  only when this matches your lifecycle policy.
- **WrapErrors:** `fmt.Errorf("read: %v", err)` → `fmt.Errorf("read: %w", err)`.
  Checks builtin `error` arguments and leaves calls already containing `%w`
  alone. Deliberate error abstraction boundaries may need suppression.
- **ErrorStringMatching:** replace `strings.Contains(err.Error(), "missing")`
  with `errors.Is` or `errors.As` when the API exposes structured errors.
  Tests are excluded; arbitrary comparisons and stored error strings are not
  analyzed.
- **DeferMutexUnlock:** consider `mu.Lock(); defer mu.Unlock()` instead of a
  terminal manual unlock. Matching is syntactic. This is not proof of equivalent
  panic behavior, receiver capture, or return-expression evaluation timing.
- **StreamCloseSafety:** synchronize fields shared by `Close` and `Next`/`Recv`,
  or make them reader-owned. Opt in only for APIs whose contract permits these
  methods to run concurrently. The analysis follows receiver fields and helpers
  with common mutexes, not arbitrary aliases, containers, or cancellation.

### Modernization

- **NewExpr:** `v := value; return &v` → `return new(value)`. Requires adjacent
  statements, a fresh single-use temporary, and constant companion results.
- **PointerHelper:** `aws.Int64(42)` → `new(int64(42))`. Only AWS SDK v2 scalar
  pointer helpers are matched; preserve conversions and argument evaluation.
- **ReflectFields:** paired `v.Field(i)`/`v.Type().Field(i)` loops →
  `for field, value := range v.Fields()`. Receiver mutation, escape, unrelated
  index uses, and unsafe callbacks are excluded.
- **StdlibUUID:** Google UUID `NewString()` → standard-library
  `uuid.NewV4().String()`. Only compatible string-producing patterns are matched;
  exposed UUID types, UUIDv5, and general parsing are excluded. Randomness
  configuration in production or tests suppresses recommendations, even when
  those files are outside the configured candidate scope.
- **URLClone:** replace an equivalent nil-safe deep-copy helper with `u.Clone()`.
  Ordinary shallow copies are excluded because cloning also copies userinfo.
- **JSONMarshalWrite:** buffered `Encoder.Encode` followed by newline trimming →
  consider `jsonv2.MarshalWrite`. Keep the local buffer and error returns, use
  v1-compatible options and explicit HTML escaping, discard partial output on
  error, and review custom marshalers and evaluation order. Streams and indented
  encoders are excluded.
- **BenchmarkLoop:** `b.ResetTimer(); for range b.N { work() }` → consider
  `for b.Loop() { work() }`. Review setup lifetime, measurement, and compiler
  effects. Includes sub-benchmarks and test-only packages; excludes parallel,
  timer-sensitive, and escaping benchmark handles.
- **SplitTrimJoin:** replace splitting, dropping trailing segments, and rejoining
  with repeated `strings.CutLast`. Preserve the first segment and predicate order;
  only single-byte separators are matched to avoid overlapping-separator changes.
- **FieldsSeq:** `for _, word := range strings.Fields(input)` →
  `for word := range strings.FieldsSeq(input)`. Preserve original input evaluation
  and empty-input fallbacks. Mutable byte slices, callbacks, repeated traversal,
  indexing, and direct `recover` calls are excluded.
