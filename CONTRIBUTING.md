# Contributing

Thanks for helping improve the Shopware SDK for Go. Keep contributions focused, tested, and aligned with the SDK's minimal, optional-by-default design.

## Development Setup

```bash
go mod download
go test ./...
```

Use Go 1.26 or newer. The SDK supports the two most recent major Go releases
(currently Go 1.26 and 1.27); see the Go version support policy in the README.
Use the latest patch release of your line, otherwise the `govulncheck` job will
fail your pull request on standard-library advisories that are already fixed
upstream. The exact patch CI builds with is pinned as `GO_VERSION` in
[.github/workflows/ci.yml](.github/workflows/ci.yml) and mirrored by
`ARG GO_VERSION` in [.devcontainer/Dockerfile](.devcontainer/Dockerfile) — keep
the two in sync when bumping, and update the recommended patch in the README
and SECURITY.md.

## Expected Checks

Before opening a pull request, run the checks that match your change:

```bash
go test ./...
go test -race ./...
(cd clients/adminapi && go test -race ./...)
go test ./examples/vanilla-service
(cd examples/gin-service && go test ./...)
(cd examples/admin-extension && go test ./...)
golangci-lint run ./...
make secrets
```

For benchmark or performance-sensitive changes:

```bash
.github/scripts/run-benchmarks.sh
```

## Secrets In Tests And Examples

CI scans every new commit for secrets with [gitleaks](https://github.com/gitleaks/gitleaks)
(job `Secret scan`); `make secrets` runs the same check locally. Fake secrets in tests,
examples and docs must match one of the allowlisted patterns in
[.gitleaks.toml](.gitleaks.toml), for example `test-secret-1234567890`. The allowlist
matches values, not files, so a real secret is reported wherever it appears.

If a real secret was committed, revoke it first: removing it from the branch does not
help once it has been pushed to a public repository.

## Design Principles

- Keep the core SDK small and framework-neutral where practical.
- Make cross-cutting concerns optional, including logging, retry logic, caching, and observability.
- Prefer interfaces at package boundaries when they reduce coupling.
- Avoid panics in library code; return structured errors instead.
- Preserve context propagation and request cancellation.
- Update examples and docs when public behavior changes.

## Pull Request Guidelines

- Use a semantic PR title such as `fix: ...`, `feat: ...`, `docs: ...`, or `test: ...`.
- Include tests for behavior changes.
- Keep unrelated refactors out of feature or bugfix PRs.
- Explain any public API change in the PR description.
- For user-visible changes, update README or `/docs`.
- Add a changelog fragment for `feat`/`fix`/`docs`/`perf` PRs: `make changelog-new`.
  See [docs/CHANGELOG_AND_RELEASES.md](docs/CHANGELOG_AND_RELEASES.md). CI enforces
  this (bypass with the `skip-changelog` label).

## Roadmap And Decisions

Roadmap decisions are made in public GitHub issues. Small bugfixes can go straight to PR. Larger features should start with an issue describing the user problem, proposed API shape, alternatives considered, and compatibility impact.
