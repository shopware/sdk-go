# The build/test logic lives in .github/scripts/ci.sh so the Makefile (local
# dev) and the compatibility workflow (CI) run the exact same steps — and CI
# needs no `make` on Windows. CGo handling is per-step inside the script
# (build: CGO_ENABLED=0 to prove pure Go; test: CGO_ENABLED=1 for -race).
# Override behaviour via GO / TEST_FLAGS env vars, which the script reads.
CI := .github/scripts/ci.sh

.PHONY: build test vet ci-test secrets \
	changelog changelog-new changelog-new-major changelog-validate changelog-plan changelog-gen

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

# Scan commits and uncommitted changes for secrets, with the same config as the
# "Secret scan" CI job. Needs gitleaks (installed in the dev container).
secrets:
	@gitleaks git --config .gitleaks.toml --redact --no-banner
	@gitleaks git --pre-commit --config .gitleaks.toml --redact --no-banner

# --- Changelog / release tooling (see docs/CHANGELOG_AND_RELEASES.md) --------
# The generator resolves .changelog/ from the git root, so these work from any
# directory. `changelog` is a generic passthrough: make changelog ARGS="plan".
CHANGELOG := go run ./changelog

changelog:
	@cd tools && $(CHANGELOG) $(ARGS)

# Create a new fragment (interactive: prompts for module, type, description).
# Assumes a non-breaking change (version bump derived from the type).
changelog-new:
	@cd tools && $(CHANGELOG) new

# Same as changelog-new, but marks the change as breaking (forces a major bump).
# Major releases are not automated — see docs/CHANGELOG_AND_RELEASES.md.
changelog-new-major:
	@cd tools && $(CHANGELOG) new --major

# Validate every fragment under .changelog/.
changelog-validate:
	@cd tools && $(CHANGELOG) validate

# Preview which modules would be released and at what version.
changelog-plan:
	@cd tools && $(CHANGELOG) plan $(ARGS)

# Print the generated release-notes body to stdout.
changelog-gen:
	@cd tools && $(CHANGELOG) gen $(ARGS)
