package auth

import (
	"context"
	"net/http"
)

type permissionsKey struct{}

func WithPermissions(ctx context.Context, permissions ...string) context.Context {
	set := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		set[permission] = struct{}{}
	}
	return context.WithValue(ctx, permissionsKey{}, set)
}

func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			permissions, _ := r.Context().Value(permissionsKey{}).(map[string]struct{})
			if _, ok := permissions[permission]; !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"code":"FORBIDDEN","message":"permission required"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
