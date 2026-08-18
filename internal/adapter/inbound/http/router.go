// Package http's router.go is the single source of truth for this
// service's HTTP surface: every path, method, middleware, and route group.
// It mirrors iam-org-membership's own router.go — main.go's job is to
// construct dependencies and call NewRouter, not to encode routing
// decisions itself. Before this file existed, main.go's inline route
// table had already drifted from test/e2e/harness_test.go's hand-copied
// duplicate (missing /metrics and the docs surface) — a single NewRouter
// used by both main.go and the e2e harness makes that drift impossible.
//
// Unlike iam-org-membership, this service is a pure leaf (LLD §4): no
// RLS/tenant-context GUC (LLD §9), no outbox/SNS/SQS (LLD §10), so there
// is no GUCBridge middleware, no tenant/membership gates, and no Outbox
// pinger — IdentityBridgeMiddleware only does identity parsing.
package http

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	catmetrics "github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

// Pinger is satisfied by any dependency /readyz must check. pgcommon.Pool
// and valkey.Cache are each wrapped/used to implement it by the
// composition root (cmd/catalog-admin-config/main.go) — this package
// never imports those concrete outbound types directly.
type Pinger interface {
	Health(ctx context.Context) error
}

// DocsConfig controls whether — and how — the interactive Swagger UI is
// exposed. Outside production it's always on; in production it's opt-in
// via Enabled and, if AuthToken is set, gated behind a bearer token so the
// API surface isn't exposed to the open internet by default. Mirrors
// iam-org-membership's DocsConfig exactly (minus the AsyncAPI surface,
// which this service has no events for).
type DocsConfig struct {
	Environment string
	Enabled     bool
	AuthToken   string
}

func (d DocsConfig) active() bool {
	return d.Environment != "production" || d.Enabled
}

// RouterConfig bundles every dependency NewRouter needs: the already-built
// handlers (composition root's job to construct), the two readiness
// pingers, and the shared gincommon/docs configuration.
type RouterConfig struct {
	GinConfig gincommon.Config
	Docs      DocsConfig

	DepartmentHandler *DepartmentHandler
	PlanHandler       *PlanHandler
	InternalHandler   *InternalHandler

	Postgres Pinger
	Cache    Pinger
}

// Router owns the Gin engine for this service.
type Router struct {
	engine *gin.Engine
}

// Handler returns the http.Handler to serve.
func (r *Router) Handler() http.Handler { return r.engine }

// NewRouter builds and wires every route this service exposes: public
// /api/v1/*, operator /api/v1/operator/*, internal /api/v1/internal/*,
// the unauthenticated infra probes, and (when enabled) the docs surface.
func NewRouter(cfg RouterConfig) *Router {
	r := gin.New()
	// Return 405 Method Not Allowed (with Allow header) when a path exists
	// but the HTTP method is not registered, instead of the default 404.
	r.HandleMethodNotAllowed = true
	// Gin's built-in 405 handler returns an empty body. Override it so every
	// 405 carries the same JSON error envelope as all other error responses
	// (LLD §20). The Allow header is preserved — Gin sets it before this
	// handler fires.
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, map[string]any{
			"error":   "method_not_allowed",
			"code":    "method_not_allowed",
			"message": c.Request.Method + " is not allowed on this endpoint",
			"status":  http.StatusMethodNotAllowed,
		})
	})

	registerDocsRoutes(r, cfg)

	// 1 MB body cap to prevent memory exhaustion via oversized JSON payloads.
	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	})
	// 30 s hard deadline on every request.
	r.Use(gincommon.TimeoutMiddleware(30 * time.Second))
	// Panic recovery, request-ID, tracing, correlation, metrics, logging.
	r.Use(gincommon.ObservabilityMiddlewares(cfg.GinConfig)...)
	// Normalize platform-gincommon 401 responses to include the code field.
	r.Use(NormalizeAuthErrors())

	registerInfraRoutes(r, cfg)
	registerAPIRoutes(r, cfg)

	return &Router{engine: r}
}

// ── Infra routes (unauthenticated) ──────────────────────────────────────

type healthHandlers struct {
	postgres Pinger
	cache    Pinger
}

func registerInfraRoutes(r *gin.Engine, cfg RouterConfig) {
	h := &healthHandlers{postgres: cfg.Postgres, cache: cfg.Cache}
	// healthz is gincommon.HealthHandler() itself, not a local
	// reimplementation — a shared-library reuse audit found this route
	// used to reproduce that handler's exact {"status":"ok"} body by hand.
	r.GET("/healthz", gincommon.HealthHandler())
	r.GET("/readyz", h.readyz)
	// /metrics is served on its own dedicated port (METRICS_PORT, see
	// cmd/catalog-admin-config/main.go's metricsServer) — not on this
	// router — so scraping never shares a listener with tenant-facing/
	// gateway traffic. Mirrors iam-org-membership's/iam-tender-acl's/
	// iam-user-profile's identical split.
}

// readyz checks Postgres and Valkey, and reports 503 if either is not
// ready. Cache is advisory (CAT-FAIL-1) everywhere else, but /readyz
// still fails while Valkey is down so the pod is removed from rotation —
// otherwise every cache miss silently amplifies DB load.
func (h *healthHandlers) readyz(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.postgres.Health(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "database": "down"})
		return
	}
	if err := h.cache.Health(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "cache": "down"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// ── Docs surface ─────────────────────────────────────────────────────────

// registerDocsRoutes is registered BEFORE any middleware so the timeout
// and observability wrappers don't interfere with Swagger UI's streaming
// response writers.
func registerDocsRoutes(r *gin.Engine, cfg RouterConfig) {
	if !cfg.Docs.active() {
		return
	}

	// Defense-in-depth security headers for the docs surface. Swagger UI
	// requires 'unsafe-inline' and 'unsafe-eval' for its bundled JS.
	secHeaders := func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'")
		c.Next()
	}

	var authMiddleware gin.HandlerFunc = func(c *gin.Context) { c.Next() }
	if cfg.Docs.Environment == "production" {
		if cfg.Docs.AuthToken != "" {
			token := cfg.Docs.AuthToken
			authMiddleware = func(c *gin.Context) {
				if c.GetHeader("Authorization") != "Bearer "+token {
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
						"code":    "unauthorized",
						"message": "docs require Authorization: Bearer <DOCS_AUTH_TOKEN>",
					})
					return
				}
				c.Next()
			}
		} else if cfg.GinConfig.Logger != nil {
			cfg.GinConfig.Logger.Warn("DOCS_ENABLED in production without DOCS_AUTH_TOKEN — API surface is unauthenticated", nil)
		}
	}

	stdSwagger := ginSwagger.WrapHandler(swaggerFiles.Handler)
	r.GET("/swagger/*any", secHeaders, authMiddleware, func(c *gin.Context) {
		switch {
		case strings.HasSuffix(c.Request.URL.Path, "/index.css"):
			SwaggerThemeHandler(c)
		case strings.HasSuffix(c.Request.URL.Path, "/swagger-initializer.js"):
			SwaggerInitializerHandler(c)
		default:
			stdSwagger(c)
		}
	})
}

// requestMetricsMiddleware records LLD §13.2's
// catalog_admin_requests_total{route,status} and
// catalog_admin_request_duration_seconds{route,quantile} for every request
// through the /api/v1 group — this service's own business-level rollup,
// distinct from (and in addition to) gincommon's generic http_requests_total/
// http_request_duration_seconds (method/status_class/error_class labels,
// le buckets, shared across the fleet). c.FullPath() is the matched route
// template (e.g. "/api/v1/departments/:id"), not the raw request path, so
// cardinality stays bounded regardless of how many distinct UUIDs are requested.
func requestMetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		status := strconv.Itoa(c.Writer.Status())
		catmetrics.RequestsTotal.WithLabelValues(route, status).Inc()
		catmetrics.RequestDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	}
}

// ── Business API routes ──────────────────────────────────────────────────

func registerAPIRoutes(r *gin.Engine, cfg RouterConfig) {
	deptH := cfg.DepartmentHandler
	planH := cfg.PlanHandler
	internalH := cfg.InternalHandler

	// Protected API group. requestMetricsMiddleware runs first so it
	// records every terminal outcome (LLD §13.2's
	// catalog_admin_requests_total/_request_duration_seconds), including
	// requests rejected by auth below it in the chain — not just ones that
	// reach a handler. No GUCBridge here (see package doc) — just identity
	// parsing (IdentityBridgeMiddleware) for role checks.
	protected := append(
		[]gin.HandlerFunc{requestMetricsMiddleware()},
		gincommon.ProtectedMiddlewares(cfg.GinConfig)...,
	)
	protected = append(protected, IdentityBridgeMiddleware(), RequireJSONContentType())
	v1 := r.Group("/api/v1", protected...)

	// CAT-6/CAT-7 — public, any authenticated caller (LLD §6).
	v1.GET("/departments", deptH.List)
	v1.GET("/departments/:id", deptH.Get)

	// Operator routes — CAT-1/CAT-2/CAT-3/CAT-4/CAT-5 (LLD §6, §9).
	op := v1.Group("/operator", RequireOperatorRole())
	op.POST("/departments", deptH.Create)              // CAT-1
	op.PATCH("/departments/:id", deptH.Patch)          // CAT-2
	op.DELETE("/departments/:id", deptH.DeleteBlocked) // CAT-3
	op.GET("/plans", planH.List)                       // CAT-4
	op.GET("/plans/:code", planH.Get)                  // CAT-4
	op.PATCH("/plans/:code", planH.Patch)              // CAT-5

	// Internal routes — CAT-I1/CAT-I2, mesh-only (LLD §7, §9).
	internal := v1.Group("/internal", RequireSystemRole())
	internal.GET("/departments", internalH.Departments) // CAT-I1
	internal.GET("/plans", internalH.Plans)             // CAT-I2
}
