package api

import (
	"net/http"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/auth"
	"github.com/klehmer/nimbusfab/internal/webapi/middleware"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// AuthHandlers wires the auth API endpoints.
type AuthHandlers struct {
	Repo         inventory.Repo
	OrgID        string
	SessionKey   []byte
	SessionTTL   time.Duration
	CookieDomain string // optional
	Secure       bool   // true → set Secure cookie flag (production HTTPS)
}

// LoginForm → POST /auth/login (form-encoded: email + password). Redirects
// to /ui/projects on success, /auth/login?error=1 on failure.
func (h *AuthHandlers) LoginForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")
	if email == "" || password == "" {
		http.Redirect(w, r, "/auth/login?error=missing", http.StatusFound)
		return
	}
	u, err := h.Repo.Users().GetByEmail(r.Context(), h.OrgID, email)
	if err != nil || u == nil {
		http.Redirect(w, r, "/auth/login?error=invalid", http.StatusFound)
		return
	}
	if !auth.VerifyPassword(u.PasswordHash, password) {
		http.Redirect(w, r, "/auth/login?error=invalid", http.StatusFound)
		return
	}
	if err := h.setSessionCookie(w, *u); err != nil {
		http.Error(w, "session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/ui/projects", http.StatusFound)
}

// Logout → POST /auth/logout. Clears the cookie + redirects.
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

// Me → GET /auth/me. Returns the authenticated user as JSON.
func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "ErrUnauthorized", "no session")
		return
	}
	writeData(w, map[string]any{
		"userId":      u.ID,
		"orgId":       u.OrgID,
		"email":       u.Email,
		"displayName": u.DisplayName,
	})
}

func (h *AuthHandlers) setSessionCookie(w http.ResponseWriter, u inventory.User) error {
	ttl := h.SessionTTL
	if ttl == 0 {
		ttl = 12 * time.Hour
	}
	sess := auth.Session{
		UserID:    u.ID,
		OrgID:     u.OrgID,
		Email:     u.Email,
		ExpiresAt: time.Now().Add(ttl),
	}
	cookie, err := auth.SignSession(h.SessionKey, sess)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    cookie,
		Path:     "/",
		Domain:   h.CookieDomain,
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	return nil
}
