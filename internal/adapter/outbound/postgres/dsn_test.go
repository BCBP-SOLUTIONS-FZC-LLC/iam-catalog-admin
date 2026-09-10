package postgres

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestApplyStatementTimeout_NoopWhenUnset(t *testing.T) {
	t.Setenv("PG_STATEMENT_TIMEOUT", "")
	assert.Equal(t, "postgres://x", ApplyStatementTimeout("postgres://x"))
}

func TestApplyStatementTimeout_EmptyDSNUnchanged(t *testing.T) {
	t.Setenv("PG_STATEMENT_TIMEOUT", "5s")
	assert.Equal(t, "", ApplyStatementTimeout(""))
}

func TestApplyStatementTimeout_AppendsWhenSet(t *testing.T) {
	withEnv(t, map[string]string{"PG_STATEMENT_TIMEOUT": "5s"})
	dsn := ApplyStatementTimeout("postgres://x")
	assert.Contains(t, dsn, "postgres://x")
	assert.Contains(t, dsn, "statement_timeout")
	assert.Contains(t, dsn, "5000") // 5s in milliseconds
}

func TestApplyStatementTimeout_NoExistingQueryString_UsesQuestionMark(t *testing.T) {
	// Regression: MigrationDSNFromEnv applies this directly to a raw
	// MIGRATION_DATABASE_URL that may have no "?" of its own (unlike the app
	// DSN, which always comes from pgcommon.ConfigFromEnv with "?sslmode=..."
	// already present) — appending "&options=..." unconditionally used to
	// produce a malformed DSN with no leading "?".
	withEnv(t, map[string]string{"PG_STATEMENT_TIMEOUT": "5s"})
	dsn := ApplyStatementTimeout("postgres://m:x@migrations.example:5432/catalog_admin")
	assert.Equal(t, "postgres://m:x@migrations.example:5432/catalog_admin?options=-c%20statement_timeout%3D5000", dsn)
}

func TestApplyStatementTimeout_ExistingQueryString_UsesAmpersand(t *testing.T) {
	withEnv(t, map[string]string{"PG_STATEMENT_TIMEOUT": "5s"})
	dsn := ApplyStatementTimeout("postgres://u@h/db?sslmode=disable")
	assert.Equal(t, "postgres://u@h/db?sslmode=disable&options=-c%20statement_timeout%3D5000", dsn)
}

func TestApplyStatementTimeout_Idempotent(t *testing.T) {
	t.Setenv("PG_STATEMENT_TIMEOUT", "5s")
	once := ApplyStatementTimeout("postgres://u@h/db?sslmode=disable")
	assert.Equal(t, once, ApplyStatementTimeout(once))
}

func TestApplyStatementTimeout_IgnoresInvalidDuration(t *testing.T) {
	withEnv(t, map[string]string{"PG_STATEMENT_TIMEOUT": "not-a-duration"})
	assert.Equal(t, "postgres://x", ApplyStatementTimeout("postgres://x"))
}

func TestApplyStatementTimeout_IgnoresNonPositiveDuration(t *testing.T) {
	withEnv(t, map[string]string{"PG_STATEMENT_TIMEOUT": "0s"})
	assert.Equal(t, "postgres://x", ApplyStatementTimeout("postgres://x"))
}

func TestDSNFromEnv_DATABASE_URL_TakesPrecedence(t *testing.T) {
	const rawDSN = "postgres://user:pass@host:5432/db"
	withEnv(t, map[string]string{
		"DATABASE_URL":         rawDSN,
		"PG_STATEMENT_TIMEOUT": "10s",
	})

	dsn := DSNFromEnv()
	assert.Equal(t, rawDSN, dsn, "DATABASE_URL must be returned verbatim, no timeout appended")
}

func TestDSNFromEnv_NoDatabaseURL_AppliesStatementTimeout(t *testing.T) {
	_ = os.Unsetenv("DATABASE_URL")
	withEnv(t, map[string]string{
		"PG_USER":              "app",
		"PG_DBNAME":            "catalog_admin",
		"PG_HOST":              "db.example",
		"PG_STATEMENT_TIMEOUT": "5s",
	})

	dsn := DSNFromEnv()
	assert.Contains(t, dsn, "db.example")
	assert.Contains(t, dsn, "sslmode=require")
	assert.Contains(t, dsn, "statement_timeout%3D5000",
		"unlike the DATABASE_URL branch, PG_*-built DSNs always have a query string, so ApplyStatementTimeout must run")
}

func TestMigrationDSNFromEnv_UsesMigrationVarWhenSet(t *testing.T) {
	_ = os.Unsetenv("PG_STATEMENT_TIMEOUT")
	t.Setenv("MIGRATION_DATABASE_URL", "postgres://m:x@migrations.example:5432/catalog_admin")
	assert.Equal(t,
		"postgres://m:x@migrations.example:5432/catalog_admin",
		MigrationDSNFromEnv())
}

func TestMigrationDSNFromEnv_AppliesStatementTimeout(t *testing.T) {
	t.Setenv("MIGRATION_DATABASE_URL", "postgres://m:x@migrations.example:5432/catalog_admin?sslmode=disable")
	t.Setenv("PG_STATEMENT_TIMEOUT", "5s")
	assert.Contains(t, MigrationDSNFromEnv(), "statement_timeout%3D5000")
}

func TestMigrationDSNFromEnv_FallsBackToDSNFromEnv(t *testing.T) {
	_ = os.Unsetenv("MIGRATION_DATABASE_URL")
	t.Setenv("DATABASE_URL", "postgres://a:b@app.example:5432/catalog_admin")
	assert.Equal(t, DSNFromEnv(), MigrationDSNFromEnv(),
		"unset MIGRATION_DATABASE_URL falls through to the app DSN")
}
