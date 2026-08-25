# Database Schema

**Database:** `catalog_admin` on PostgreSQL (local dev: `postgres:16-alpine` via
`docker-compose.yml`, port 5535). Production runs behind PgBouncer in transaction-pooling mode
(`PG_BOUNCER_MODE=true` in `deploy/helm/values.yaml`).

**No RLS, no tenant context.** Neither `departments` nor `plans` carries a `tenant_id` column —
this service has no row-level security policy on either table, matching their treatment in the
source `iam-org-membership` LLD (which already listed both as "tables without RLS"). There is no
GUC to inject, no `GUCProvider` on `pgcommon.NewPool` in `main.go`.

**Pool configuration:**
```go
pgCfg, pgWarnings := pgcommon.ConfigFromEnv()   // validates and warns instead of silently defaulting
pgCfg.DSN = pgadapter.DSNFromEnv()              // ApplyStatementTimeout, skipped when DATABASE_URL is set
pgCfg.Logger = pgadapter.NewDomainLogger(log)   // routes pgcommon's slow-query WARN logs through this service's own structured logger
```
`pgcommon.ConfigFromEnv()` is used instead of a hand-rolled `PG_*` env parse — the hand-rolled
version had a real bug where a typo'd `PG_MAX_CONNS` silently became `0` connections
(`strconv.Atoi`'s error was discarded). `ConfigFromEnv` returns `[]pgcommon.ConfigWarning` instead;
every warning is logged unconditionally, and `validatePostgresConfig` escalates `PG_SSLMODE`/
`DATABASE_URL` insecure-config warnings to a startup panic only in `production`/`staging`.

`DSNFromEnv` (not a bare `ApplyStatementTimeout(pgCfg.DSN)`) — `DATABASE_URL` is returned verbatim
by `pgcommon.ConfigFromEnv` and may carry no `?` query string of its own, so appending
`PG_STATEMENT_TIMEOUT`'s `&options=...` directly onto it (the previous behavior) could produce a
malformed DSN when both were set. `DSNFromEnv` skips the append in that case — the same guard
`iam-org-membership`/`iam-user-profile`'s identically-named helper applies.

`PG_MAX_CONNS` is sized for a "read-heavy, write-rare workload" (LLD §15) — production default
`10`. `PG_MIN_CONNS` is intentionally **omitted** from both `.env-example` and
`deploy/helm/values.yaml`: `pgcommon.ConfigFromEnv` rejects `"0"` as an invalid, non-positive value
and falls back to its own default (2) with a startup warning — leave it unset rather than fighting
that validation.

**Migration DSN routing:** migrations **must bypass PgBouncer** because the migration runner uses
`pg_advisory_lock`, session-scoped and broken under transaction pooling. `MigrationDSNFromEnv`
prefers `MIGRATION_DATABASE_URL`; falls back to the already-resolved app DSN if unset. `main.go`
panics at startup if `PG_BOUNCER_MODE=true` and neither `MIGRATION_DATABASE_URL` nor
`DATABASE_URL` is set.

**Core tables (2 total — the entire schema):**

| Table | Purpose | Tenant-scoped | RLS | Deletable |
|-------|---------|----------------|-----|-----------|
| `departments` | Global operator-managed department catalog (LLD §5.1) | No | No | Never — hard delete is trigger-blocked (D-4/OP-3); retire via `is_active=false` |
| `plans` | Global three-tier entitlement catalog (LLD §5.2), PK is `code` | No | No | Never — no create/delete API at all (PLAN-4); tier set is fixed to the `tenant_plan` ENUM |

There is **no `processed_events` table** (no SQS consumer to dedup for) and **no outbox table**
(no SNS publisher) — see [api-and-events.md](api-and-events.md#events).

## `departments`

```sql
CREATE TABLE public.departments (
    id             uuid        NOT NULL DEFAULT gen_random_uuid(),
    code           text        NOT NULL,                        -- immutable (D-10)
    name           text        NOT NULL,
    is_system      boolean     NOT NULL DEFAULT false,           -- immutable (D-2)
    is_active      boolean     NOT NULL DEFAULT true,
    record_version bigint      NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT departments_pkey                    PRIMARY KEY (id),
    CONSTRAINT uq_departments_code                 UNIQUE (code),
    CONSTRAINT departments_code_not_empty          CHECK (code <> ''),
    CONSTRAINT departments_name_not_empty          CHECK (name <> ''),
    CONSTRAINT chk_system_department_active        CHECK (NOT (is_system = true AND is_active = false)),
    CONSTRAINT departments_record_version_positive CHECK (record_version > 0)
);
CREATE INDEX idx_departments_active ON public.departments (is_active) WHERE is_active = true;
```

Seeded with 5 system departments (`ENGINEERING`, `DESIGN`, `PROCUREMENT`, `FINANCE`, `LEGAL`,
all `is_system=true`) via `ON CONFLICT (code) DO NOTHING` in `000001_init_schema` — byte-identical
shape to the source O&M LLD's own departments table.

**Lifecycle invariants (D-1..D-11, ported unchanged from the source LLD, enforced by triggers —
see below):**
- `code` is immutable after creation (D-10) — an `UPDATE ... SET code = ...` raises a trigger
  exception, not a silent no-op.
- `is_system` is immutable after creation (D-2) — enforced at the **handler** layer
  (`department_handler.go`'s `Patch` rejects the field in the request body with `422
  field_immutable` before it ever reaches the DB), not by a DB trigger.
- A system department (`is_system=true`) can never be retired (`is_active=false`) — `chk_system_department_active` CHECK constraint (D-7/D-9).
- A system department's `name` is immutable — enforced by trigger, not CHECK (D-11) — see the
  triggers section below.
- Hard delete is unconditionally blocked regardless of `is_system` (D-4/OP-3) — a `BEFORE DELETE`
  trigger `RAISE EXCEPTION`s on every attempt.

## `plans`

```sql
CREATE TYPE public.tenant_plan AS ENUM ('starter', 'pro', 'enterprise');
CREATE TYPE public.branding_level AS ENUM ('none', 'logo');

CREATE TABLE public.plans (
    code                    public.tenant_plan     NOT NULL,
    display_name            text                   NOT NULL,
    workflow_template_limit int,                                 -- NULL = unlimited (CAT-D6)
    tender_limit            int,                                 -- NULL = unlimited (CAT-D6)
    trial_duration_days     int                    NOT NULL,
    sso_enabled             boolean                NOT NULL DEFAULT false,
    custom_branding         public.branding_level  NOT NULL DEFAULT 'none',
    feature_set             jsonb                  NOT NULL DEFAULT '{}'::jsonb,
    record_version          bigint                 NOT NULL DEFAULT 1,
    created_at              timestamptz            NOT NULL DEFAULT now(),
    updated_at              timestamptz            NOT NULL DEFAULT now(),
    CONSTRAINT plans_pkey                    PRIMARY KEY (code),
    CONSTRAINT plans_display_name_not_empty  CHECK (display_name <> ''),
    CONSTRAINT plans_wf_limit_nonneg         CHECK (workflow_template_limit IS NULL OR workflow_template_limit >= 0),
    CONSTRAINT plans_tender_limit_nonneg     CHECK (tender_limit IS NULL OR tender_limit >= 0),
    CONSTRAINT plans_trial_days_nonneg       CHECK (trial_duration_days >= 0),
    CONSTRAINT plans_record_version_positive CHECK (record_version > 0)
);
```

Seeded with the three fixed tiers in `000001_init_schema`:

| `code` | `workflow_template_limit` | `tender_limit` | `trial_duration_days` | `sso_enabled` | `custom_branding` | `feature_set` |
|---|---|---|---|---|---|---|
| `starter` | 5 | 10 | 30 | false | `none` | `{}` |
| `pro` | 50 | 100 | 30 | false | `logo` | `{}` |
| `enterprise` | `NULL` (unlimited) | `NULL` (unlimited) | 30 | true | `logo` | `{"require_mfa_all_users_allowed": true}` |

`NULL` on `workflow_template_limit`/`tender_limit` means **unlimited** (CAT-D6) — the shipped
representation is a nullable column, not a `-1` sentinel, per the source LLD's own recommendation.
`feature_set` is the **baseline** entitlement only; the effective per-tenant value
(`FeatureSet ⊕ tenants.feature_flags` override delta) is computed exclusively in Core at I-8 read
time (PLAN-6) — this service never reads or writes that delta.

## Triggers (in `000001_init_schema`, ported verbatim from the O&M LLD)

- **`touch_row()`** — `BEFORE UPDATE` on both tables, guarded by
  `WHEN (OLD.* IS DISTINCT FROM NEW.*)` (no spurious version bump on a no-op `UPDATE`). Bumps
  `record_version := OLD.record_version + 1` and `updated_at := now()`. This is the mechanism
  behind optimistic locking — see [flows-and-concurrency.md](flows-and-concurrency.md).
- **`prevent_department_delete()`** — `BEFORE DELETE` on `departments`. Unconditionally
  `RAISE EXCEPTION`s (D-4/OP-3) — there is no code path, `is_system` value, or role that can hard-
  delete a department row. Backs CAT-3's `405 method_not_allowed` at the service layer.
- **`prevent_department_code_change()`** — `BEFORE UPDATE OF code`. Raises if
  `OLD.code <> NEW.code` (D-10).
- **`prevent_system_department_name_change()`** — `BEFORE UPDATE OF name`. Raises if
  `OLD.is_system AND OLD.name <> NEW.name` (D-11), with a structured SQLSTATE + constraint name
  (`ERRCODE = 'check_violation'`, `CONSTRAINT = 'chk_system_department_name_immutable'`), not just
  a message string, so the Go side can match it via `pgcommon.IsCheckViolation`/`ConstraintName`
  exactly like the real `chk_system_department_active` CHECK constraint. Can't be a real CHECK
  constraint: the rule compares `OLD.name` to `NEW.name`, which CHECK has no way to express (CHECK
  only sees the new row).

## Migrations

A single consolidated migration, `000001_init_schema`: `tenant_plan`/`branding_level` ENUMs,
`departments`/`plans` tables + seed data, all four lifecycle triggers above, and the
`catalog_admin_app` role/grants. Kept as one file while this service remains pre-production — no
environment has applied an earlier multi-file history that needs preserving, so there's no
forward-only-migration constraint yet forcing a split.

Applied via `platform-pgcommon`'s `pkg/migrate.Runner` against an embedded
`//go:embed migrations/*.sql` filesystem (`internal/adapter/outbound/postgres/migrate.go`). No
down-migration is ever run in production; the `.down.sql` file exists for local dev rollback only.

## Roles (LLD §9)

**Single runtime role — no migrator/app split**, unlike every RLS-scoped sibling service:
`catalog_admin_app` is used for both migrations and app traffic. `000001_init_schema`'s own
comment states the reasoning directly: "no migrator/admin_readonly split is needed — no
cross-tenant data exists to read around."

- `catalog_admin_app` — `LOGIN NOBYPASSRLS`. `SELECT`/`INSERT`/`UPDATE` on `departments`;
  `SELECT`/`UPDATE` on `plans`. No `DELETE` grant on either table (defense-in-depth on top of the
  trigger block). `SELECT` is broad/mesh-readable; write is gated at the **service layer**
  (`platform_operator` role check), not by a DB grant split.

**CI verifies `NOBYPASSRLS` directly against a real Postgres**:
`TestRoles_AppRoleHasNoBYPASSRLS` (`test/e2e/roles_test.go`) queries `pg_roles.rolbypassrls` and
fails the build if it's ever `true` — mirrors `iam-org-membership`'s own
`TestRLS_Case1b_AppRoleHasNoBYPASSRLS`. This assertion previously existed only as a documentation
claim with no test behind it; added when a reconciliation pass found the gap (LLD §9).

## Optimistic locking (`record_version`)

Every write repository method (`DepartmentRepository.Update`, `PlanRepository.Update`) issues:
```sql
UPDATE <table> SET ... WHERE <pk> = $1 AND record_version = $2 RETURNING <cols>
```
Zero rows returned (`pgx.ErrNoRows`) is ambiguous between "row doesn't exist" and "version
mismatch" — both repositories disambiguate with a follow-up probe query
(`SELECT record_version FROM <table> WHERE <pk> = $1`) inside the **same transaction**: no row on
the probe → `404 <table>_not_found`; a row with a different version → `409
optimistic_lock_conflict` carrying the row's actual current `record_version` in the error body, so
the client can retry without a second round-trip.
