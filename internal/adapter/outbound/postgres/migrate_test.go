package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunMigrationsFrom_InvalidSubPath(t *testing.T) {
	// fs.Sub returns an error when the subPath is not a valid fs path
	// (".." is rejected by fs.ValidPath).
	err := runMigrationsFrom(context.Background(), migrationsFS, "..", "unused-dsn")
	require.Error(t, err)
}
