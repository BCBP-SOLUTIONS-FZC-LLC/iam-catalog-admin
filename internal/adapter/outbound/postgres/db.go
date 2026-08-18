// Package postgres implements the outbound repository ports backed by
// PostgreSQL through platform-pgcommon. Unlike iam-org-membership, this
// service has no RLS GUC to bridge (LLD §9 — neither departments nor plans
// carries a tenant_id) and no multi-statement write surface (CAT-FAIL-3 —
// every write is single-row, single-table), so there is no TxRunner/event
// injection seam here: withPool wraps each repository call in its own
// single-statement transaction via pgcommon.RunInTx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ApplyStatementTimeout appends a `statement_timeout` libpq option to dsn
// when PG_STATEMENT_TIMEOUT is set, e.g. "5s". pgcommon.ConfigFromEnv has
// no concept of statement timeout, so this is applied as a second step on
// top of the DSN it returns, not folded into pgcommon.Config itself.
func ApplyStatementTimeout(dsn string) string {
	t := os.Getenv("PG_STATEMENT_TIMEOUT")
	if t == "" {
		return dsn
	}
	d, err := time.ParseDuration(t)
	if err != nil || d <= 0 {
		return dsn
	}
	return dsn + fmt.Sprintf("&options=-c%%20statement_timeout%%3D%d", d.Milliseconds())
}

// MigrationDSNFromEnv returns the DSN for schema migrations. Migrations
// MUST bypass PgBouncer because the migration runner uses
// pg_advisory_lock, which is session-scoped and breaks under transaction
// pooling. MIGRATION_DATABASE_URL must be set whenever PG_BOUNCER_MODE=true.
// appDSN is the already-resolved application DSN (pgcommon.ConfigFromEnv's
// output, with ApplyStatementTimeout applied) — used as the fallback when
// MIGRATION_DATABASE_URL is unset.
func MigrationDSNFromEnv(appDSN string) string {
	if dsn := os.Getenv("MIGRATION_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return appDSN
}

// withPool runs fn inside a single-statement transaction. There is no
// higher-level RunInTx seam in this service (see package doc) — every
// repository method owns its own transaction boundary.
func withPool(ctx context.Context, pool *pgcommon.Pool, fn func(pgx.Tx) error) error {
	return wrapConnErr(pgcommon.RunInTx(ctx, pool, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(tx)
	}))
}

// wrapConnErr converts non-protocol database errors into
// ErrDependencyUnavailable. SQL-protocol errors (pgconn.PgError) and
// context cancellations pass through unchanged so the service layer can
// distinguish an integrity violation from a network outage.
func wrapConnErr(err error) error {
	if err == nil {
		return nil
	}
	var de *domain.DomainError
	if errors.As(err, &de) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return domain.NewError(domain.ErrDependencyUnavailable, "database unavailable")
}
