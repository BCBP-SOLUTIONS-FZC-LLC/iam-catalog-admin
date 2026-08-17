# -----------------------------
# CONFIG
# -----------------------------
-include .env

APP_NAME      ?= catalog-admin-config
APP_ENV       ?= dev
GO            ?= go
BUILD_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BINARY        := bin/$(APP_NAME)

# Private modules — resolved via SSH (git@github.com:) using the global URL
# rewrite. No tokens needed for local dev; an SSH key registered with the
# BCBP-SOLUTIONS-FZC-LLC org is required.
export GOPRIVATE ?= github.com/BCBP-SOLUTIONS-FZC-LLC/*
export GONOSUMDB ?= github.com/BCBP-SOLUTIONS-FZC-LLC/*

export APP_NAME APP_ENV BUILD_VERSION

# Test package groups — mirrors iam-org-membership's tiered layout
# (test/unit, test/postgres, test/e2e), scaled down: this service has no
# events/outbox, so there is no test/integration (SNS/SQS) tier at all.
TEST_UNIT_PKGS := ./test/unit/...
TEST_PG_PKGS   := ./test/postgres/...
TEST_E2E_PKGS  := ./test/e2e/...

# White-box (package-internal) tests run alongside test/unit — these
# exercise package-private helpers (isDBUnavailableSQLState, requireOperator,
# withPool, envOr, ...) that a black-box test/unit package can't reach.
TEST_INTERNAL_PKGS := ./internal/adapter/inbound/http/... \
                      ./internal/adapter/outbound/postgres/... \
                      ./internal/adapter/outbound/valkey/... \
                      ./internal/adapter/outbound/metrics/... \
                      ./internal/core/domain/... \
                      ./internal/core/service/... \
                      ./pkg/...

COVER_PKG_LIST := $(shell $(GO) list ./internal/... ./pkg/... 2>/dev/null | tr '\n' ',' | sed 's/,$$//')

.PHONY: help
help:
	@echo "Available commands:"
	@echo "  make setup           - copy .env-example to .env if missing; install .githooks/pre-commit"
	@echo "  make tidy            - go mod tidy"
	@echo "  make fmt             - gofmt -w ."
	@echo "  make fmt-check       - verify gofmt formatting (mirrors CI)"
	@echo "  make vet             - go vet all packages"
	@echo "  make lint            - alias for vet (no golangci-lint config yet)"
	@echo "  make mod-verify      - go mod verify"
	@echo "  make vuln-check      - govulncheck on internal + pkg"
	@echo "  make test            - unit tests only (no Docker required)"
	@echo "  make test-unit       - unit tests only (no Docker required)"
	@echo "  make test-postgres   - Postgres-backed repository/migration tests (requires Docker)"
	@echo "  make test-e2e        - full-stack HTTP tests against real Postgres+Valkey (requires Docker)"
	@echo "  make test-ci         - unit + postgres + e2e with merged coverage (used in CI)"
	@echo "  make race            - unit tests with -race"
	@echo "  make run             - run the server locally (go run)"
	@echo "  make build           - compile the binary to bin/"
	@echo "  make cover           - merged coverage HTML report across unit+postgres+e2e (requires Docker)"
	@echo "  make cover-func      - merged coverage summary by function across unit+postgres+e2e (requires Docker)"
	@echo "  make ci              - tidy + fmt-check + vet + lint + test-ci + build"
	@echo "  make swag            - regenerate docs/swagger/ from handler annotations (mirrors sibling iam-user-profile2)"
	@echo "  make swag-check      - fail if Swagger regeneration would change docs/swagger/ (CI drift gate)"
	@echo "  make docker-up       - start local Postgres + Valkey"
	@echo "  make docker-down     - stop local containers"
	@echo "  make install-hooks   - install local git hooks (.githooks -> .git/hooks); also run by make setup"
	@echo "  make clean           - remove build artefacts"

# -----------------------------
# SETUP
# -----------------------------

.PHONY: setup
setup: install-hooks
	@test -f .env || cp .env-example .env
	@echo "Environment ready (.env)"

# -----------------------------
# GO BASICS
# -----------------------------

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: fmt
fmt:
	gofmt -l -w .

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l . 2>/dev/null); \
	if [ -n "$$unformatted" ]; then \
		echo "FAIL: unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "gofmt: all files formatted"

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: lint
lint: vet

.PHONY: mod-verify
mod-verify:
	$(GO) mod verify

.PHONY: vuln-check
vuln-check:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./internal/... ./pkg/...

# -----------------------------
# TESTS
# -----------------------------

.PHONY: test
test: test-unit

.PHONY: test-unit
test-unit:
	$(GO) test $(TEST_UNIT_PKGS) $(TEST_INTERNAL_PKGS) -count=1 -timeout 60s

.PHONY: test-postgres
test-postgres:
	$(GO) test $(TEST_PG_PKGS) -tags=integration -count=1 -timeout 180s -v

.PHONY: test-e2e
test-e2e:
	$(GO) test $(TEST_E2E_PKGS) -tags=e2e -count=1 -timeout 300s -v

# Legacy alias — "integration" here means "Postgres-backed", since this
# service has no events/outbox and therefore no separate cross-service
# integration tier the way iam-org-membership does.
.PHONY: test-integration
test-integration: test-postgres

.PHONY: race
race:
	$(GO) test $(TEST_UNIT_PKGS) $(TEST_INTERNAL_PKGS) -race -count=1 -timeout 120s

# _test-* variants instrument the *same* coverpkg set (everything under
# internal/ and pkg/, i.e. excluding cmd/) regardless of which test tier is
# running — a Postgres-only file like department_repository.go only shows
# real coverage once test/postgres's profile is merged with test/unit's.
# Each writes its own .coverage/<tier>.out; _merge-coverage combines them.
.PHONY: _test-unit
_test-unit: | .coverage
	$(GO) test $(TEST_UNIT_PKGS) $(TEST_INTERNAL_PKGS) -race -count=1 -timeout 120s \
	  -coverpkg=$(COVER_PKG_LIST) -coverprofile=.coverage/unit.out

.PHONY: _test-postgres
_test-postgres: | .coverage
	$(GO) test $(TEST_PG_PKGS) -tags=integration -race -count=1 -timeout 180s \
	  -coverpkg=$(COVER_PKG_LIST) -coverprofile=.coverage/postgres.out

.PHONY: _test-e2e
_test-e2e: | .coverage
	$(GO) test $(TEST_E2E_PKGS) -tags=e2e -race -count=1 -timeout 300s \
	  -coverpkg=$(COVER_PKG_LIST) -coverprofile=.coverage/e2e.out

# Merge the three per-suite profiles into a single coverage.out (max-count
# strategy — any suite covering a block wins). Mirrors iam-org-membership's
# scripts/merge_coverage.py convention.
.PHONY: _merge-coverage
_merge-coverage:
	@python3 scripts/merge_coverage.py \
	  .coverage/unit.out .coverage/postgres.out .coverage/e2e.out \
	  > coverage.out
	@echo "==> coverage.out merged from all suites (max-count strategy)"

.PHONY: test-ci
test-ci: _test-unit _test-postgres _test-e2e
	$(MAKE) _merge-coverage

.coverage:
	@mkdir -p .coverage

# -----------------------------
# RUN
# -----------------------------

.PHONY: run
run:
	@-lsof -ti :$${APP_PORT:-8081} | xargs kill -9 2>/dev/null; true
	bash -c 'set -a && source .env 2>/dev/null; set +a && BUILD_VERSION=$(BUILD_VERSION) $(GO) run ./cmd/catalog-admin-config'

# -----------------------------
# BUILD
# -----------------------------

.PHONY: build
build:
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w -X main.buildVersion=$(BUILD_VERSION)" -o $(BINARY) ./cmd/catalog-admin-config

# -----------------------------
# DOCKER
# -----------------------------

.PHONY: docker-up
docker-up:
	docker compose up -d

.PHONY: docker-down
docker-down:
	docker compose down

# -----------------------------
# GIT HOOKS
# -----------------------------

.PHONY: install-hooks
install-hooks:
	@mkdir -p .git/hooks
	@cp .githooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit

# -----------------------------
# SWAGGER
# -----------------------------

# swag: generate the OpenAPI/Swagger 2.0 spec from // @… annotations on handlers
# under cmd/catalog-admin-config and internal/adapter/inbound/http. Mirrors
# iam-user-profile2's pattern so developers moving between services see the
# same authoring workflow. The output under docs/swagger/ is checked into the
# repo; CI's swag-check target fails a PR whose annotations drift from what's
# on disk.
.PHONY: swag
swag:
	@echo "Generating Swagger docs..."
	$(GO) tool swag init \
	  -g swagger_info.go \
	  -d cmd/catalog-admin-config,internal/adapter/inbound/http \
	  --output docs/swagger \
	  --parseDependency \
	  --parseInternal
	@python3 scripts/patch-swagger-extensions.py
	@echo "Swagger docs written to docs/swagger/"

.PHONY: swag-check
swag-check:
	bash .github/scripts/check-swagger-stale.sh

# -----------------------------
# CI
# -----------------------------

.PHONY: ci
ci: tidy fmt-check vet lint test-ci build

# -----------------------------
# COVERAGE
# -----------------------------

.PHONY: cover
cover: test-ci
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: cover-func
cover-func: test-ci
	$(GO) tool cover -func=coverage.out

# -----------------------------
# CLEAN
# -----------------------------

.PHONY: clean
clean:
	rm -rf bin .coverage
	rm -f coverage.out coverage.html
