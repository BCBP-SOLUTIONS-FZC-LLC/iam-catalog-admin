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
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

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
	dsn := pgadapter.DSNFromEnv()
	migrationDSN := pgadapter.MigrationDSNFromEnv()

	maxConns, _ := strconv.Atoi(envOr("PG_MAX_CONNS", "10"))
	minConns, _ := strconv.Atoi(envOr("PG_MIN_CONNS", "0"))
	slowQueryThreshold := 200 * time.Millisecond
	if s := os.Getenv("PG_SLOW_QUERY_THRESHOLD"); s != "" {
		if d, perr := time.ParseDuration(s); perr == nil && d > 0 {
			slowQueryThreshold = d
		}
	}

	pool, err := pgcommon.NewPool(context.Background(), pgcommon.Config{
		DSN:                dsn,
		MaxConns:           int32(maxConns),
		MinConns:           int32(minConns),
		PGBouncerMode:      os.Getenv("PG_BOUNCER_MODE") == "true",
		SlowQueryThreshold: slowQueryThreshold,
	})
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

	deptSvc := service.NewDepartmentService(deptRepo, cache)
	planSvc := service.NewPlanService(planRepo, cache)

	deptH := httpadapter.NewDepartmentHandler(deptSvc)
	planH := httpadapter.NewPlanHandler(planSvc)
	internalH := httpadapter.NewInternalHandler(deptSvc, planSvc)

	// ── 6. Router ─────────────────────────────────────────────────────────
	r := gin.New()
	r.HandleMethodNotAllowed = true

	// Swagger UI is registered BEFORE any middleware so the timeout and
	// observability wrappers don't interfere with its streaming response
	// writers. Docs are served when not in production OR when explicitly
	// opted in via DOCS_ENABLED=true. When enabled in production, set
	// DOCS_AUTH_TOKEN to require a bearer token — otherwise the full API
	// surface is exposed unauthenticated.
	if appEnv != "production" || os.Getenv("DOCS_ENABLED") == "true" {
		// Defense-in-depth security headers for the docs surface. Swagger UI
		// requires 'unsafe-inline' and 'unsafe-eval' for its bundled JS.
		docsSecHeaders := func(c *gin.Context) {
			c.Header("X-Frame-Options", "DENY")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("Content-Security-Policy",
				"default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
					"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'")
			c.Next()
		}

		var docsAuthMiddleware gin.HandlerFunc
		if appEnv == "production" {
			// In production, gate docs behind a static bearer token. Set
			// DOCS_AUTH_TOKEN to a secret value; leave it empty to skip the
			// guard (logged as a warning — ensure the deployment is not
			// internet-reachable).
			if docsToken := os.Getenv("DOCS_AUTH_TOKEN"); docsToken != "" {
				docsAuthMiddleware = func(c *gin.Context) {
					if c.GetHeader("Authorization") != "Bearer "+docsToken {
						c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
							"code":    "unauthorized",
							"message": "docs require Authorization: Bearer <DOCS_AUTH_TOKEN>",
						})
					}
				}
			} else {
				log.Warn("DOCS_ENABLED in production without DOCS_AUTH_TOKEN — API surface is unauthenticated", nil)
				docsAuthMiddleware = func(c *gin.Context) { c.Next() }
			}
		} else {
			docsAuthMiddleware = func(c *gin.Context) { c.Next() }
		}

		stdSwagger := ginSwagger.WrapHandler(swaggerFiles.Handler)
		r.GET("/swagger/*any", docsSecHeaders, docsAuthMiddleware, func(c *gin.Context) {
			switch {
			case strings.HasSuffix(c.Request.URL.Path, "/index.css"):
				httpadapter.SwaggerThemeHandler(c)
			case strings.HasSuffix(c.Request.URL.Path, "/swagger-initializer.js"):
				httpadapter.SwaggerInitializerHandler(c)
			default:
				stdSwagger(c)
			}
		})
	}

	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	})
	r.Use(gincommon.TimeoutMiddleware(30 * time.Second))
	r.Use(gincommon.ObservabilityMiddlewares(cfg)...)
	r.Use(httpadapter.NormalizeAuthErrors())

	r.GET("/healthz", gincommon.HealthHandler())
	r.GET("/readyz", func(c *gin.Context) {
		hs := pool.Health(c.Request.Context())
		if !hs.Healthy {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "database": "down"})
			return
		}
		if err := cache.Health(c.Request.Context()); err != nil {
			// Cache is advisory (CAT-FAIL-1), but /readyz still fails so the
			// pod is removed from rotation while Valkey is down — otherwise
			// every cache miss silently amplifies DB load.
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "cache": "down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Protected API group. No GUCBridge here (see package doc) — just
	// identity parsing (IdentityBridgeMiddleware) for role checks.
	protected := append(
		gincommon.ProtectedMiddlewares(cfg),
		httpadapter.IdentityBridgeMiddleware(),
		httpadapter.RequireJSONContentType(),
	)
	v1 := r.Group("/api/v1", protected...)
	{
		// CAT-6/CAT-7 — public, any authenticated caller (LLD §6).
		v1.GET("/departments", deptH.List)
		v1.GET("/departments/:id", deptH.Get)

		// Operator routes — CAT-1/CAT-2/CAT-3/CAT-4/CAT-5 (LLD §6, §9).
		op := v1.Group("/operator", httpadapter.RequireOperatorRole())
		op.POST("/departments", deptH.Create)              // CAT-1
		op.PATCH("/departments/:id", deptH.Patch)          // CAT-2
		op.DELETE("/departments/:id", deptH.DeleteBlocked) // CAT-3
		op.GET("/plans", planH.List)                       // CAT-4
		op.GET("/plans/:code", planH.Get)                  // CAT-4
		op.PATCH("/plans/:code", planH.Patch)              // CAT-5

		// Internal routes — CAT-I1/CAT-I2, mesh-only (LLD §7, §9).
		internal := v1.Group("/internal", httpadapter.RequireSystemRole())
		internal.GET("/departments", internalH.Departments) // CAT-I1
		internal.GET("/plans", internalH.Plans)             // CAT-I2
	}

	// ── 7. Graceful shutdown ───────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + envOr("APP_PORT", "8081"),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 35 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("service starting", map[string]interface{}{
		"service": cfg.ServiceName,
		"version": cfg.BuildVersion,
		"env":     appEnv,
		"addr":    srv.Addr,
	})
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", map[string]interface{}{"error": err.Error()})
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
	cancelBackground()
	shutdownTracing()
	if err := gincommon.Shutdown(log); err != nil {
		log.Error("logger/tracer flush error", map[string]interface{}{"error": err.Error()})
	}
}

// ── helpers ────────────────────────────────────────────────────────────

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// validateRequiredEnv panics with a descriptive message if any environment
// variable required for correct operation is absent. Dev mode relaxes
// checks so local setups without Valkey/TLS still start.
func validateRequiredEnv(appEnv string) {
	var missing []string
	if os.Getenv("VALKEY_URL") == "" {
		missing = append(missing, "VALKEY_URL: Valkey address is required for caching")
	}
	if os.Getenv("DATABASE_URL") == "" &&
		(os.Getenv("PG_HOST") == "" || os.Getenv("PG_USER") == "" || os.Getenv("PG_PASSWORD") == "") {
		missing = append(missing, "DATABASE_URL (or PG_HOST + PG_USER + PG_PASSWORD): PostgreSQL connection required")
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
