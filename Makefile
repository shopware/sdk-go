# The build/test logic lives in .github/scripts/ci.sh so the Makefile (local
# dev) and the compatibility workflow (CI) run the exact same steps — and CI
# needs no `make` on Windows. CGo handling is per-step inside the script
# (build: CGO_ENABLED=0 to prove pure Go; test: CGO_ENABLED=1 for -race).
# Override behaviour via GO / TEST_FLAGS env vars, which the script reads.
CI := .github/scripts/ci.sh

.PHONY: build test vet ci-test

build:
	@bash $(CI) build

test:
	@bash $(CI) test

# ci-test is the entry point CI uses: compile, then run tests.
ci-test:
	@bash $(CI) all

# Not part of CI; kept as a local convenience.
vet:
	@for m in . integrations/gin clients/adminapi; do echo ">> vet $$m"; (cd "$$m" && go vet ./...) || exit 1; done
