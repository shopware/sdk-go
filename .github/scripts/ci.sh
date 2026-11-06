#!/usr/bin/env bash
# Single source of truth for the compatibility build/test steps. Invoked by both
# the Makefile (local dev) and .github/workflows/compatibility.yml (CI). Running
# it directly under bash means Windows CI needs no `make` (git-bash provides bash
# on the runner), avoiding the flaky `choco install make` step.
set -euo pipefail

cmd="${1:-all}"
GO="${GO:-go}"
TEST_FLAGS="${TEST_FLAGS:--race -timeout=10m}"

# Modules that contain tests. Examples are checked by the separate "examples"
# job in ci.yml, because they are not released.
MODULES=". integrations/gin clients/adminapi"

# Retry a command a few times — proxy.golang.org occasionally returns transient
# HTTP/2 stream errors (INTERNAL_ERROR) mid-download. Conditions in `until` are
# exempt from `set -e`, so a failed attempt loops instead of aborting.
retry() {
	local n=0 max="${RETRIES:-3}" delay="${RETRY_DELAY:-5}"
	until "$@"; do
		n=$((n + 1))
		if [ "$n" -ge "$max" ]; then
			echo "!! giving up after $max attempts: $*" >&2
			return 1
		fi
		echo "   retry $n/$max in ${delay}s: $*" >&2
		sleep "$delay"
	done
}

# Warm the module cache up front (with retries) so the subsequent build/test
# steps run against cached modules and don't fail on a flaky single download.
deps() {
	for m in $MODULES; do
		echo ">> deps $m"
		(cd "$m" && retry "$GO" mod download)
	done
}

# CGO_ENABLED=0 proves the SDK builds with zero CGo — the SQLite driver is the
# pure-Go modernc.org/sqlite, so a CGo dependency sneaking back in fails here.
build() {
	for m in $MODULES; do
		echo ">> build $m"
		(cd "$m" && CGO_ENABLED=0 "$GO" build ./...)
	done
}

# Tests keep -race, whose instrumentation needs cgo (a C compiler, present on all
# CI runners) — hence CGO_ENABLED=1 here even though the product is CGo-free.
run_tests() {
	for m in $MODULES; do
		echo ">> test $m"
		# shellcheck disable=SC2086 # word-splitting of TEST_FLAGS is intended
		(cd "$m" && CGO_ENABLED=1 "$GO" test $TEST_FLAGS ./...)
	done
}

case "$cmd" in
	build) deps; build ;;
	test) deps; run_tests ;;
	all) deps; build; run_tests ;;
	*) echo "usage: ci.sh [build|test|all]" >&2; exit 2 ;;
esac
