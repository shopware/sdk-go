# ADR 0002: No Logging in Core; Observability via Typed Errors

- **Status:** Proposed
- **Date:** 2026-06-16
- **Deciders:** app-sdk-go maintainers
- **Tags:** logging, errors, architecture, dependencies
- **Relates to:** [ADR 0001](0001-multi-module-repository-structure.md)

---

## Context

A library should not decide how the application logs. It should return errors with
enough context, and let the caller choose whether and how to log. This keeps the
library quiet, testable, and free of a logging dependency.

Reading the current code confirms the core SDK already follows this rule, almost fully:

- The core packages (`pkg/app`, `pkg/client`, `pkg/store`, `pkg/webhook`,
  `pkg/registration`, `pkg/signature`, `pkg/resolver`) do **not** call the logger at
  all. A search for `logger.` in core business logic finds nothing.
- The only real logger user is the **gin integration** (`pkg/integration/gin/gin.go`).
  It does the right thing: it inspects the typed error and decides what to log.
- The core returns rich, typed errors through `pkg/errors`. `*errors.Error` carries
  `Type`, `Message`, `Code`, and a wrapped `Err`, and supports `errors.Is` /
  `errors.As`. That is enough context for a caller to log well.

Two leftovers contradict this and should be cleaned up:

- A custom `Logger` interface, `Field` helpers, and adapters live in the core module
  under `pkg/logger`. The zap adapter (`pkg/logger/zap.go`) pulls `go.uber.org/zap`
  into the **root** `go.mod` as a direct dependency, even though the core never logs.
  Since Go 1.21, `log/slog` is the stdlib logging standard and `slog.Handler` is the
  shared extension point the ecosystem bridges to — a custom interface and a zap
  dependency are no longer warranted.
- `pkg/app/doc.go` showed a `Logger:` field in `app.Config` and listed "Logger: Logs
  SDK operations" as a component. `app.Config` has no such field — the comment was
  stale. (Fixed alongside this ADR.)

---

## Decision

1. **The core SDK does not log.** Core packages report problems only by returning
   typed errors from `pkg/errors`. No logger is passed into core constructors.
2. **Observability in the core is carried by errors.** `*errors.Error` (`Type`,
   `Message`, `Code`, wrapped `Err`, `Is`/`Unwrap`) is the contract the caller uses to
   log, count metrics, or map to an HTTP response.
3. **Logging belongs to the integration layer.** The framework integration (gin today;
   fiber/echo later) owns the request/response cycle and decides what to log.
4. **Integrations log through `log/slog`.** An integration accepts a `*slog.Logger`
   directly (a `nil` logger falls back to a no-op handler). It does not define a custom
   logging interface — `slog.Handler` is already the standard extension point, so
   consumers can plug zap/zerolog/etc. through their slog bridges without the SDK
   depending on any of them.
5. **`pkg/logger` and zap are removed.** The custom `Logger` interface, the `Field`
   helpers, and the zap adapter are deleted, and `go.uber.org/zap` is dropped from the
   repository entirely. `log/slog` is stdlib, so this adds no external dependency.

---

## Consequences

### Benefits

- The core has no logging dependency; `go.uber.org/zap` leaves the root `go.mod`.
- The core stays quiet and easy to test — no log output to assert or silence.
- The caller controls log format, level, sampling, and destination.
- Clear separation: core = pure logic + typed errors; integration = transport + logging.

### Costs and trade-offs

- For deep debugging of the core, the caller relies on errors (and stack context in
  wrapped errors) instead of internal log lines.
- Error messages must stay descriptive enough to be useful when logged by the caller.
- Integrations are responsible for logging consistently across frameworks.

---

## How callers log (pattern)

The integration holds a `*slog.Logger`, inspects the typed error, and logs once, at
the boundary:

```go
var sdkErr *errors.Error
if errors.As(err, &sdkErr) {
    log.Warn("shopware sdk error",
        slog.String("type", sdkErr.Type),
        slog.Int("code", sdkErr.Code),
        slog.String("message", sdkErr.Message),
    )
    // map sdkErr.Code to the HTTP response
}
```

This replaces the current zap-based logging in `pkg/integration/gin/gin.go`
(`handleSDKError`), keeping the same one-log-at-the-boundary behaviour.

---

## Guidelines for contributors

- Core code returns errors; it never logs. Do not add a `Logger` to core constructors.
- Wrap underlying errors with `errors.Wrap` / the `New*` constructors so the `Type`,
  `Code`, and a clear `Message` reach the caller.
- If a core operation needs more context for debugging, add it to the error, not to a
  log line.
- Advanced hooks (callbacks, metrics) may be considered later, but errors are the
  default and sufficient observability mechanism.

---

## Alternatives Considered

1. **Keep a custom `Logger` interface (backed by slog) in the integration.** Rejected:
   `slog.Handler` is already the standard extension point, so a custom 4-method
   interface plus `Field` helpers only duplicates what stdlib provides. Accepting
   `*slog.Logger` directly is simpler and just as pluggable.
2. **Keep the zap adapter as the integration default.** Rejected: it forces
   `go.uber.org/zap` on every integration consumer. Consumers who want zap can attach
   it via a `slog.Handler` bridge instead.
3. **Add a logger to the core anyway (any flavour).** Rejected: the core has nothing to
   log and this re-introduces the logging dependency/policy coupling this ADR removes.

---

## Acceptance Criteria

- No core package imports or calls a logger.
- The core surfaces all failures as typed `*errors.Error` values.
- The custom `Logger` interface, `Field` helpers, and zap adapter are deleted; `pkg/logger`
  no longer exists.
- `go.uber.org/zap` is not a dependency of any module in the repository.
- Integrations accept a `*slog.Logger` (with a no-op fallback when `nil`).
- Documentation and examples show logging done by the caller/integration via `slog`,
  not the core.
