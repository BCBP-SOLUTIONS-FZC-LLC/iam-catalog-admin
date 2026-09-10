// Package postgres implements the outbound repository ports backed by
// PostgreSQL through platform-pgcommon. Unlike iam-org-membership, this
// service has no RLS GUC to bridge (LLD §10 — neither departments nor plans
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
	"strings"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
	"github.com/jackc/pgx/v5"
)

// DSNFromEnv builds the application pool's DSN via pgcommon.ConfigFromEnv,
// applying ApplyStatementTimeout on top — except when DATABASE_URL is set,
// in which case pgcommon returns it verbatim and it may have no `?` query
// string for ApplyStatementTimeout to safely append onto (see its own
// contract below). Mirrors iam-org-membership/iam-realm-provisioner's
// postgres.DSNFromEnv so DSN assembly has exactly one implementation.
func DSNFromEnv() string {
	cfg, _ := pgcommon.ConfigFromEnv()
	if os.Getenv("DATABASE_URL") != "" {
		return cfg.DSN
	}
	return ApplyStatementTimeout(cfg.DSN)
}

// ApplyStatementTimeout appends a server-side statement_timeout option to dsn
// so hung queries release pool connections instead of holding them for the
// full HTTP deadline. PG_STATEMENT_TIMEOUT accepts a Go duration string
// (e.g. "5s", "500ms"). This has no pgcommon equivalent — pgcommon.Config has
// no statement-timeout field — so it remains a small extension layered on
// top of the pgcommon-built DSN rather than a full DSN builder. Ignored when
// dsn is empty, already contains statement_timeout, or PG_STATEMENT_TIMEOUT
// is unset.
//
// dsn is always the URL form (postgres://…, per DATABASE_URL/
// MIGRATION_DATABASE_URL's own naming), but not always guaranteed to already
// carry a "?" query string — DSNFromEnv's caller only ever hits this with
// pgcommon.ConfigFromEnv's own DSN (which always has "?sslmode=..."), but
// MigrationDSNFromEnv applies it directly to the raw MIGRATION_DATABASE_URL
// env var, which an operator could set with no query string at all (a
// perfectly valid Postgres URL). Appending "&options=..." unconditionally in
// that case produced "...db&options=..." — no leading "?", a malformed DSN
// that would break migrations at startup. Picks "?" or "&" based on whether
// dsn already contains one, so both call sites are safe.
func ApplyStatementTimeout(dsn string) string {
	if dsn == "" {
		return dsn
	}
	if t := os.Getenv("PG_STATEMENT_TIMEOUT"); t != "" {
		if d, err := time.ParseDuration(t); err == nil && d > 0 {
			if strings.Contains(dsn, "statement_timeout") {
				return dsn
			}
			sep := "&"
			if !strings.Contains(dsn, "?") {
				sep = "?"
			}
			dsn += fmt.Sprintf("%soptions=-c%%20statement_timeout%%3D%d", sep, d.Milliseconds())
		}
	}
	return dsn
}

// MigrationDSNFromEnv returns the DSN for schema migrations. Migrations
// MUST bypass PgBouncer because the migration runner uses
// pg_advisory_lock, which is session-scoped and breaks under transaction
// pooling. MIGRATION_DATABASE_URL must be set whenever PG_BOUNCER_MODE=true.
// Falls back to DSNFromEnv() when unset — the same no-arg signature as
// iam-org-membership / iam-realm-provisioner.
func MigrationDSNFromEnv() string {
	if dsn := os.Getenv("MIGRATION_DATABASE_URL"); dsn != "" {
		return ApplyStatementTimeout(dsn)
	}
	return DSNFromEnv()
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
// ErrDependencyUnavailable. SQL-protocol errors that are not
// connectivity/resource classes pass through so the service layer can
// distinguish an integrity violation from a network outage.
//
// SQLSTATE class 08 (connection exception), 53 (insufficient resources),
// 57 (operator intervention) and 58 (system error) are remapped to
// domain.ErrDBUnavailable here so HTTP HandleError never needs to inspect
// a raw *pgconn.PgError — those classes are availability failures (503).
func wrapConnErr(err error) error {
	if err == nil {
		return nil
	}
	var de *domain.DomainError
	if errors.As(err, &de) {
		return err
	}
	if pgcommon.IsConnectionException(err) || pgcommon.IsInsufficientResources(err) || isOperatorOrSystemErrorSQLState(err) {
		return domain.NewError(domain.ErrDBUnavailable, "database unavailable")
	}
	if pgcommon.IsPgError(err) {
		return err // server responded with a SQL error — not a connectivity failure
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return domain.NewError(domain.ErrDependencyUnavailable, "database unavailable")
}

// isOperatorOrSystemErrorSQLState reports whether err is a Postgres error
// in SQLSTATE class 57 or 58. pgcommon v1.3.0 has dedicated helpers for
// 08/53 but not these two; we classify via the pgconn Error() text
// ("… (SQLSTATE 57P01)") so callers never import pgconn.
func isOperatorOrSystemErrorSQLState(err error) bool {
	if !pgcommon.IsPgError(err) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 57") || strings.Contains(msg, "SQLSTATE 58")
}
