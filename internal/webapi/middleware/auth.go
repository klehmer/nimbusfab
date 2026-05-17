// Package middleware composes HTTP middlewares used across the webapi
// router. Auth Phase 1 replaces the env-var BearerToken stub with the
// real Auth middleware that supports cookie sessions, argon2-hashed
// PATs, and a disabled-mode dev passthrough.
package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/auth"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// AuthMode selects how the Auth middleware handles incoming requests.
type AuthMode string

const (
	// AuthModeDisabled attaches a fixed dev user to every request and skips
	// all credential checks. For local dev only.
	AuthModeDisabled AuthMode = "disabled"
	// AuthModeLocal requires either a cookie session OR a Bearer PAT.
	AuthModeLocal AuthMode = "local"
)

// SessionCookieName is the cookie key under which the signed session lives.
const SessionCookieName = "nimbusfab_session"

// AuthConfig carries the middleware's dependencies.
type AuthConfig struct {
	Mode       AuthMode
	Repo       inventory.Repo
	SessionKey []byte
	// RedirectLogin, if set, sends unauthenticated UI requests to this
	// path (default /auth/login). Empty disables redirect — always 401.
	RedirectLogin string
}

// ctxKey is a private key type to avoid context-value collisions.
type ctxKey string

const (
	userCtxKey ctxKey = "nfb.user"
)

// devUser is what AuthModeDisabled attaches.
var devUser = &inventory.User{
	ID:    "dev",
	OrgID: "default",
	Email: "dev@localhost",
}

// Auth returns a middleware factory. apiOnly=true means failed auth
// returns 401 JSON (for /api/v1/*); false means UI routes redirect to
// /auth/login when unauthenticated.
func Auth(cfg AuthConfig, apiOnly bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Mode == AuthModeDisabled {
				ctx := context.WithValue(r.Context(), userCtxKey, devUser)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			u, err := resolveUser(r, cfg)
			if err != nil || u == nil {
				rejectUnauthenticated(w, r, apiOnly, cfg.RedirectLogin)
				return
			}
			ctx := context.WithValue(r.Context(), userCtxKey, u)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext returns the authenticated user attached by Auth, or nil
// if none. Handlers use this to scope queries by orgID / userID.
func UserFromContext(ctx context.Context) *inventory.User {
	if v, ok := ctx.Value(userCtxKey).(*inventory.User); ok {
		return v
	}
	return nil
}

// resolveUser tries cookie session first, then Bearer PAT. Returns
// (nil, nil) when no credentials present (caller treats as 401/redirect).
func resolveUser(r *http.Request, cfg AuthConfig) (*inventory.User, error) {
	// Cookie session.
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		sess, err := auth.VerifySession(cfg.SessionKey, c.Value)
		if err == nil {
			u, _ := cfg.Repo.Users().Get(r.Context(), sess.OrgID, sess.UserID)
			return u, nil
		}
	}
	// Bearer PAT.
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		token := header[len("Bearer "):]
		prefix, secret, ok := auth.ParsePAT(token)
		if !ok {
			return nil, nil
		}
		row, err := cfg.Repo.ApiTokens().GetByPrefix(r.Context(), prefix)
		if err != nil || row == nil {
			return nil, nil
		}
		if !auth.VerifyPAT(secret, row.TokenHash) {
			return nil, nil
		}
		// Fire-and-forget last-used update; non-fatal.
		go func(id string) {
			_ = cfg.Repo.ApiTokens().UpdateLastUsed(context.Background(), id, time.Now().UTC())
		}(row.ID)
		u, _ := cfg.Repo.Users().Get(r.Context(), row.OrgID, row.UserID)
		return u, nil
	}
	return nil, nil
}

func rejectUnauthenticated(w http.ResponseWriter, r *http.Request, apiOnly bool, redirect string) {
	if apiOnly || redirect == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"ErrUnauthorized","message":"authentication required"}}`))
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// BearerToken stays exported temporarily for the existing router tests
// that haven't migrated yet. Now a thin wrapper around the legacy shape.
// New code should call Auth directly.
//
// Deprecated: use Auth.
func BearerToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h != "Bearer "+token {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"ErrUnauthorized","message":"invalid bearer token"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
