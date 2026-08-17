package main

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
)

func mustNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic, got none")
		}
	}()
	fn()
}

func TestValidateRequiredEnv_DevRelaxed(t *testing.T) {
	t.Setenv("VALKEY_URL", "localhost:6379")
	t.Setenv("DATABASE_URL", "postgres://localhost/catalog_admin?sslmode=disable")
	mustNotPanic(t, func() { validateRequiredEnv("dev") })
}

func TestValidateRequiredEnv_MissingValkeyURL(t *testing.T) {
	t.Setenv("VALKEY_URL", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/catalog_admin")
	mustPanic(t, func() { validateRequiredEnv("dev") })
}

func TestValidateRequiredEnv_ProdRequiresRedissURL(t *testing.T) {
	t.Setenv("VALKEY_URL", "redis://localhost:6379")
	t.Setenv("DATABASE_URL", "postgres://localhost/catalog_admin")
	mustPanic(t, func() { validateRequiredEnv("production") })
}

func TestValidateRequiredEnv_ProdAcceptsRedissURL(t *testing.T) {
	t.Setenv("VALKEY_URL", "rediss://localhost:6379")
	t.Setenv("DATABASE_URL", "postgres://localhost/catalog_admin?sslmode=require")
	mustNotPanic(t, func() { validateRequiredEnv("production") })
}

func TestValidateRequiredEnv_BouncerModeRequiresMigrationDSN(t *testing.T) {
	t.Setenv("VALKEY_URL", "localhost:6379")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PG_HOST", "localhost")
	t.Setenv("PG_USER", "app")
	t.Setenv("PG_PASSWORD", "pw")
	t.Setenv("PG_BOUNCER_MODE", "true")
	t.Setenv("MIGRATION_DATABASE_URL", "")
	mustPanic(t, func() { validateRequiredEnv("dev") })
}

// ── validatePostgresConfig ───────────────────────────────────────────────
// Postgres-specific validation now lives here, run against
// pgcommon.ConfigFromEnv's resolved DSN/warnings, not validateRequiredEnv
// directly — see main.go's comment on why.

func TestValidatePostgresConfig_EmptyDSNPanics(t *testing.T) {
	mustPanic(t, func() { validatePostgresConfig("dev", "", nil) })
}

func TestValidatePostgresConfig_DevIgnoresInsecureSSLModeWarning(t *testing.T) {
	warnings := []pgcommon.ConfigWarning{{Key: "PG_SSLMODE", Reason: "sslmode=disable can negotiate plaintext connections"}}
	mustNotPanic(t, func() { validatePostgresConfig("dev", "postgres://x", warnings) })
}

// TestValidatePostgresConfig_ProdRejectsInsecureSSLModeWarning is a
// regression test for the PG_SSLMODE enforcement added alongside the
// VALKEY_URL rediss:// check — previously only Valkey's TLS requirement
// was code-enforced in prod/staging, leaving Postgres TLS as a
// Helm-default convention only. pgcommon.ConfigFromEnv itself only warns
// on an insecure sslmode (disable/allow/prefer); this service escalates
// that warning to a hard failure in production/staging.
func TestValidatePostgresConfig_ProdRejectsInsecureSSLModeWarning(t *testing.T) {
	warnings := []pgcommon.ConfigWarning{{Key: "PG_SSLMODE", Reason: "sslmode=disable can negotiate plaintext connections"}}
	mustPanic(t, func() { validatePostgresConfig("production", "postgres://x", warnings) })
}

func TestValidatePostgresConfig_ProdRejectsInsecureDatabaseURLWarning(t *testing.T) {
	warnings := []pgcommon.ConfigWarning{{Key: "DATABASE_URL", Reason: "sslmode=disable can negotiate plaintext connections"}}
	mustPanic(t, func() { validatePostgresConfig("staging", "postgres://x?sslmode=disable", warnings) })
}

func TestValidatePostgresConfig_ProdAcceptsNoWarnings(t *testing.T) {
	mustNotPanic(t, func() { validatePostgresConfig("production", "postgres://x?sslmode=require", nil) })
}

func TestValidatePostgresConfig_ProdIgnoresUnrelatedWarnings(t *testing.T) {
	warnings := []pgcommon.ConfigWarning{{Key: "PG_MAX_CONNS", Reason: "invalid integer \"abc\", using default 10"}}
	mustNotPanic(t, func() { validatePostgresConfig("production", "postgres://x", warnings) })
}
