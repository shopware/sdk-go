# Benchmarks

Benchmarks cover the hot paths of the SDK: signed request handling, webhook verification, the registration flow and store operations. Use them to spot regressions between versions; they are not a comparison with other SDKs.

## Current Baseline

Measured on 2026-11-04 with `.github/scripts/run-benchmarks.sh` (`benchtime=100ms`):

- Hardware: Apple Silicon, 14 cores, Linux VM (OrbStack)
- OS/arch: linux/arm64
- Go: 1.26.10

| Area | Result |
| --- | ---: |
| Webhook HMAC verification | 712 ns/op, 656 B/op, 9 allocs/op |
| Registration HMAC verification | 705 ns/op, 720 B/op, 10 allocs/op |
| 10 KB HMAC signature computation | 4.1 µs/op, 656 B/op, 9 allocs/op |
| Registration handler | 5.0 µs/op, 3.7 KB/op, 61 allocs/op |
| Confirmation handler | 2.4 µs/op, 2.2 KB/op, 24 allocs/op |
| Gin registration endpoint | 10.2 µs/op, 10.5 KB/op, 79 allocs/op |
| Gin webhook verification endpoint | 10.2 µs/op, 9.3 KB/op, 60 allocs/op |
| In-memory shop lookup / update | 206–237 ns/op, 208 B/op, 1 alloc/op |
| In-memory shop activation | 38 ns/op, 0 allocs/op |
| Production-package statement coverage (core) | 98.5% |

Absolute numbers depend on the hardware. Compare runs made on the same machine, for
example before and after a change.

## Bounded Benchmark Command

Use the release script for repeatable output:

```bash
.github/scripts/run-benchmarks.sh
```

The script prints the Go version, host details, git revision, selected benchmark groups, and production coverage. It uses a short benchtime to prevent release checks from getting stuck in long-running storage benchmarks.
