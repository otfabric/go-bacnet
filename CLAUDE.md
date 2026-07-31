# CLAUDE.md — OT Fabric Go protocol libraries

Guidance for humans and coding agents working on OT Fabric Go protocol
libraries.

This file defines shared defaults, not a requirement for identical APIs.
Repository-local truth takes precedence, in this order:

1. Current code and tests
2. `go.mod`
3. Repository-local `CLAUDE.md`
4. `README.md`, `API.md`, and other maintained documentation
5. This shared guide

When these disagree, verify the current behaviour before editing and correct
stale documentation as part of the change.

## Mission

Build small, idiomatic, production-usable Go libraries for OT and industrial
protocols.

Optimize for:

- clear and honest scope;
- protocol-correct wire behaviour;
- predictable ownership, lifecycle, and concurrency;
- actionable errors;
- testability and interoperability;
- quiet, opt-in observability;
- clean layering between protocol libraries.

Do not optimize for identical APIs across fundamentally different protocols.

## Protocol and dependency boundaries

Typical dependency direction:

```text
go-serial / net
    └── go-modbus
          ├── go-sunspec
          └── go-modbus-identity

go-tpkt
    └── go-cotp
          ├── go-s7comm
          └── go-mms
                └── go-iec61850

go-opcua and go-otfp are largely standalone.
```

General rules:

- A package owns only the concerns of its layer.
- Do not reimplement lower-layer framing, transport, retry, or diagnostics in a
  higher layer without a concrete protocol or consumer need.
- Do not expose lower-layer details through a higher-level API unless they are
  intentionally part of that API.
- A wrapper around an injected client normally does not own that client's
  lifecycle unless the contract explicitly says otherwise.
- When changing an exported API in a shared lower layer, inspect and test known
  OT Fabric consumers before merging.
- Bump and release dependencies from lower layers upward. Humans own tags and
  releases unless explicitly delegating that work.

## Start by inspecting the repository

Before proposing or implementing a quality pass:

1. Read `go.mod`, `README.md`, package documentation, public error definitions,
   configuration types, examples, tests, Makefile, and CI workflows.
2. Identify the repository's actual role: codec, transport, client, server,
   scanner, domain layer, CLI, or a combination.
3. Record current behaviour before proposing an ideal replacement.
4. Separate:
   - confirmed defects;
   - documentation gaps;
   - API or behavioural improvements;
   - optional new features.
5. Recheck file-level findings against the current branch before editing.

Do not infer missing behaviour from sibling repositories.

## Scope and API design

- Prefer small, focused packages and interfaces.
- Keep leaf codecs and framing libraries minimal.
- Do not add logging, metrics, retries, context wrappers, lifecycle methods, or
  configuration abstractions merely for family parity.
- `New...` should normally construct or validate. Use names such as `Dial`,
  `Open`, `Connect`, `Listen`, or `Serve` when external I/O occurs.
- Existing public APIs should not be broken solely to look like another OT
  Fabric library.
- A breaking change must solve an independent correctness, safety, usability,
  or maintenance problem.
- Prefer additive improvements and migration paths over immediate redesigns.
- Validate configuration at a clear boundary and return actionable errors.
- Avoid silent clamping. When compatibility requires clamping, document it and
  expose the decision through configured logging or returned state where useful.

## Context, cancellation, and timeouts

Use `context.Context` for operations that can genuinely support cancellation or
deadlines, especially network RPCs, connection setup, discovery, and graceful
shutdown.

- Do not add context to pure encode/decode helpers.
- Do not claim cancellation works when the underlying operation cannot be
  safely interrupted.
- Document cancellation by phase when relevant: queue wait, dial, handshake,
  write, response wait, subscription, or shutdown.
- Returning early must not leave a shared session desynchronized.
- If cancellation requires aborting the connection, say so.
- Preserve `context.Canceled` and `context.DeadlineExceeded` through
  `errors.Is`.
- Document configured timeout precedence and any platform-specific limitations.
- A per-request timeout must not accidentally alter unrelated concurrent
  operations on a shared connection.

## Ownership, lifetime, and concurrency

Every API accepting or returning a connection, stream, buffer, handler, or
callback must make ownership clear.

Document, where applicable:

- who closes an injected connection or client;
- whether ownership transfers on success, immediately, or never;
- whether `Close` unblocks pending operations;
- whether returned byte slices alias internal or caller-owned memory;
- whether callback arguments may be retained;
- whether a value is safe for concurrent use;
- whether one reader and one writer may run concurrently;
- whether requests are serialized or multiplexed;
- what happens to queued and in-flight work during shutdown.

For stateful types, document the lifecycle near the primary usage example.

## Errors

Use normal Go error semantics.

- Export sentinels only for stable conditions callers can meaningfully act on.
- Use `errors.Is` for categories and `errors.As` for structured detail.
- Use typed errors when location or protocol detail matters, such as field,
  offset, model, function, status, or phase.
- Preserve useful underlying causes, normally with `%w`.
- Do not require every error to wrap a sentinel.
- Do not make consumers parse error strings.
- Keep wire-level status codes distinct from local validation, transport, and
  lifecycle errors.
- Document partial, best-effort, and mixed-result operations explicitly.
- For writes and controls, document retry and execution ambiguity when a request
  may have reached the peer but the response was lost.

Tiny codec libraries may document a small sentinel set in GoDoc or `API.md`.
Larger error surfaces should have an `ERRORS.md`.

## Logging

Libraries are silent by default.

- Do not use `log.Default()` or `slog.Default()` as an implicit library logger.
- Prefer `*slog.Logger` for new structured-logging APIs.
- Existing custom logger interfaces may be retained for compatibility; adapters
  are preferable to disruptive rewrites.
- A nil or omitted logger must disable output.
- Avoid expensive formatting and allocation when logging is disabled.
- Inject logging at a natural runtime boundary and propagate it internally.
- Adjacent layers may keep separate loggers. Inheritance must be explicit,
  documented, and overrideable.
- Do not log credentials, keys, authentication material, complete control
  payloads, or unbounded wire data.
- Raw frame or payload diagnostics must be explicit opt-in and document data
  sensitivity, ownership, and volume.

Add `OBSERVABILITY.md` only when the repository has enough logging, metrics, or
observer behaviour to justify it.

## Metrics and observers

Metrics are optional, not a family requirement.

Add them only when there is a concrete consumer or operational use case.

- Keep metrics interfaces small and protocol-appropriate.
- Nil or omitted metrics must have negligible overhead.
- Document whether callbacks are synchronous and whether they may run
  concurrently.
- Do not invoke user callbacks while holding internal protocol-state locks.
- Use bounded operation and outcome names.
- Do not use raw error strings, device identifiers, object references, endpoints,
  or payloads as metric labels by default.
- Distinguish a logical operation from retry attempts where the distinction
  matters.
- Scanner and discovery observers are progress or audit events, not necessarily
  request metrics.
- Higher layers should not duplicate lower-layer transport metrics unless they
  add meaningful domain semantics.

## Retries and reconnects

- Retries and reconnection are separate behaviours.
- Reconnection must not silently imply replay of a failed operation.
- Unknown errors should not be retried by default.
- Be conservative with writes, controls, and other non-idempotent operations.
- Document whether retries provide at-most-once, at-least-once, or uncertain
  execution behaviour.
- When execution may be ambiguous after transmission, expose or document that
  state rather than reporting an ordinary definitive failure.
- Preserve existing behaviour during documentation-only quality passes; audit
  and change retry policy separately.

## Documentation

Documentation depth should match the repository's actual complexity and
maturity. Do not create files merely to make repositories look uniform.

### README.md

A public library README should normally include:

1. Purpose and scope
2. Position in the OT Fabric stack, when relevant
3. Explicit out-of-scope items
4. Installation and supported Go version
5. A correct getting-started example with errors handled
6. Important ownership, lifecycle, concurrency, or timeout notes
7. Links to deeper documentation and examples
8. License

Badges are useful when accurate:

- Go version
- pkg.go.dev
- license
- CI
- coverage
- latest release

The Go version in the badge, text, CI, and `go.mod` must agree.

### Package documentation

Each public package should explain:

- what it implements;
- what it does not implement;
- its primary entry points;
- important ownership or concurrency rules;
- protocol boundaries.

Use `doc.go` when it improves discoverability. It is not mandatory when an
existing package comment already serves the purpose well.

### API.md

Use `API.md` when the public behavioural contract is too large for README and
GoDoc.

Useful topics include:

- construction and lifecycle;
- ownership and buffer aliasing;
- concurrency;
- timeout and cancellation behaviour;
- EOF and stream semantics;
- public configuration;
- result models;
- limitations.

Do not duplicate generated GoDoc mechanically.

### ERRORS.md

Use when consumers need a non-trivial taxonomy, such as:

- sentinels versus typed errors;
- local errors versus wire status;
- partial or best-effort results;
- examples using `errors.Is` and `errors.As`.

### OBSERVABILITY.md

Use when logging, metrics, raw hooks, observers, redaction, or callback semantics
need more than a short README section.

### Known limitations and interoperability

Large protocol libraries should state unsupported services, known constraints,
and the scope of interoperability evidence.

Do not claim hardware or vendor interoperability that has not been tested.

### Examples

- Prefer runnable `Example...` functions for compact API demonstrations.
- Use `// Output:` only when output is deterministic.
- Larger examples may live under `examples/`.
- README snippets must match current exported names and signatures.
- Prefer offline, fixture, loopback, or in-process examples for automated tests.
- Never ignore errors in published examples unless the omission is explicitly
  part of a focused snippet and cannot mislead readers.

### Release notes

Use the repository's existing release-note convention. When `RELEASE.md` is the
source:

- place the newest release first;
- state the version and date;
- summarize meaningful additions, changes, fixes, and dependency updates;
- call out behavioural changes even when source-compatible;
- state when there are no public API changes;
- do not invent breaking changes.

## Repository layout

Prefer the simplest layout appropriate to the module:

```text
go-<protocol>/
├── *.go
├── internal/              # only when useful
├── cmd/                   # optional CLIs
├── examples/              # optional larger examples
├── testdata/              # fixtures and sanitized captures
├── docs/ or spec/         # only for maintained, useful material
├── API.md                 # when warranted
├── ERRORS.md              # when warranted
├── OBSERVABILITY.md       # when warranted
├── README.md
├── RELEASE.md             # when used by the repository
├── CONTRIBUTING.md
├── SECURITY.md
├── LICENSE
├── Makefile
└── .github/workflows/
```

Guidelines:

- A flat root package is appropriate for small libraries.
- Split implementation files by concern; avoid both needless fragmentation and
  oversized god files.
- Empty placeholder documentation is not useful. Delete it, complete it, or
  stop linking to it.
- Generated code must be clearly identified and should not drive the style of
  manually designed APIs.

## Testing

Match test strategy to the repository's capabilities.

Baseline expectations:

- `go test ./...`
- `go vet ./...`
- formatting and tidy checks
- static analysis
- runnable examples compile

Use when appropriate:

- race tests for concurrent stateful code;
- table-driven unit tests;
- golden binary or hexadecimal fixtures under `testdata/`;
- fuzz tests for stable public parsers and decoders;
- loopback or in-process transport tests;
- interoperability tests behind build tags;
- negative and boundary tests;
- benchmarks for hot parsers, codecs, logging, metrics, or high-throughput paths.

Do not chase 100% coverage with low-value tests. Cover meaningful behaviour:
validation, malformed input, EOF, limits, ownership, concurrency, cancellation,
shutdown, and protocol errors.

Hardware-dependent tests should be isolated and documented.

## Makefiles and local checks

Use the repository's existing conventions. A typical library should provide a
clear local path for:

- formatting;
- tidy verification;
- vet;
- static analysis;
- unit tests;
- race tests where supported;
- coverage;
- vulnerability scanning;
- an aggregate check target.

Recommended practices:

- set `GOWORK=off` for standalone module verification when a sibling `go.work`
  could mask dependency problems;
- keep tool versions pinned or centrally controlled;
- do not keep stale fuzz targets or Makefile targets;
- exclude generated, command, or example packages from policies only when there
  is a clear reason;
- do not make every expensive or platform-specific check a per-commit hard gate.

CLI binaries should use the repository's established version-injection pattern.
Do not introduce a different ldflags scheme merely for uniformity.

## CI and releases

Prefer shared workflows from `otfabric/.github` when they fit the repository,
but verify the current workflow version and inputs before editing.

A normal CI matrix should test:

- the module's supported Go floor;
- one current supported Go release;
- additional versions only when they add useful compatibility evidence.

Do not hardcode speculative future Go versions.

Release workflows should remain consistent with the repository's established
process. Agents must not commit, push, tag, force-push, or create releases unless
the human explicitly requests that action.

## Licensing and repository hygiene

- Keep an MIT `LICENSE` at the repository root where that is the established
  project license.
- Preserve existing SPDX headers and build tags.
- Apply SPDX headers consistently to new first-party Go files when that is the
  repository convention.
- Never commit credentials or private operational data.
- Sanitize packet captures, binary fixtures, logs, certificates, and device
  identifiers before adding them to `testdata`.
- Keep badges, import paths, module paths, package names, and examples accurate.

## Dependency hygiene

- Prefer released OT Fabric sibling versions.
- Test downstream consumers before merging an exported lower-layer change.
- Run `go mod tidy` after dependency changes.
- Never commit a local sibling `replace` directive intended only for development.
- A temporary direct proxy fetch may be needed immediately after a new tag, but
  release configuration must remain normal and reproducible.
- Do not bump dependencies solely for visual consistency; record the reason and
  test the affected behaviour.

## Change discipline

- Keep diffs focused.
- Separate documentation/tooling cleanup from behavioural changes where
  practical.
- Preserve public behaviour unless the task explicitly calls for changing it.
- Update tests and relevant documentation whenever a contract changes.
- State uncertainty instead of guessing about protocol or repository behaviour.
- Work repository by repository for broad quality passes.
- Do not perform unrelated refactors while fixing a focused issue.
- Do not force-push or rewrite shared history without an explicit human request.

## Reference repositories

Use these as examples of strong existing patterns, not mandatory templates:

| Concern | Useful reference |
|---|---|
| Small framing or codec library | `go-tpkt` |
| Ownership, context, and error contracts | `go-cotp` |
| Rich operation result models | `go-s7comm` |
| Silent structured logging and redaction | `go-mms` |
| Configuration, retry, and metrics | `go-modbus` |
| Large protocol and interop documentation | `go-iec61850`, `go-opcua` |
| Cross-platform transport documentation | `go-serial` |
| Scanner and progress observers | `go-otfp` |
| Early-project honesty and incremental maturity | `go-modbus-identity` |

Copy the principle that fits the repository. Do not copy an API or abstraction
without validating that it fits the protocol and consumers.

## Anti-goals

Do not introduce:

- cross-repository governance frameworks;
- forced identical constructors or lifecycle APIs;
- logging or metrics at every layer;
- shared abstractions without demonstrated reuse;
- documentation files created only for appearance;
- silent behavioural changes;
- fake cancellation;
- automatic replay of unsafe operations without a documented policy;
- protocol features in layers that do not own them;
- major releases motivated only by family consistency.