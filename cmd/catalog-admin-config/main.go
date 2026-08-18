// Package main is the composition root for the Catalog / Admin Config
// Service (LLD §4). This service is a pure leaf: no outbound synchronous
// calls to any other IAM service, no outbox/SNS/SQS (LLD §10, CAT-EVT-1/2),
// and no RLS/tenant-context GUC (LLD §9 — neither departments nor plans
// carries a tenant_id). The startup sequence otherwise mirrors
// iam-org-membership's cmd/server/main.go so the two services share
// on-call ergonomics (same health/readyz/metrics shape, same middleware
// stack, same shutdown order).
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/docs/swagger"
	httpadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/inbound/http"
	catmetrics "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	pgadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/postgres"
	valkeyadapter "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/valkey"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/service"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/pgcommon"
)

// buildVersion is injected by -ldflags at build time (see Dockerfile / Makefile).
var buildVersion = "dev"

func main() {
	appEnv := envOr("APP_ENV", "dev")
	if appEnv != "dev" {
		gin.SetMode(gin.ReleaseMode)
	}

	validateRequiredEnv(appEnv)

	// ── 1. Logger ─────────────────────────────────────────────────────────
	log, err := logger.NewLogger(appEnv)
	if err != nil {
		panic("init logger: " + err.Error())
	}

	catmetrics.Register()
	httpadapter.SetLogger(log)
	valkeyadapter.SetLogger(log)

	// ── 2. Tracing (opt-in) ───────────────────────────────────────────────
	var shutdownTracing func()
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		shutdownTracing = gincommon.InitTracingFromEnv()
	} else {
		shutdownTracing = func() {}
	}

	cfg := gincommon.Config{
		Logger:       log,
		ServiceName:  envOr("APP_NAME", "catalog-admin-config"),
		BuildVersion: envOr("BUILD_VERSION", buildVersion),
	}

	// ── 3. Database ───────────────────────────────────────────────────────
	// No GUCProvider — this service has no RLS/tenant-context GUC to bridge
	// (LLD §9); pgcommon.NewPool is still used for pooling, slow-query
	// logging, and OTel tracing, which are RLS-independent.
	//
	// pgcommon.ConfigFromEnv() (not a hand-rolled DSN/MaxConns/MinConns/
	// SlowQueryThreshold parse) — closes a real bug the hand-rolled version
	// had: a typo'd PG_MAX_CONNS used to silently become 0 connections
	// (strconv.Atoi's error was discarded); ConfigFromEnv validates and
	// warns instead. Warnings are logged, not silently applied — see
	// validatePostgresConfig, which also escalates specific warnings to a
	// hard failure in prod/staging, since this service's own policy (like
	// the VALKEY_URL check below) is to fail fast on insecure config there,
	// not just warn.
	pgCfg, pgWarnings := pgcommon.ConfigFromEnv()
	for _, w := range pgWarnings {
		log.Warn("postgres config warning", map[string]interface{}{"key": w.Key, "reason": w.Reason})
	}
	pgCfg.DSN = pgadapter.ApplyStatementTimeout(pgCfg.DSN)
	validatePostgresConfig(appEnv, pgCfg.DSN, pgWarnings)

	migrationDSN := pgadapter.MigrationDSNFromEnv(pgCfg.DSN)

	pool, err := pgcommon.NewPool(context.Background(), pgCfg)
	if err != nil {
		panic(fmt.Sprintf("connect to postgres: %v", err))
	}
	defer pool.Close()

	ctx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()

	if err := pgadapter.RunMigrations(ctx, migrationDSN); err != nil {
		panic(fmt.Sprintf("domain migrations: %v", err))
	}

	// ── 4. Cache ──────────────────────────────────────────────────────────
	cache := valkeyadapter.New(envOr("VALKEY_URL", "localhost:6379"))
	defer func() {
		if cerr := cache.Close(); cerr != nil {
			log.Error("valkey close error", map[string]interface{}{"error": cerr.Error()})
		}
	}()

	// ── 5. Repositories, services, handlers ───────────────────────────────
	deptRepo := pgadapter.NewDepartmentRepository(pool)
	planRepo := pgadapter.NewPlanRepository(pool)

	// CATALOG_TTL_SECONDS externalizes the cat:departments/cat:plans cache
	// TTL (LLD §15) — previously a compiled constant in both services. A
	// malformed value falls back to the 60s default (WithCacheTTL ignores
	// d <= 0) rather than crashing — cache TTL is low-stakes enough not to
	// warrant validatePostgresConfig's startup-panic treatment — but the
	// parse error is still logged, not silently discarded.
	catalogTTLRaw := envOr("CATALOG_TTL_SECONDS", "60")
	catalogTTLSeconds, err := strconv.Atoi(catalogTTLRaw)
	if err != nil {
		log.Warn("invalid CATALOG_TTL_SECONDS, falling back to 60s default", map[string]interface{}{"value": catalogTTLRaw, "error": err.Error()})
		catalogTTLSeconds = 60
	}
	catalogTTL := time.Duration(catalogTTLSeconds) * time.Second

	deptSvc := service.NewDepartmentService(deptRepo, cache).WithCacheTTL(catalogTTL)
	planSvc := service.NewPlanService(planRepo, cache).WithCacheTTL(catalogTTL)

	deptH := httpadapter.NewDepartmentHandler(deptSvc)
	planH := httpadapter.NewPlanHandler(planSvc)
	internalH := httpadapter.NewInternalHandler(deptSvc, planSvc)

	// ── 6. Router — all routing/middleware wiring lives in the inbound
	// HTTP adapter (internal/adapter/inbound/http/router.go), not here.
	// main.go's job is to construct dependencies and hand them to
	// NewRouter (mirrors iam-org-membership's composition root).
	router := httpadapter.NewRouter(httpadapter.RouterConfig{
		GinConfig: cfg,
		Docs: httpadapter.DocsConfig{
			Environment: appEnv,
			Enabled:     os.Getenv("DOCS_ENABLED") == "true",
			AuthToken:   os.Getenv("DOCS_AUTH_TOKEN"),
		},

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

	// ── 7. Graceful shutdown ───────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + envOr("APP_PORT", "8081"),
		Handler:      router.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 35 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Metrics on a dedicated port/listener, separate from the API server
	// above — so a NetworkPolicy can grant the monitoring namespace scrape
	// access without also granting it access to the tenant-facing/gateway
	// API surface. Mirrors iam-org-membership's/iam-tender-acl's/
	// iam-user-profile's identical split.
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsServer := &http.Server{
		Addr:              ":" + envOr("METRICS_PORT", "9090"),
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("service starting", map[string]interface{}{
		"service": cfg.ServiceName,
		"version": cfg.BuildVersion,
		"env":     appEnv,
		"addr":    srv.Addr,
	})
	// recoverAndExit is defense-in-depth: ListenAndServe itself essentially
	// never panics, but without this a panic in either goroutine (now or
	// from future code added here) would otherwise be silently swallowed
	// by the Go runtime's default top-level goroutine handling — which
	// actually crashes the whole process anyway, just without a
	// structured log line first. Logging then exiting explicitly gives
	// the same "let Kubernetes restart the pod" outcome, deliberately,
	// with a diagnosable log line instead of a bare stack trace on stderr.
	recoverAndExit := func(name string) {
		if r := recover(); r != nil {
			log.Error("panic in server goroutine", map[string]interface{}{"goroutine": name, "panic": fmt.Sprintf("%v", r)})
			os.Exit(1)
		}
	}
	go func() {
		defer recoverAndExit("api")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", map[string]interface{}{"error": err.Error()})
		}
	}()
	go func() {
		defer recoverAndExit("metrics")
		log.Info("metrics server starting", map[string]interface{}{"addr": metricsServer.Addr})
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics server error", map[string]interface{}{"error": err.Error()})
		}
	}()

	<-quit
	log.Info("shutdown signal received — draining", nil)

	// Order: (1) HTTP Shutdown — stop accepting new requests, drain
	// in-flight; (2) cancelBackground; (3) shutdownTracing; (4) logger
	// flush. No outbox/SQS-consumer drain step — this service has neither
	// (LLD §10, CAT-EVT-1/2).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("HTTP server shutdown error", map[string]interface{}{"error": err.Error()})
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Error("metrics server shutdown error", map[string]interface{}{"error": err.Error()})
	}
	cancelBackground()
	shutdownTracing()
	if err := gincommon.Shutdown(log); err != nil {
		log.Error("logger/tracer flush error", map[string]interface{}{"error": err.Error()})
	}
}

// ── helpers ────────────────────────────────────────────────────────────

// pingerFunc adapts a plain func to httpadapter.Pinger so /readyz's
// Postgres check (pool.Health returns a struct, not an error) fits the
// same interface as valkeyadapter.Cache's Health method.
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Health(ctx context.Context) error { return f(ctx) }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// validateRequiredEnv panics with a descriptive message if any environment
// variable required for correct operation is absent. Dev mode relaxes
// checks so local setups without Valkey/TLS still start. Postgres-specific
// validation lives in validatePostgresConfig instead, called later once
// pgcommon.ConfigFromEnv has resolved a DSN and its own warnings — this
// function only covers what ConfigFromEnv has no concept of (Valkey,
// migration DSN routing).
func validateRequiredEnv(appEnv string) {
	var missing []string
	if os.Getenv("VALKEY_URL") == "" {
		missing = append(missing, "VALKEY_URL: Valkey address is required for caching")
	}
	if os.Getenv("MIGRATION_DATABASE_URL") == "" && os.Getenv("PG_BOUNCER_MODE") == "true" &&
		os.Getenv("DATABASE_URL") == "" {
		missing = append(missing, "MIGRATION_DATABASE_URL: required when PG_BOUNCER_MODE=true — migrations must bypass PgBouncer")
	}
	isProd := appEnv == "production" || appEnv == "staging"
	if isProd {
		if v := os.Getenv("VALKEY_URL"); v != "" && !strings.HasPrefix(v, "rediss://") {
			missing = append(missing, "VALKEY_URL: must use rediss:// in production/staging")
		}
	}
	if len(missing) > 0 {
		msg := "startup aborted — required env vars missing or misconfigured:\n"
		for _, m := range missing {
			msg += "  • " + m + "\n"
		}
		panic(msg)
	}
}

// validatePostgresConfig panics if pgcommon.ConfigFromEnv couldn't build a
// usable DSN at all (dsn == ""), or — in production/staging only — if any
// of its warnings flag an insecure SSL mode (PG_SSLMODE/DATABASE_URL
// negotiating plaintext: disable/allow/prefer). ConfigFromEnv itself only
// warns and falls back to a safe default for these; this service's own
// policy, like the VALKEY_URL check in validateRequiredEnv, is to fail
// fast on insecure config in production rather than silently proceed.
func validatePostgresConfig(appEnv, dsn string, warnings []pgcommon.ConfigWarning) {
	var missing []string
	if dsn == "" {
		missing = append(missing, "DATABASE_URL (or PG_USER + PG_DBNAME): PostgreSQL connection required — no DSN could be built")
	}
	isProd := appEnv == "production" || appEnv == "staging"
	if isProd {
		for _, w := range warnings {
			if w.Key == "PG_SSLMODE" || w.Key == "DATABASE_URL" {
				missing = append(missing, fmt.Sprintf("%s: %s (insecure in production/staging)", w.Key, w.Reason))
			}
		}
	}
	if len(missing) > 0 {
		msg := "startup aborted — required env vars missing or misconfigured:\n"
		for _, m := range missing {
			msg += "  • " + m + "\n"
		}
		panic(msg)
	}
}
