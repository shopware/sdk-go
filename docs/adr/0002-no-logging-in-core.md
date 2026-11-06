# ADR 0002: No Logging in Core; Observability via Typed Errors

- **Status:** Accepted
- **Date:** 2026-06-16
- **Deciders:** sdk-go maintainers
- **Tags:** logging, errors, architecture, dependencies
- **Relates to:** [ADR 0001](0001-multi-module-repository-structure.md)

---

## Context

A library should not decide how the application logs. It should return errors with
enough context, and let the caller choose whether and how to log. This keeps the
library quiet, testable, and free of a logging dependency.

The core packages (`app`, `store`, `webhook`, `registration`, `signature`,
`resolver`) contain business logic only. The HTTP request/response cycle — the place
where it is clear what happened and how important it is — belongs to the framework
integration (for example `integrations/gin`).

Since Go 1.21, `log/slog` is the standard library logging API, and `slog.Handler` is
the shared extension point the ecosystem bridges to (zap, zerolog, and others). A
custom logging interface in the SDK is not needed.

---

## Decision

1. **The core SDK does not log.** Core packages report problems only by returning
   typed errors from `sdkerrors`. No logger is passed into core constructors.
2. **Observability in the core is carried by errors.** `*sdkerrors.Error` (`Type`,
   `Message`, `Code`, wrapped `Err`, `Is`/`Unwrap`) is the contract the caller uses to
   log, count metrics, or map to an HTTP response.
3. **Logging belongs to the integration layer.** The framework integration (Gin today;
   Fiber/Echo later) owns the request/response cycle and decides what to log.
4. **Integrations log through `log/slog`.** An integration accepts a `*slog.Logger`
   directly (a `nil` logger falls back to a no-op handler). It does not define a custom
   logging interface — consumers plug zap/zerolog/etc. through their slog bridges
   without the SDK depending on any of them.

---

## Consequences

### Benefits

- The core has no logging dependency.
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
var sdkErr *sdkerrors.Error
if errors.As(err, &sdkErr) {
    log.Warn("shopware sdk error",
        slog.String("type", sdkErr.Type),
        slog.Int("code", sdkErr.Code),
        slog.String("message", sdkErr.Message),
    )
    // map sdkErr.Code to the HTTP response
}
```

The Gin integration does exactly this in `handleSDKError` (`integrations/gin/gin.go`).

---

## Guidelines for contributors

- Core code returns errors; it never logs. Do not add a `Logger` to core constructors.
- Wrap underlying errors with `sdkerrors.Wrap` / the `New*` constructors so the `Type`,
  `Code`, and a clear `Message` reach the caller.
- If a core operation needs more context for debugging, add it to the error, not to a
  log line.
- Advanced hooks (callbacks, metrics) may be considered later, but errors are the
  default and sufficient observability mechanism.

---

## Alternatives Considered

1. **A custom `Logger` interface (backed by slog) in the integration.** Rejected:
   `slog.Handler` is already the standard extension point, so a custom interface plus
   `Field` helpers only duplicates what the standard library provides. Accepting
   `*slog.Logger` directly is simpler and just as pluggable.
2. **A zap adapter as the integration default.** Rejected: it forces
   `go.uber.org/zap` on every integration consumer. Consumers who want zap can attach
   it via a `slog.Handler` bridge instead.
3. **A logger in the core (any flavour).** Rejected: the core has nothing to log, and
   this would add a logging dependency and policy coupling.

---

## Acceptance Criteria

- No core package imports or calls a logger.
- The core surfaces all failures as typed `*sdkerrors.Error` values.
- No module in the repository depends on a third-party logging library.
- Integrations accept a `*slog.Logger` (with a no-op fallback when `nil`).
- Documentation and examples show logging done by the caller/integration via `slog`,
  not the core.
