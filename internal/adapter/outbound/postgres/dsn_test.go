package postgres

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestDSNFromEnv_PrefersDatabaseURL(t *testing.T) {
	withEnv(t, map[string]string{"DATABASE_URL": "postgres://explicit"})
	assert.Equal(t, "postgres://explicit", DSNFromEnv())
}

func TestDSNFromEnv_BuildsFromParts(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	withEnv(t, map[string]string{
		"PG_HOST": "db.internal", "PG_PORT": "5432", "PG_USER": "app", "PG_PASSWORD": "secret",
		"PG_DBNAME": "catalog_admin", "PG_SSLMODE": "disable",
	})
	dsn := DSNFromEnv()
	require.Contains(t, dsn, "db.internal:5432")
	require.Contains(t, dsn, "catalog_admin")
	require.True(t, strings.Contains(dsn, "sslmode=disable"))
}

func TestDSNFromEnv_StatementTimeout(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	withEnv(t, map[string]string{
		"PG_HOST": "localhost", "PG_USER": "app", "PG_PASSWORD": "secret",
		"PG_STATEMENT_TIMEOUT": "5s",
	})
	dsn := DSNFromEnv()
	require.Contains(t, dsn, "statement_timeout")
}

func TestMigrationDSNFromEnv_FallsBackToDSNFromEnv(t *testing.T) {
	os.Unsetenv("MIGRATION_DATABASE_URL")
	withEnv(t, map[string]string{"DATABASE_URL": "postgres://app-dsn"})
	assert.Equal(t, "postgres://app-dsn", MigrationDSNFromEnv())

	withEnv(t, map[string]string{"MIGRATION_DATABASE_URL": "postgres://migration-dsn"})
	assert.Equal(t, "postgres://migration-dsn", MigrationDSNFromEnv())
}
