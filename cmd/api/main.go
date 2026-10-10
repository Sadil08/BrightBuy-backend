// Command api is the BrightBuy backend's entry point and composition root.
//
// "Composition root" means: this is the ONE file in the whole codebase allowed to know about
// every concrete type — every repository, every service, every handler. Everywhere else in the
// code depends on interfaces; this file is where those interfaces get their real implementations
// wired in (specs/global/06_ENGINEERING_STANDARDS.md §3). As features are added, this file grows
// a few lines per feature — it constructs the feature's repository, service, and handler, and
// registers its routes — but the pattern never changes.
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	cartapp "brightbuy-backend/internal/cart/app"
	carthttp "brightbuy-backend/internal/cart/httpapi"
	cartmysql "brightbuy-backend/internal/cart/mysql"
	catalogapp "brightbuy-backend/internal/catalog/app"
	cataloghttp "brightbuy-backend/internal/catalog/httpapi"
	catalogmysql "brightbuy-backend/internal/catalog/mysql"
	identityapp "brightbuy-backend/internal/identity/app"
	identityhttp "brightbuy-backend/internal/identity/httpapi"
	identitymysql "brightbuy-backend/internal/identity/mysql"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/config"
	"brightbuy-backend/internal/shared/dbx"
	"brightbuy-backend/internal/shared/logging"
	"brightbuy-backend/internal/shared/ratelimit"

	orderapp "brightbuy-backend/internal/ordering/app"
	orderhttp "brightbuy-backend/internal/ordering/httpapi"
	ordermysql "brightbuy-backend/internal/ordering/mysql"
	"brightbuy-backend/internal/payment"

	inventoryapp "brightbuy-backend/internal/inventory/app"
	inventoryhttp "brightbuy-backend/internal/inventory/httpapi"
	inventorymysql "brightbuy-backend/internal/inventory/mysql"
	reportingapp "brightbuy-backend/internal/reporting/app"
	reportinghttp "brightbuy-backend/internal/reporting/httpapi"
	reportingmysql "brightbuy-backend/internal/reporting/mysql"

	deliveryapp "brightbuy-backend/internal/delivery/app"
	deliveryhttp "brightbuy-backend/internal/delivery/httpapi"
	deliverymysql "brightbuy-backend/internal/delivery/mysql"
)

func main() {
	// 1. Load configuration. If anything required is missing, fail immediately and loudly —
	// before we've opened a socket or a database connection, while the failure is still simple
	// to diagnose from a single log line.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	// 2. Open the database connection pool. Also fails fast: a backend instance that can't reach
	// MySQL should never accept traffic in the first place.
	db, err := dbx.Open(cfg.DatabaseDSN)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 2a. --create-first-admin (plan.md §2.2) is a one-off CLI path, not a server boot — checked
	// here, right after the database is reachable but before anything else starts, and the process
	// exits immediately afterward either way. There's no safe HTTP shape for "create the first admin
	// with no existing admin to authorize it," so this never becomes a route.
	if len(os.Args) > 1 && os.Args[1] == "--create-first-admin" {
		if err := createFirstAdmin(context.Background(), db, logger); err != nil {
			logger.Error("create-first-admin failed", "error", err)
			os.Exit(1)
		}
		return
	}

	// 3. Build the router and attach middleware. chi.Router is just an http.Handler with routing
	// sugar on top — nothing here is chi-specific magic, it's the same net/http you'd write by
	// hand, with less boilerplate.
	r := chi.NewRouter()
	r.Use(middleware.RequestID) // gives every request a correlation ID (specs/global/06 §5 error handling)
	r.Use(middleware.Logger)    // logs method, path, status, latency for every request
	r.Use(middleware.Recoverer) // converts a panic into a 500 instead of crashing the process

	// Deliberately NOT using chi's middleware.RealIP: it trusts the client-supplied
	// X-Forwarded-For/X-Real-IP header, which is spoofable unless a trusted reverse proxy is
	// stripping and re-setting it first (we don't have one configured yet — that lands with
	// deployment, specs/global/12_DEVOPS_CICD.md §3). Using r.RemoteAddr directly for now, which
	// reflects the actual TCP connection and cannot be spoofed by a request header. This matters
	// concretely for 02-auth's per-IP rate limiting (specs/global/02_SECURITY_BASELINE.md §4) —
	// trusting a spoofable header there would make the rate limiter trivially bypassable.
	// Revisit once a reverse proxy is in place: re-enable RealIP configured with
	// middleware.WithTrustedProxies() pointed at that proxy's actual address, or set it manually
	// from a header the proxy itself controls.

	// Health endpoints (specs/global/12_DEVOPS_CICD.md §1.1):
	//   /healthz = "is the process up" (liveness) — no dependency check.
	//   /readyz  = "can this instance actually serve traffic" (readiness) — pings the database.
	// A container orchestrator uses these to decide whether to route traffic to this instance,
	// and whether to restart it if it's stuck.
	r.Get("/healthz", handleLiveness)
	r.Get("/readyz", handleReadiness(db))

	// 01-catalog — first feature module wired in. The composition root always builds bottom-up:
	// repository (talks to MySQL) -> service (business logic, knows nothing about SQL or HTTP) ->
	// handler (knows nothing about SQL, only calls the service) -> routes registered on the router.
	productRepo := catalogmysql.NewProductRepository(db)
	categoryRepo := catalogmysql.NewCategoryRepository(db)
	catalogService := catalogapp.NewCatalogService(productRepo, categoryRepo)
	catalogHandler := cataloghttp.NewCatalogHandler(catalogService).WithImageBaseURL(cfg.S3PublicURL)

	// r.Route groups a set of routes under a shared path prefix ("/api/v1") without those routes
	// needing to know that prefix exists — RegisterRoutes itself just registers "/categories",
	// "/products", etc., exactly as plan.md §3 lists them; the final path a client actually requests
	// (/api/v1/categories) is assembled here, in the one place that's allowed to care about URL
	// structure across the whole API.
	// 02-auth — second feature module. tokenIssuer is constructed once here and handed to BOTH
	// identity (to issue tokens at login/refresh) and shared/auth's own Authenticate middleware (to
	// verify them) — one signing key, one place it's read from config, never duplicated.
	tokenIssuer := auth.NewTokenIssuer(cfg.JWTSigningKey)

	userRepo := identitymysql.NewUserRepository(db)
	roleRepo := identitymysql.NewRoleRepository(db)
	refreshTokenRepo := identitymysql.NewRefreshTokenRepository(db)
	authService := identityapp.NewAuthService(userRepo, roleRepo, refreshTokenRepo, tokenIssuer)
	accountService := identityapp.NewAccountService(userRepo, roleRepo)

	// cookieSecure gates the Secure flag on session cookies (shared/auth.SetAuthCookies): true in
	// staging/production (served over HTTPS, where Secure is required and harmless), false for local
	// dev — a browser silently DROPS a Secure cookie sent over plain HTTP, which would make login
	// simply not work on a laptop running `go run ./cmd/api` directly.
	cookieSecure := cfg.Env != "local"

	// Rate limits (specs/global/02_SECURITY_BASELINE.md §4): concrete numbers this project's own
	// choice, since neither spec document names one. 5/minute per IP for login (credential
	// stuffing), 5/minute per submitted email (protects one targeted account from a distributed
	// attacker rotating IPs), 3/hour per IP for registration (bulk fake-account creation).
	loginIPLimiter := ratelimit.New(5.0/60.0, 5)
	loginEmailLimiter := ratelimit.New(5.0/60.0, 5)
	registerIPLimiter := ratelimit.New(3.0/3600.0, 3)

	authHandler := identityhttp.NewAuthHandler(authService, cookieSecure, loginEmailLimiter)
	adminHandler := identityhttp.NewAdminHandler(accountService)

	// 03-cart — third feature module, same bottom-up wiring as above.
	cartRepo := cartmysql.NewCartRepository(db)
	cartService := cartapp.NewService(cartRepo, catalogService)
	cartHandler := carthttp.NewHandler(cartService)

	// 04-checkout-orders — depends on cart's public service (to read the cart and clear it in the
	// order's own transaction) and on the payment port, whose Phase 1 implementation is a stub.
	orderRepo := ordermysql.NewOrderRepository(db)
	checkoutService := orderapp.NewCheckoutService(
		cartService,
		orderRepo,
		payment.StubProcessor{},
		func(ctx context.Context, fn func(*sql.Tx) error) error {
			return dbx.WithTx(ctx, db, fn)
		},
	)
	orderHandler := orderhttp.NewHandler(checkoutService)
	paymentRepo := ordermysql.NewPaymentRepository()
	orderStatusService := orderapp.NewOrderStatusService(
		orderRepo,
		paymentRepo,
		func(ctx context.Context, fn func(*sql.Tx) error) error {
			return dbx.WithTx(ctx, db, fn)
		},
	)
	staffOrderHandler := orderhttp.NewStaffHandler(orderStatusService).WithQueries(orderapp.NewStaffOrderQueries(orderRepo))

	r.Route("/api/v1", func(apiRouter chi.Router) {
		cataloghttp.RegisterRoutes(apiRouter, catalogHandler)
		identityhttp.RegisterRoutes(apiRouter, authHandler, adminHandler, tokenIssuer, registerIPLimiter, loginIPLimiter)
		carthttp.RegisterRoutes(apiRouter, cartHandler, tokenIssuer)
		orderhttp.RegisterRoutes(apiRouter, orderHandler, tokenIssuer)
		orderhttp.RegisterStaffRoutes(apiRouter, staffOrderHandler, tokenIssuer)
	})

	deliveryRepository := deliverymysql.NewRepository(db)
	deliveryService := deliveryapp.NewService(deliveryRepository, deliveryRepository, time.Now)
	deliveryHandler := deliveryhttp.NewHandler(deliveryService)

	deliveryhttp.RegisterRoutes(r, deliveryHandler)

	// 06-inventory-stock — staff-only. Authenticate runs first (a missing/invalid session is a 401);
	// RequirePermission alone would answer 403 for an unauthenticated caller. The permission is a
	// code ("stock:adjust"), granted to WAREHOUSE_STAFF by migration 0004 — never a role name here
	// (SEC-INVENTORY-1). The acting user recorded in stock_movement comes from the verified JWT.
	inventoryRepository := inventorymysql.NewRepository(db)
	inventoryService := inventoryapp.NewService(inventoryRepository)
	inventoryHandler := inventoryhttp.NewHandler(inventoryService, func(ctx context.Context) (int, bool) {
		claims := auth.ClaimsFromContext(ctx)
		if claims == nil {
			return 0, false
		}
		return claims.UserID, true
	})
	requireStockAdjust := func(next http.Handler) http.Handler {
		return auth.Authenticate(tokenIssuer)(auth.RequirePermission("stock:adjust")(next))
	}
	inventoryhttp.RegisterRoutes(r, inventoryHandler, requireStockAdjust)

	// 07-admin-catalog — staff catalogue management and image administration.
	catalogAdminRepository := catalogmysql.NewRepository(db)
	catalogAdminService := catalogapp.NewCatalogAdminService(catalogAdminRepository, nil)
	cataloghttp.RegisterAdminRoutes(r, cataloghttp.NewAdminHandler(catalogAdminService), guardPermission(tokenIssuer, "catalog:write"))
	if cfg.S3EndpointURL != "" {
		imageStorage, err := catalogmysql.NewImageStorage(cfg.S3EndpointURL, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket)
		if err != nil {
			logger.Error("image storage setup failed", "error", err)
			os.Exit(1)
		}
		imageRepository := catalogmysql.NewImageRepository(db)
		imageService := catalogapp.NewImageService(imageStorage, imageRepository)
		cataloghttp.RegisterImageRoutes(r, cataloghttp.NewImageHandler(imageService).WithBaseURL(cfg.S3PublicURL), guardPermission(tokenIssuer, "catalog:image:write"))
	}

	// 09-management-reporting — read-only views behind reports:view (MANAGER). With
	// REPORTING_DB_DSN set it uses its own narrow brightbuy_reporting pool (SEC-REPORTING-1).
	var reportingRepository *reportingmysql.Repository
	if cfg.ReportingDSN != "" {
		reportingRepository, err = reportingmysql.NewRepository(cfg.ReportingDSN)
		if err != nil {
			logger.Error("reporting database setup failed", "error", err)
			os.Exit(1)
		}
		defer reportingRepository.Close()
	} else {
		reportingRepository = reportingmysql.NewRepositoryFromDB(db)
	}
	requireReportsView := func(next http.Handler) http.Handler {
		return auth.Authenticate(tokenIssuer)(auth.RequirePermission("reports:view")(next))
	}
	reportinghttp.RegisterRoutes(r, reportinghttp.NewHandler(reportingapp.NewService(reportingRepository)), requireReportsView)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second, // mitigates slow-header (Slowloris-style) connections
	}

	// 4. Start serving in a background goroutine so the main goroutine is free to wait for a
	// shutdown signal below. If the server fails to start (e.g. the port is already in use),
	// that's fatal — log it and exit.
	go func() {
		logger.Info("server starting", "addr", cfg.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// 5. Wait for SIGINT (Ctrl+C locally) or SIGTERM (what a container orchestrator sends before
	// killing a replaced instance during a deploy). On either signal, stop accepting new
	// connections but let in-flight requests finish — that's what makes a deploy zero-downtime
	// rather than dropping whoever happened to be mid-request (specs/global/12_DEVOPS_CICD.md §5).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutdown signal received, draining connections")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown did not complete cleanly", "error", err)
	}
	logger.Info("shutdown complete")
}

// handleLiveness always answers 200 if the process is running at all — it deliberately checks
// nothing else. A liveness probe that checks the database would cause an orchestrator to kill and
// restart a perfectly healthy process just because the database had a momentary blip; that's the
// job of the readiness probe instead.
func handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReadiness answers whether THIS instance can currently serve traffic, by pinging the
// database. Returns a plain http.HandlerFunc via a closure over db, so main() doesn't need a
// package-level variable just to get the database handle into this function.
func handleReadiness(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unreachable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// guardPermission composes Authenticate (401 without a valid session) with RequirePermission (403
// without the permission code) — the order matters, see internal/shared/auth/middleware.go.
func guardPermission(tokens *auth.TokenIssuer, code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return auth.Authenticate(tokens)(auth.RequirePermission(code)(next))
	}
}
