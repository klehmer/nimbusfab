// Package middleware composes HTTP middlewares used across the webapi
// router. UI Phase 1 ships only the bearer-token middleware (Phase-1 PAT
// stub); Auth Phase 1 will add cookie sessions, OIDC, and real per-user
// PATs with argon2 hashing.
package middleware

import (
	"net/http"
	"strings"
)

// BearerToken returns a middleware that rejects requests without
// "Authorization: Bearer <token>" matching the configured token. If the
// configured token is empty, the middleware is a no-op — useful for local
// dev / when running with NIMBUSFAB_AUTH_MODE=disabled.
//
// Phase-1 stub: one shared token, no per-user identity. Real PATs land
// in Auth Phase 1 (per-user, hashed, expirable).
func BearerToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				writeAPIError(w, http.StatusUnauthorized, "ErrUnauthorized", "Authorization header missing or malformed")
				return
			}
			if header[len(prefix):] != token {
				writeAPIError(w, http.StatusUnauthorized, "ErrUnauthorized", "invalid bearer token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeAPIError writes the standard JSON error envelope.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}
