#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-head}" # head or base
BASE_REF="${2:-}" # required if MODE=base
PKG_PREFIX="${PKG_PREFIX:-github.com/shopware/app-sdk-go}"
ALL_OUT="all_coverage-report.out"
OUT="coverage-${MODE}.out"

cleanup() {
    if [[ -n "${WT_DIR:-}" ]]; then
        if [[ -d "$WT_DIR" ]]; then
            if ! git worktree remove --force "$WT_DIR"; then
                echo "Warning: Failed to remove worktree at $WT_DIR" >&2
            fi
        fi
    fi
}
trap cleanup EXIT

if [[ "$MODE" == "base" ]]; then
  [[ -n "$BASE_REF" ]] || { echo "BASE_REF required for base mode"; exit 2; }
  git fetch --no-tags --prune --depth=1 origin "$BASE_REF"
  WT_DIR="$(mktemp -d)"
  git worktree add "$WT_DIR" "origin/$BASE_REF"
  cd "$WT_DIR"
fi

# Get all packages matching prefix (only pkg/, exclude examples)
pkgs="$(go list ./pkg/... | grep "^${PKG_PREFIX}" || true)"
if [[ -z "${pkgs}" ]]; then
    echo "ERROR: no packages match prefix ${PKG_PREFIX}" >&2
    exit 3
fi

# Run tests with coverage
go test -covermode=atomic -coverprofile="${ALL_OUT}" ${pkgs}

# Exclude internal/testutil from coverage (test utilities, not production code)
grep -v "/internal/testutil/" "${ALL_OUT}" > "${OUT}" || cp "${ALL_OUT}" "${OUT}"

# Calculate total coverage
total=$(go tool cover -func="${OUT}" | awk '/^total:/{print $3}')
num="${total%\%}"

if [[ "${MODE}" == "base" ]]; then
    echo "BASE_COVERAGE=${num}" >> "${GITHUB_ENV}"
    echo "📊 Coverage (Base): ${total}" >> "${GITHUB_STEP_SUMMARY}"
else
    echo "HEAD_COVERAGE=${num}" >> "${GITHUB_ENV}"
    echo "📊 Coverage (Head): ${total}" >> "${GITHUB_STEP_SUMMARY}"
fi

