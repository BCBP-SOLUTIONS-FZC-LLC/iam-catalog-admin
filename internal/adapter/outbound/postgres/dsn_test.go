package postgres

import (
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

func TestApplyStatementTimeout_AppendsWhenSet(t *testing.T) {
	withEnv(t, map[string]string{"PG_STATEMENT_TIMEOUT": "5s"})
	dsn := ApplyStatementTimeout("postgres://x")
	assert.Contains(t, dsn, "postgres://x")
	assert.Contains(t, dsn, "statement_timeout")
	assert.Contains(t, dsn, "5000") // 5s in milliseconds
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

func TestMigrationDSNFromEnv_FallsBackToAppDSN(t *testing.T) {
	t.Setenv("MIGRATION_DATABASE_URL", "")
	assert.Equal(t, "postgres://app-dsn", MigrationDSNFromEnv("postgres://app-dsn"))
}

func TestMigrationDSNFromEnv_PrefersExplicitMigrationURL(t *testing.T) {
	withEnv(t, map[string]string{"MIGRATION_DATABASE_URL": "postgres://migration-dsn"})
	assert.Equal(t, "postgres://migration-dsn", MigrationDSNFromEnv("postgres://app-dsn"))
}
