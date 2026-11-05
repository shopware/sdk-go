# Roadmap

This page lists the main directions for the SDK. Plans can change; concrete work is
tracked in [GitHub issues](https://github.com/shopware/app-sdk-go/issues). To propose a
feature, open an issue first (see [CONTRIBUTING.md](CONTRIBUTING.md#roadmap-and-decisions)).

## Admin API Client

The client lives in its own module, [`clients/adminapi`](clients/adminapi/README.md)
([ADR 0003](docs/adr/0003-admin-api-client-module.md)). Planned next:

- DAL helpers: search criteria, filters, aggregations, repositories and sync.
- Extension manager: install, activate, update and upload extensions.
- Media upload and cache clearing.
- Browser login for CLI tools.

## Core SDK

- Optional observability hooks, without forcing a metrics or tracing backend.
- More typed Shopware context structs and helper builders for common app-system workflows.

## Integrations And Examples

- More framework integrations, driven by user demand. Each one is a separate module,
  so the core stays free of framework dependencies.
- More examples for common app scenarios.

## Out Of Scope

- Full feature parity with the PHP SDK.
- Required OpenTelemetry, logging, retry or cache dependencies in the core SDK. These stay
  optional.
