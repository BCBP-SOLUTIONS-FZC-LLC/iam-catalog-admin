package postgres

import (
	"context"
	"embed"
	"io/fs"

	pgmigrate "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/migrate"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies this service's domain migrations (departments,
// plans, triggers, roles/grants). Uses the direct (non-PgBouncer) DSN
// because pg_advisory_lock is session-scoped.
func RunMigrations(ctx context.Context, dsn string) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	return (&pgmigrate.Runner{FS: sub, DSN: dsn}).Up(ctx)
}
