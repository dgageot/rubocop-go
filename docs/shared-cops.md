# Shared opt-in cops

These cops are available through `cops.NewLint…` constructors. They are not
included in `All()` or `AllProgram()`: consumers explicitly select their policies.
The catalog descriptions match each cop's `Meta.Description`.

| Cop | Description | Target Go |
| --- | --- | --- |
| `Lint/ContextFirstParameter` | Put context.Context first in function parameter lists. | — |
| `Lint/NoContextField` | Pass contexts to operations instead of storing them in structs. | — |
| `Lint/HTTPRequestWithContext` | Use http.NewRequestWithContext to construct HTTP requests. | — |
| `Lint/NoFatalOutsideMain` | Reserve log.Fatal calls for package main. | — |
| `Lint/SlogContextual` | Use contextual slog calls when a context is available. | — |
| `Lint/ConstructorPurity` | Avoid starting goroutines in constructors. | — |
| `Lint/ConstructorNetworkIO` | Avoid network I/O in constructors. | — |
| `Lint/ConstructorCommandExec` | Avoid preparing or executing external commands in constructors. | — |
| `Lint/NoStdoutInLibraries` | Use caller-provided writers instead of stdout in library packages. | — |
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
| `Lint/CutPrefix` | Use strings or bytes CutPrefix for paired prefix checks and removals. | 1.20+ |
| `Lint/CutSuffix` | Use strings or bytes CutSuffix for paired suffix checks and removals. | 1.20+ |
| `Lint/FieldsSeqLookup` | Use strings.FieldsSeq for first-field and membership lookups. | 1.24+ |
| `Lint/SlicesClone` | Consider slices.Clone for shallow copies, preserving nilness, types, and capacity contracts. | 1.21+ |
| `Lint/SortStableFunc` | Use slices.SortStableFunc for simple integer and string comparisons. | 1.21+ |
| `Lint/ErrorsAsType` | Use errors.AsType for fresh error targets consumed only on success. | 1.26+ |
| `Lint/MapsCopy` | Use maps.Copy for plain map entry copy loops. | 1.21+ |
| `Lint/MapsClone` | Use maps.Clone for equivalent nil-safe shallow map copy helpers. | 1.21+ |
| `Lint/SlicesContains` | Use slices.Contains for simple slice membership helpers. | 1.21+ |
| `Lint/SlicesEqual` | Use slices.Equal for simple element-wise slice equality helpers. | 1.21+ |
| `Lint/SplitSeq` | Use strings.SplitSeq or SplitAfterSeq when split results are only iterated once. | 1.24+ |
| `Lint/SortedMapKeys` | Use slices.Sorted(maps.Keys(m)) for equivalent sorted map key collection. | 1.23+ |
| `Lint/WaitGroupGo` | Consider WaitGroup.Go for simple Add/go/Done patterns. | 1.25+ |
| `Lint/HTTPTestRequestWithContext` | Use httptest.NewRequestWithContext for immediate test-request context attachment. | 1.23+ |
| `Lint/StreamCloseSafety` | Flag potentially unsynchronized field access between Close and Next/Recv. | — |
| `Lint/CancellationLoop` | Leave polling loops after context cancellation. | — |
| `Lint/ClosedChannelLoop` | Stop polling a channel after observing closure. | — |
| `Lint/ChannelStateCheck` | Do not use channel length or capacity checks to justify blocking operations. | — |
| `Lint/CancellationSafeResultSend` | Do not strand result senders when callers return on cancellation. | — |
| `Lint/NonblockingChannelReplacement` | Keep channel replacement fallbacks nonblocking. | — |
| `Lint/SlicesConcat` | Consider slices.Concat for concatenating slices into fresh storage. | 1.22+ |

## Embedding

File cops return `*cop.Func`; program cops return `*prog.Func`. Keep program
cops program-backed: they need resolved imports or cross-file information.

```go
fileCops := []cop.Cop{
    cops.NewLintContextFirstParameter(),
    cops.NewLintNoContextField(),
    cops.NewLintHTTPRequestWithContext(),
    cops.NewLintNoFatalOutsideMain(),
    cops.NewLintSlogContextual(),
    cops.NewLintWrapErrors(),
    cops.NewLintNewExpr(),
    cops.NewLintNoStdoutInLibraries(),
}
programCops := []prog.Cop{
    cops.NewLintConstructorCommandExec(),
    cops.NewLintFieldsSeq(),
    cops.NewLintFieldsSeqLookup(),
    cops.NewLintCutPrefix(),
    cops.NewLintCutSuffix(),
    cops.NewLintSlicesClone(),
    cops.NewLintSortStableFunc(),
    cops.NewLintBenchmarkLoop(),
    cops.NewLintStreamCloseSafety(),
    cops.NewLintCancellationLoop(),
    cops.NewLintClosedChannelLoop(),
    cops.NewLintChannelStateCheck(),
    cops.NewLintCancellationSafeResultSend(),
    cops.NewLintNonblockingChannelReplacement(),
    cops.NewLintSlicesConcat(),
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
code; `FieldsSeq`, `FieldsSeqLookup`, and `SplitTrimJoin` retain their broader coverage. Add a scope
if your project excludes generated files from those checks too.

Version requirements use the target module and file build constraints, never the
linter's toolchain. Language features use the effective file language version;
standard-library APIs use the newer of the module and file requirements. Iterator
recommendations check both API availability and range-over-function syntax.
Unknown or older targets are skipped. The type checker’s Go 1.21 language floor for
older build constraints does not raise standard-library API availability. Options
cannot lower built-in minimums;
`cop.WithMinGoVersion` and `cop.WithMinStdlibVersion` may raise them. Run analysis
with a toolchain that supports the target project. The full fixture suite needs
Go 1.27+; Go 1.26 still runs portable pattern tests, skipping only fixtures that
use newer APIs or load Go 1.27 modules.

## Examples and limitations

These cops report suggestions, not automatic fixes.

### Context usage and process termination

These four file cops skip test files. Matching is syntactic: import and type
aliases are not resolved, and unrelated identifiers with the expected spelling
can match. Add `cop.WithScope` to restrict them to selected production directories;
no project-specific directory exclusions are built in.

- **ContextFirstParameter:** `func run(id string, ctx context.Context)` →
  `func run(ctx context.Context, id string)`. Checks the first syntactic context
  parameter in function and method declarations, including bodyless declarations.
  Function literals, function types, and interface signatures are excluded.
- **NoContextField:** replace `type Service struct { ctx context.Context }` with
  context parameters on operations. Checks direct fields, including embedded
  contexts, in top-level named structs. Local and nested anonymous structs,
  pointer fields, and containers are excluded. Deliberate context wrappers may
  need suppression.
- **HTTPRequestWithContext:** `http.NewRequest(...)` →
  `http.NewRequestWithContext(ctx, ...)`. Reports construction even if `WithContext`
  or `Clone` attaches a context later. Does not prove caller-context propagation or
  inspect other HTTP APIs.
- **NoFatalOutsideMain:** return an error instead of calling `log.Fatal`, `Fatalf`,
  or `Fatalln` in library code. All of **package main** is exempt, including helpers
  and `init`, not just `func main`. Logger instance methods and other termination
  APIs are excluded.

### Logging, errors, and lifecycle

- **SlogContextual:** `slog.Info("started")` → `slog.InfoContext(ctx, "started")`
  when a context is lexically available, respecting declaration order and shadowing.
  Checks top-level slog helpers, including calls in package-level function literals,
  not logger methods. Blank and unnamed parameters do not provide a usable context;
  `context.Cause` and `context.AfterFunc` are not context producers.
- **ConstructorPurity:** move `go worker()` from `NewClient` to an explicit
  lifecycle method. Checks direct construction-time statements, including converted
  IIFEs and inline range functions, not side effects hidden in helpers. Unevaluated `unsafe` operands
  are excluded. Intentionally lazy closures are excluded.
- **ConstructorNetworkIO:** move `net.Dial(...)` from a constructor to an explicit
  operation. Covers selected `net`/`net/http` APIs, including `net.ListenConfig`
  listeners, not every network method.
  Goroutine bodies are excluded, but their synchronously evaluated receivers and
  arguments are checked. Network I/O and background work in constructors can be intentional; opt in
  only when this matches your lifecycle policy.
- **ConstructorCommandExec:** move `exec.Command(...)` and `cmd.Run()` from
  constructors to explicit operations. Covers `Command`, `CommandContext`, and
  resolved `exec.Cmd` methods `Start`, `Run`, `Output`, and `CombinedOutput`,
  including aliases and promoted methods. Unlike the original Docker cop,
  unrelated methods named `Run` or `Start` are not reported. Without types, only
  syntactic `exec.Command`/`CommandContext` calls match. Immediate and deferred
  closures are checked; stored closures and indirect helpers are not. Goroutine
  bodies are excluded, but their synchronously evaluated receivers and arguments
  are checked. Preparing a command does not itself start a process; this is an
  opt-in lifecycle policy, not proof of process execution.
- **NoStdoutInLibraries:** `fmt.Println(...)` or `fmt.Fprintln(os.Stdout, ...)` →
  write to a caller-provided `io.Writer`. All non-main packages are candidates,
  not just a `pkg/` directory; tests are excluded. Matching is syntactic, so
  import aliases are not resolved and shadowed identifiers can match. Other
  stdout-writing APIs are not covered. Selecting `Lint/FmtPrint` as well can
  produce overlapping diagnostics.
- **WrapErrors:** `fmt.Errorf("read: %v", err)` → `fmt.Errorf("read: %w", err)`.
  Checks builtin `error` arguments, including aliases, and leaves calls already containing `%w`
  alone. Resolved `fmt.Errorf` identity and constant formats are used; indexed operands,
  width/precision arguments, escaped percent verbs, and type-only `%T`/`%p` output
  do not imply error-message interpolation. Deliberate error abstraction boundaries
  may need suppression.
- **ErrorStringMatching:** replace `strings.Contains(err.Error(), "missing")`
  with `errors.Is` or `errors.As` when the API exposes structured errors.
  Tests are excluded; arbitrary comparisons and stored error strings are not
  analyzed. Import aliases and implicit pointer-receiver calls are resolved.
- **DeferMutexUnlock:** consider `mu.Lock(); defer mu.Unlock()` instead of a
  terminal manual unlock. Matching is syntactic. Early returns, jumps, and
  intervening lock operations are excluded. This is not proof of equivalent panic behavior, receiver capture,
  or return-expression evaluation timing.
- **StreamCloseSafety:** synchronize fields shared by `Close` and `Next`/`Recv`,
  or make them reader-owned. Opt in only for APIs whose contract permits these
  methods to run concurrently. The analysis follows receiver fields and helpers
  with common mutexes, not arbitrary aliases, containers, or cancellation.
  Stored closures and callbacks do not inherit locks from their declaration site;
  immediate closures do. Value-receiver writes to copied fields are excluded,
  while shared pointees remain checked; copied mutexes do not prove synchronization.
  Receiver copies are checked before method-body locks. Helper unlocks invalidate
  caller locks, aggregate copies include their fields, and `go`/`defer` arguments
  retain their synchronous evaluation context. Possible asynchronous releases
  invalidate held-lock assumptions. Only receiver-method helpers with
  loaded bodies are followed; test-only implementations are not loaded.

### Channel safety

These five cops use resolved types and skip generated files. `CancellationLoop`,
`ClosedChannelLoop`, and `ChannelStateCheck` also inspect internal/external tests
and test-only packages, retaining usable partial types from test packages.
Production packages must still load through the shared SSA loader.
`CancellationSafeResultSend` and `NonblockingChannelReplacement` inspect
production packages only. They are
local pattern checks, not proofs of deadlock or race freedom, and do not rewrite code.
The loop checks recognize direct testing failures/skips, `runtime.Goexit`, and
unconditional Testify failures with concrete `*testing.T`, `*testing.B`, or
`*testing.F` arguments. Conditional assertions, unknown helpers, and interface-only
test handles remain conservative; deferred or asynchronous exits do not stop the caller.

- **CancellationLoop:** a `case <-ctx.Done(): break` inside `for { select { ... } }`
  only exits the select. Return or break the enclosing loop instead. Flags paths
  that can revisit the same select after cancellation, including fallthrough and
  `continue`. Conditional loops, context aliases, helper side effects, and custom
  cancellation protocols are not proved.
- **ClosedChannelLoop:** after `value, ok := <-ch`, leave the loop or disable/replace
  the channel when `!ok`. Tracks simple closure tests through local control flow
  in unconditional loops, including labelled exits. Receives without explicit
  closure tests, channel aliases, and arbitrary helper effects are excluded.
- **ChannelStateCheck:** `if len(ch) > 0 { <-ch }` and
  `if len(ch) < cap(ch) { ch <- value }` do not reserve a value or capacity. Prefer
  `select` or explicit synchronization. Matching covers local guards and resolved
  storage expressions, not channel aliases or indexed containers. Nonblocking
  select communications are excluded. The cop does not prove locks or exclusive
  ownership; intentionally synchronized or private queues may need suppression.
- **CancellationSafeResultSend:** a goroutine sending on an unbuffered result
  channel can be stranded when its sole receiver returns on context cancellation.
  Select on matching cancellation or use a buffered one-shot result. Recognizes
  straight-line setup, one inline producer, simple aliases, context copies, and
  `WithValue`. Every returning cancellation arm must be covered by the producer.
  Escaped channels/contexts, extra receivers, unresolved context constructors,
  and buffered channels are excluded. Buffering is not sufficient for an arbitrary
  stream; the cop does not prove that a producer will finish other work.
- **NonblockingChannelReplacement:** a failed nonblocking send followed by a
  blocking drain and resend can block if another consumer empties the queue.
  Keep both fallback operations nonblocking. Matches adjacent operations on the
  same local or stable field channel, excluding effectful send expressions and
  indexed channels. It does not prove competing consumers or exclusive access,
  and only matches the fully blocking fallback, not partially repaired variants.

### Modernization

- **NewExpr:** `v := value; return &v` → `return new(value)`. Requires adjacent
  statements, a fresh single-use temporary, and constant companion results.
- **PointerHelper:** `aws.Int64(42)` → `new(int64(42))`. Only AWS SDK v2 scalar
  pointer helpers are matched, not methods or calls whose results are discarded
  directly by expression, defer, or go statements. Preserve conversions and
  argument evaluation. Helpers must have the expected nongeneric scalar-to-pointer
  signature.
- **ReflectFields:** paired `v.Field(i)`/`v.Type().Field(i)` loops →
  `for field, value := range v.Fields()`. Keep an initial `_ = v.NumField()`
  to preserve invalid/non-struct panic behavior. Receiver mutation, escape, unrelated
  index uses, and unsafe callbacks are excluded.
- **StdlibUUID:** Google UUID `NewString()` → standard-library
  `uuid.NewV4().String()`. Only compatible string-producing patterns are matched;
  exposed UUID types, UUIDv5, and general parsing are excluded. Randomness
  configuration in production, tests, or imported dependencies suppresses random-UUID
  recommendations, including assignments or address escapes of `crypto/rand.Reader`,
  even outside the configured candidate scope. Canonical literal parsing remains
  eligible independently of randomness configuration or randomness-load failures,
  and literal-only candidates do not load randomness dependencies. Direct
  `go`/`defer NewString()` calls are excluded to preserve generation timing.
- **URLClone:** replace an equivalent nil-safe deep-copy helper with `u.Clone()`.
  Ordinary shallow copies are excluded because cloning also copies userinfo. Retain
  an existing nil guard when an interface result must remain a nil interface.
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

- **CutPrefix:** `if strings.HasPrefix(s, prefix) { return strings.TrimPrefix(s, prefix) }`
  → `if rest, ok := strings.CutPrefix(s, prefix); ok { return rest }`.
  Also recognizes string slices at matching byte offsets, exit guards, and selected
  tagless switch cases. Preserve short-circuit evaluation and assignments to
  existing variables. Calls or mutations between the check and removal,
  fallthrough cases, and named-string slices are excluded.
- **CutSuffix:** the corresponding `HasSuffix` plus `TrimSuffix` or
  `s[:len(s)-len(suffix)]` → `strings.CutSuffix`. Restricted to adjacent uses of
  local strings or constants, with optional pure string wrappers. Preserve
  original values, assignment scope, and evaluation order. Compound conditions,
  field/index sources, and named-string slices are excluded.
- **FieldsSeqLookup:** guarded `words := strings.Fields(s); ...; words[0]` →
  extract the first field with `FieldsSeq` at the original declaration, retaining
  the empty-input fallback. `slices.Contains(strings.Fields(s), word)` → iterate
  until a match. Evaluate input and search value once, in their original order,
  even for empty input. Keep surrounding calls and fallback code outside the
  iterator body. Bytes, predicates, escaping slices, and other slice uses are
  excluded. Complements `FieldsSeq` without reporting its range-loop patterns.
- **SlicesClone:** `append([]T(nil), source...)` or adjacent `make`/`copy` →
  consider `slices.Clone(source)`. This is **not** a mechanical replacement:
  nil-destination append yields nil for non-nil empty input, while Clone preserves
  source nilness; empty-literal append and make/copy can return non-nil for nil
  input. Preserve the destination's named type and any capacity contract too.
  Includes production, internal tests, external tests, and test-only packages;
  generated code is excluded. Explicit capacity and effectful repeated sources
  are not matched.
- **SlicesConcat:** nested or adjacent appends into fresh storage, such as
  `append(append([]T(nil), a...), b...)`, suggest `slices.Concat(a, b)`.
  Requires Go 1.22+. Skips in-place appends, scalar tails, mixed named slice
  types, and inputs with calls or other side effects. Includes fresh nil/empty
  slices, zero-length `make`, and `slices.Clone` seeds. Preserve nil versus
  non-nil empty results, named destination types, capacity assumptions, and
  allocation/panic behavior; `Concat` returns nil for an empty result. Production
  packages only; generated files are excluded. Suggestions can overlap with
  `SlicesClone` and are never automatic rewrites.
- **SortStableFunc:** simple `sort.SliceStable` callbacks comparing integer or
  string keys → `slices.SortStableFunc` with `cmp.Compare`. Reverse arguments for
  descending keys and preserve lazy tie-break evaluation; eagerly computing all
  comparisons can dereference nil pointers that the original never touched.
  Floating-point keys (NaN ordering), effectful callbacks, deferred/goroutine
  sorting calls, tests, and generated code are excluded.

- **ErrorsAsType:** fresh `var e *E; if errors.As(err, &e) { ... }` →
  `if e, ok := errors.AsType[*E](err); ok { ... }`. Target reads must stay in
  the success branch, without taking its address. Only guaranteed direct matches
  or concrete leaf errors without custom `As`/`Unwrap` methods are reported:
  a custom `As` can retain the target pointer, whereas `AsType` returns a copy.
  Arbitrary interface targets not implementing `error` are excluded. Generic
  targets require the identical source type parameter: reflection assignability
  between named and unnamed instantiations is not equivalent to a type assertion.
  Composite generic types are excluded from the leaf-error fallback unless
  assignability is already established.
  Includes resolved production, internal/external tests, and test-only packages;
  skips generated files. Safety must hold in every healthy production/test package
  variant, including methods added by internal tests.

- **MapsCopy:** Plain `for k, v := range src { dst[k] = v }` → `maps.Copy(dst, src)`. Only local identifiers and exact key/value types are matched; transformations, filters, extra work, and externally assigned loop variables are excluded. Separate merges keep their original order; nil destinations are not initialized. Selecting MapsClone too can produce overlapping diagnostics. Map-constrained type
  parameters are supported when all terms have the same key/value shape; complex
  constraint intersections are conservatively excluded. Includes resolved production, internal/external tests,
  and test-only packages; generated code is excluded.

- **MapsClone:** Exact nil-safe shallow-copy helpers → `maps.Clone(src)`. Requires matching source/result/destination types and an explicit nil-return guard. Deep copies, unconditional allocation, custom capacity hints, and key types that may contain NaNs are excluded. Includes resolved production, internal/external tests,
  and test-only packages; generated code is excluded.

- **SlicesContains:** Exact boolean slice-membership helpers → `slices.Contains(xs, needle)`. Requires comparable elements and an inert parameter or compatible constant needle. Side effects, field/index/dereference needles, nil-sensitive guards, and incompatible generic argument inference are excluded. Includes resolved production, internal/external tests,
  and test-only packages; generated code is excluded.

- **SlicesEqual:** Exact length-plus-element equality helpers → `slices.Equal(a, b)`. Preserves comparable-element equality, including NaNs and interface-comparison panics; nil and empty remain equal. Nil-sensitive checks, side effects, arrays, and incompatible named slice types are excluded. Includes resolved production, internal/external tests,
  and test-only packages; generated code is excluded.

- **SplitSeq:** Direct value-only `range strings.Split(s, sep)` or `SplitAfter` → corresponding Seq API. Requires both iterator syntax and API availability. Preserve input/separator evaluation and empty/trailing fragments. Indexed ranges, retained slices, mutable bytes, and direct recover are excluded. Includes resolved production, internal/external tests,
  and test-only packages; generated code is excluded.

- **SortedMapKeys:** Adjacent nil-slice declaration, unfiltered key collection, and ascending sort → `slices.Sorted(maps.Keys(src))`. Restricts keys to integers/strings and destinations to unnamed slices. Non-nil empty results, capacity hints, floats, custom ordering, and delayed traversal are excluded. Preserve any downstream capacity contract; exact capacity is not guaranteed by the API. This is a clarity suggestion, not an allocation-performance guarantee. Includes resolved production, internal/external tests,
  and test-only packages; generated code is excluded.

- **WaitGroupGo:** Adjacent `wg.Add(1); go func() { defer wg.Done(); ... }()` → consider `wg.Go`. Requires exact sync methods, a stable local receiver, no arguments, and the matching defer first. Reassigned or escaping receiver variables, promoted methods, extra group operations (including direct
  aliases), and direct recover are excluded. Advisory only: the callback must not let a panic escape; review captures and completion timing. Includes resolved production, internal/external tests,
  and test-only packages; generated code is excluded.

- **Byte prefix/suffix cuts:** CutPrefix and CutSuffix also recognize
  `if bytes.HasPrefix(s, p) { return bytes.TrimPrefix(s, p) }` and suffix
  equivalents, including matching byte-offset slices. Only simple conditions
  on local unnamed `[]byte` values and an immediate removal are matched.
  Preserve nilness, capacity, and backing-array aliasing. Calls, mutations,
  compound conditions, wrappers, named slices, and full-slice capacity changes
  are excluded.

- **HTTPTestRequestWithContext:** direct
  `httptest.NewRequest("GET", "/", nil).WithContext(ctx)` →
  `httptest.NewRequestWithContext(ctx, "GET", "/", nil)`. Matches constant
  method/target strings, a nil body, and a local context identifier or inert
  `context.Background`/`TODO` call. Keep context evaluation and request validation
  in their original order; verify the supplied context is non-nil. Direct `go`/`defer`
  calls are excluded because they move construction timing. Bodies,
  effectful context expressions, fields, globals, retained request temporaries,
  and arbitrary non-contextual requests are excluded. Program-backed and includes
  test-only/internal/external packages; generated code is excluded. Complements
  HTTPRequestWithContext without changing its production-only policy.
