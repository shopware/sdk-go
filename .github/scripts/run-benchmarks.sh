#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

BENCHTIME="${BENCHTIME:-100ms}"
PKG_PREFIX="${PKG_PREFIX:-github.com/shopware/app-sdk-go}"

coverage_all="$(mktemp)"
coverage_prod="$(mktemp)"
cleanup() {
    rm -f "${coverage_all}" "${coverage_prod}"
}
trap cleanup EXIT

echo "== Environment =="
go version
uname -a || true
echo "git: $(git rev-parse --short HEAD)"
echo "benchtime: ${BENCHTIME}"
echo

echo "== Bounded SDK benchmarks =="
go test \
    -run '^$' \
    -bench 'Benchmark(VerifyRegistration|VerifyWebhook|ComputeSignature_LargePayload|HandleRegistration|HandleConfirmation|InMemoryStore_(GetShopByID|UpdateShop|SetShopActive))$' \
    -benchmem \
    -benchtime="${BENCHTIME}" \
    ./pkg/signature ./pkg/registration ./pkg/store
echo

# The Gin integration is a separate module, so its benchmarks run from its root.
echo "== Bounded Gin integration benchmarks =="
(cd integrations/gin && go test \
    -run '^$' \
    -bench 'Benchmark(RegistrationEndpoint|WebhookVerification)$' \
    -benchmem \
    -benchtime="${BENCHTIME}" \
    .)
echo

echo "== Production coverage =="
pkgs="$(go list ./pkg/... | grep "^${PKG_PREFIX}" || true)"
if [[ -z "${pkgs}" ]]; then
    echo "ERROR: no packages match prefix ${PKG_PREFIX}" >&2
    exit 3
fi

go test -covermode=atomic -coverprofile="${coverage_all}" ${pkgs}
grep -v "/internal/testutil/" "${coverage_all}" > "${coverage_prod}" || cp "${coverage_all}" "${coverage_prod}"
go tool cover -func="${coverage_prod}" | tail -n 1
