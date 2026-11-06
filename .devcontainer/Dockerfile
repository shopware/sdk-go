# Tool/image versions — pinned for a reproducible dev environment; bump intentionally.
ARG GO_IMAGE_VERSION=1.26-bookworm
# Exact Go toolchain. The base image only tracks the 1.26 line and may lag behind
# the latest patch, so we install GO_VERSION explicitly over it to guarantee
# standard-library security fixes (e.g. GO-2026-6218 / GO-2026-6179, fixed in 1.26.6;
# GO-2026-6617, fixed in 1.26.9; GO-2026-6809, fixed in 1.26.10). Keep >= the recommended patch in README / go.mod compatibility floor.
ARG GO_VERSION=1.26.10
ARG GOLANGCI_LINT_VERSION=v2.13.2  # keep in sync with .github/workflows/ci.yml
ARG DELVE_VERSION=v1.27.2
ARG GOVULNCHECK_VERSION=v1.8.0
ARG GOTESTSUM_VERSION=v1.13.0
# Keep GITLEAKS_VERSION in sync with .github/workflows/ci.yml.
ARG GITLEAKS_VERSION=v8.30.1

FROM mcr.microsoft.com/devcontainers/go:${GO_IMAGE_VERSION}

# System packages: build-essential/gcc for the -race detector's instrumentation
# (the SQLite driver is now pure Go, modernc.org/sqlite, so the SDK needs no CGo);
# bc is required by .github/scripts/compute-coverage.sh.
RUN apt-get update \
 && export DEBIAN_FRONTEND=noninteractive \
 && apt-get install -y --no-install-recommends build-essential bc \
 && apt-get clean && rm -rf /var/lib/apt/lists/*

# Install the exact Go toolchain over the one bundled in the base image, so the
# environment is reproducible and includes the latest security patches regardless
# of when the base image was last rebuilt. Runs as root, before any `go install`.
ARG GO_VERSION
RUN ARCH="$(dpkg --print-architecture)" \
 && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz \
 && rm -rf /usr/local/go \
 && tar -C /usr/local -xzf /tmp/go.tar.gz \
 && rm /tmp/go.tar.gz \
 && go version

# golangci-lint. Note: `version: "2"` in .golangci.yml is the config *schema*
# version, which is independent of the golangci-lint binary version pinned here.
# Pin both the install script (fetched from the release tag, not HEAD) and the
# binary version for reproducible, supply-chain-safe builds. ARG is re-declared
# here to bring it into this build stage.
ARG GOLANGCI_LINT_VERSION
RUN curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/${GOLANGCI_LINT_VERSION}/install.sh" \
      | sh -s -- -b "$(go env GOPATH)/bin" "${GOLANGCI_LINT_VERSION}"

# Additional developer tools, installed into the vscode user's GOPATH.
USER vscode
ARG DELVE_VERSION
ARG GOVULNCHECK_VERSION
ARG GOTESTSUM_VERSION
ARG GITLEAKS_VERSION
RUN go install github.com/go-delve/delve/cmd/dlv@${DELVE_VERSION} \
 && go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} \
 && go install gotest.tools/gotestsum@${GOTESTSUM_VERSION} \
 && go install github.com/zricethezav/gitleaks/v8@${GITLEAKS_VERSION}
