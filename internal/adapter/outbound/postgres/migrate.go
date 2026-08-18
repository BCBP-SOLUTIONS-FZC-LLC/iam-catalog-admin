package postgres

import (
	"context"
	"embed"
	"io/fs"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/domain"
	pgmigrate "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/migrate"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies this service's domain migrations (departments,
// plans, triggers, roles/grants). Uses the direct (non-PgBouncer) DSN
// because pg_advisory_lock is session-scoped. logger is optional — pass nil
// to run silently (as tests do); the composition root passes
// NewDomainLogger(log) so each applied migration step is logged through the
// service's own structured logger.
func RunMigrations(ctx context.Context, dsn string, logger domain.Logger) error {
	return runMigrationsFrom(ctx, migrationsFS, "migrations", dsn, logger)
}

func runMigrationsFrom(ctx context.Context, fsys fs.FS, subPath string, dsn string, logger domain.Logger) error {
	sub, err := fs.Sub(fsys, subPath)
	if err != nil {
		return err
	}
	return (&pgmigrate.Runner{FS: sub, DSN: dsn, Logger: logger}).Up(ctx)
}
