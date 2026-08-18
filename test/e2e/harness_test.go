//go:build e2e

// Package e2e_test spins up the full iam-catalog-admin stack against a
// real Postgres testcontainer and a real (miniredis-backed) Valkey
// endpoint, exposing it over an httptest.Server. Tests hit the running
// server via net/http with gateway-forwarded identity headers — the same
// shape an API gateway sets in production — exercising the actual route
// table, middleware chain, and gincommon wiring, not just handler methods
// called directly (that's what the package-level *_test.go unit tests
// already cover).
//
// Design mirrors iam-org-membership's test/e2e/harness_test.go:
//   - Postgres testcontainer per test, real migrations applied
//   - Every handler, every route, every middleware — same wiring as
//     cmd/catalog-admin-config/main.go
//   - miniredis stands in for Valkey (same wire protocol, real TCP server)
//   - No outbox runner, no SNS/SQS — this service has neither (LLD §10)
//
// Requires Docker on the runner. Tag: e2e.
package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	httpadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/inbound/http"
	catmetrics "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	pgadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/postgres"
	valkeyadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/valkey"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
)

// pingerFunc adapts a plain func to httpadapter.Pinger, mirroring
// cmd/catalog-admin-config/main.go's own adapter (pool.Health returns a
// struct, not an error).
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Health(ctx context.Context) error { return f(ctx) }

// e2eEnv bundles a fully wired stack + a live httptest.Server so tests can
// issue real HTTP requests.
type e2eEnv struct {
	ctx        context.Context
	pool       *pgcommon.Pool
	rawPool    *pgxpool.Pool
	cache      *valkeyadapter.Cache
	server     *httptest.Server
	baseURL    string
	metricsURL string
}

// newE2EEnv provisions Postgres + Valkey, wires the real production
// stack (same constructors cmd/catalog-admin-config/main.go calls), and
// boots an httptest server on top of it.
func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx := context.Background()

	pool, rawPool := setupE2EDB(t, ctx)

	mr := miniredis.RunT(t)
	cache := valkeyadapter.New("redis://" + mr.Addr())

	// Repositories — same constructors main.go calls.
	deptRepo := pgadapter.NewDepartmentRepository(pool)
	planRepo := pgadapter.NewPlanRepository(pool)

	// Services.
	deptSvc := service.NewDepartmentService(deptRepo, cache)
	planSvc := service.NewPlanService(planRepo, cache)

	// Handlers.
	deptH := httpadapter.NewDepartmentHandler(deptSvc)
	planH := httpadapter.NewPlanHandler(planSvc)
	internalH := httpadapter.NewInternalHandler(deptSvc, planSvc)

	// Router — same NewRouter cmd/catalog-admin-config/main.go calls, so
	// this harness can never drift from the production route table
	// (LLD §6/§7/§9).
	gin.SetMode(gin.TestMode)
	cfg := gincommon.Config{ServiceName: "iam-catalog-admin-e2e"}
	router := httpadapter.NewRouter(httpadapter.RouterConfig{
		GinConfig: cfg,

		DepartmentHandler: deptH,
		PlanHandler:       planH,
		InternalHandler:   internalH,

		Postgres: pingerFunc(func(ctx context.Context) error {
			if hs := pool.Health(ctx); !hs.Healthy {
				return fmt.Errorf("database not healthy")
			}
			return nil
		}),
		Cache: cache,
	})

	server := httptest.NewServer(router.Handler())
	t.Cleanup(server.Close)

	// catalog_admin_* business metrics (LLD §13.2) — registered the same way
	// main.go does, after NewRouter (which is what populates gincommon's
	// registerer/const-labels). Register is idempotent (sync.Once): only the
	// first test in this binary actually registers; later tests reuse it.
	catmetrics.Register(gincommon.MetricsRegisterer(), gincommon.MetricsConstLabels())

	// A separate metrics-only server, mirroring main.go's dedicated
	// METRICS_PORT listener (router.Handler() deliberately has no /metrics
	// route of its own — see router.go's registerInfraRoutes).
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsServer := httptest.NewServer(metricsMux)
	t.Cleanup(metricsServer.Close)

	return &e2eEnv{
		ctx: ctx, pool: pool, rawPool: rawPool, cache: cache,
		server: server, baseURL: server.URL, metricsURL: metricsServer.URL,
	}
}

// setupE2EDB spins up a Postgres testcontainer, runs the service's own
// migrations against it (the same RunMigrations cmd/catalog-admin-config
// calls at startup), and returns a pgcommon.Pool + a raw pgxpool for
// direct-SQL assertions.
func setupE2EDB(t *testing.T, ctx context.Context) (*pgcommon.Pool, *pgxpool.Pool) {
	t.Helper()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("catalog_admin_e2e"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	require.NoError(t, pgadapter.RunMigrations(ctx, dsn, nil))

	pool, err := pgcommon.NewPool(ctx, pgcommon.Config{DSN: dsn, MaxConns: 10})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	rawPool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(rawPool.Close)

	return pool, rawPool
}

// ── header helpers ───────────────────────────────────────────────────────

// operatorHeaders returns the gateway-forwarded identity headers for a
// platform_operator caller (CAT-1/CAT-2/CAT-3/CAT-4/CAT-5).
func operatorHeaders(req *http.Request) {
	req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
	req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	req.Header.Set("x-tenant-roles", "platform_operator")
}

// publicHeaders returns identity headers for any authenticated, non-operator
// caller (CAT-6/CAT-7 — "any authenticated caller").
func publicHeaders(req *http.Request) {
	req.Header.Set("x-user-id", "11111111-1111-1111-1111-111111111111")
	req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	req.Header.Set("x-tenant-roles", "tenant_admin")
}

// systemHeaders returns identity headers for the reserved iam-system
// principal (CAT-I1/CAT-I2 — mesh-only).
func systemHeaders(req *http.Request) {
	req.Header.Set("x-user-id", "iam-system")
	req.Header.Set("x-tenant-id", "22222222-2222-2222-2222-222222222222")
	req.Header.Set("x-tenant-roles", "iam-system")
}
