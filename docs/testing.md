# Testing And Coverage

Run checks from the repository root unless a command explicitly changes directory.

## Required Release Checks

```bash
go test ./...
go test -race ./...
(cd clients/adminapi && go test -race ./...)
(cd integrations/gin && go test -race ./...)
(cd examples/vanilla-service && go test ./...)
(cd examples/gin-service && go test ./...)
(cd examples/admin-extension && go test ./...)
golangci-lint run ./...
.github/scripts/run-benchmarks.sh
```

`bash .github/scripts/ci.sh all` builds and tests every module in one go, the same way CI does.

## Coverage

The CI coverage script excludes `internal/testutil` because it is test support code, not production SDK behavior.

```bash
.github/scripts/compute-coverage.sh head
```

Production-package statement coverage of the core module, excluding
`internal/testutil`: 98.5% (2026-11-04).

## Example Modules

Each example in `examples/` is a separate Go module. Run its checks from the module root; the committed `go.work` points the SDK modules at the local sources.

CI builds, vets and tests every example in the `examples` job of `.github/workflows/ci.yml`.

## Benchmarks

Use the bounded benchmark script for release notes and README numbers:

```bash
.github/scripts/run-benchmarks.sh
```

For deeper local investigation, run a full benchmark suite manually:

```bash
go test -run '^$' -bench=. -benchmem ./...
```

The full suite can take noticeably longer because some storage benchmarks perform setup work outside the timed section. Use bounded runs for release readiness.
