package httpapi

import (
	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/ratelimit"
)

// RegisterRoutes wires every /auth/* and /admin/* endpoint, including which middleware gates which
// route — this is the one place that decides "public," "needs a valid access token," or "needs a
// specific permission" for this entire module, matching plan.md §3's table exactly.
//
// registerLimiter/loginIPLimiter are IP-keyed (ratelimit.Middleware, checked before the handler ever
// runs); the login endpoint's EMAIL-keyed limiter is checked inside AuthHandler.Login itself, since
// that key only exists after the request body is decoded — see AuthHandler's own doc comment.
func RegisterRoutes(r chi.Router, authHandler *AuthHandler, adminHandler *AdminHandler, tokens *auth.TokenIssuer, registerLimiter, loginIPLimiter *ratelimit.Limiter) {
	r.Route("/auth", func(r chi.Router) {
		r.With(registerLimiter.Middleware(ratelimit.ByIP)).Post("/register", authHandler.Register)
		r.With(loginIPLimiter.Middleware(ratelimit.ByIP)).Post("/login", authHandler.Login)
		r.Post("/refresh", authHandler.Refresh) // no Authenticate — see Refresh's own comment

		r.Group(func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Post("/logout", authHandler.Logout)
			r.Get("/me", authHandler.Me)
		})
	})

	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.Authenticate(tokens)) // every /admin/** route requires SOME valid session first

		r.With(auth.RequirePermission("account:manage_users")).Get("/users", adminHandler.ListUsers)
		r.With(auth.RequirePermission("account:manage_users")).Post("/users", adminHandler.CreateAccount)

		r.With(auth.RequirePermission("account:manage_roles")).Get("/roles", adminHandler.ListRoles)
		r.With(auth.RequirePermission("account:manage_roles")).Get("/permissions", adminHandler.ListPermissions)
		r.With(auth.RequirePermission("account:manage_roles")).Put("/roles/{roleId}/permissions", adminHandler.SetRolePermissions)
	})
}
