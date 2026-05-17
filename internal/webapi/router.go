// Package webapi composes the HTTP surface of nimbusfab-server. Auth
// Phase 1 mounts cookie-session + PAT auth via middleware.Auth; UI Phase
// 1/2 + HTTP Phase 1/2 mount the actual handlers under that.
package webapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/klehmer/nimbusfab/internal/webapi/api"
	"github.com/klehmer/nimbusfab/internal/webapi/middleware"
	"github.com/klehmer/nimbusfab/internal/webapi/runner"
	"github.com/klehmer/nimbusfab/internal/webapi/ui"
	"github.com/klehmer/nimbusfab/pkg/engine"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// Config carries the dependencies the router wires together.
type Config struct {
	Repo  inventory.Repo
	OrgID string
	// AuthMode selects the auth flow: "disabled" attaches a dev user; "local"
	// requires cookie session or Bearer PAT. Default is "disabled" for
	// backward compatibility with pre-Auth-Phase-1 deployments.
	AuthMode middleware.AuthMode
	// SessionKey signs cookie sessions; ≥16 bytes. Required when AuthMode
	// != disabled.
	SessionKey []byte
	// APIToken is the legacy env-var bearer token. Phased out by Auth
	// Phase 1's PAT support but kept here for one release cycle so
	// existing deployments don't break on upgrade.
	APIToken string
	// CookieSecure: true → set Secure cookie flag (production HTTPS).
	CookieSecure bool
	// Engine is required for HTTP Phase 2 mutating endpoints + SSE.
	Engine engine.Engine
}

// New returns an http.Handler with all routes mounted.
func New(cfg Config) (http.Handler, error) {
	if cfg.Repo == nil {
		return nil, fmt.Errorf("webapi: Config.Repo is required")
	}
	if cfg.OrgID == "" {
		cfg.OrgID = "default"
	}
	if cfg.AuthMode == "" {
		cfg.AuthMode = middleware.AuthModeDisabled
	}
	renderer, err := ui.NewRenderer(cfg.Repo, cfg.OrgID, cfg.APIToken)
	if err != nil {
		return nil, err
	}
	assets, err := ui.AssetsFS()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	// Public routes (no auth): static assets, health checks, auth pages,
	// root redirect.
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		// Real readiness: ping the inventory DB if the repo exposes a Ping
		// method (sqlite + postgres both do). Repos without Ping fall back
		// to the accessor-returns check for backward compat with nullRepo.
		if p, ok := cfg.Repo.(interface {
			Ping(ctx context.Context) error
		}); ok {
			if err := p.Ping(r.Context()); err != nil {
				http.Error(w, "not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		} else if cfg.Repo.Orgs() == nil {
			http.Error(w, "not ready: repo Orgs accessor returns nil", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/projects", http.StatusFound)
	})
	mux.HandleFunc("GET /auth/login", renderer.LoginPage)

	// Auth handlers + auth API endpoints.
	authHandlers := &api.AuthHandlers{
		Repo:       cfg.Repo,
		OrgID:      cfg.OrgID,
		SessionKey: cfg.SessionKey,
		Secure:     cfg.CookieSecure,
	}
	mux.HandleFunc("POST /auth/login", authHandlers.LoginForm)
	mux.HandleFunc("POST /auth/logout", authHandlers.Logout)

	// Middleware factories. UI routes redirect to /auth/login when
	// unauthenticated; API routes return 401 JSON.
	uiAuth := middleware.Auth(middleware.AuthConfig{
		Mode: cfg.AuthMode, Repo: cfg.Repo, SessionKey: cfg.SessionKey,
		RedirectLogin: "/auth/login",
	}, false)
	apiAuth := middleware.Auth(middleware.AuthConfig{
		Mode: cfg.AuthMode, Repo: cfg.Repo, SessionKey: cfg.SessionKey,
	}, true)

	// UI routes (cookie session preferred; PAT also accepted).
	mux.Handle("GET /ui/projects", uiAuth(http.HandlerFunc(renderer.ListProjects)))
	mux.Handle("GET /ui/projects/{id}", uiAuth(http.HandlerFunc(renderer.ProjectDetail)))
	mux.Handle("GET /ui/deployments/{id}", uiAuth(http.HandlerFunc(renderer.DeploymentDetail)))
	mux.Handle("GET /ui/runs/{id}", uiAuth(http.HandlerFunc(renderer.RunDetail)))
	mux.Handle("GET /ui/drift", uiAuth(http.HandlerFunc(renderer.Drift)))

	// /api/v1/* JSON endpoints.
	apiHandlers := &api.Handlers{Repo: cfg.Repo, OrgID: cfg.OrgID}
	mux.Handle("GET /api/v1/projects", apiAuth(http.HandlerFunc(apiHandlers.ListProjects)))
	mux.Handle("GET /api/v1/projects/{id}", apiAuth(http.HandlerFunc(apiHandlers.GetProject)))
	mux.Handle("GET /api/v1/deployments/{id}", apiAuth(http.HandlerFunc(apiHandlers.GetDeployment)))
	mux.Handle("GET /api/v1/runs/{id}", apiAuth(http.HandlerFunc(apiHandlers.GetRun)))
	mux.Handle("GET /api/v1/deployments/{id}/costs", apiAuth(http.HandlerFunc(apiHandlers.GetDeploymentCosts)))
	mux.Handle("GET /api/v1/drift", apiAuth(http.HandlerFunc(apiHandlers.GetDrift)))
	mux.Handle("GET /api/v1/auth/me", apiAuth(http.HandlerFunc(authHandlers.Me)))

	// HTTP Phase 2: mutating endpoints + SSE. Audit middleware wraps the
	// mutating handlers so each successful call writes an audit_log row.
	if cfg.Engine != nil {
		broker := runner.NewRunBroker(64)
		mutations := &api.Mutations{Engine: cfg.Engine, Broker: broker, OrgID: cfg.OrgID}
		sse := &api.SSEEvents{Broker: broker}
		audit := middleware.AuditLog(cfg.Repo)
		mux.Handle("POST /api/v1/deployments/{id}/applies", apiAuth(audit("deployment.apply")(http.HandlerFunc(mutations.PostApply))))
		mux.Handle("POST /api/v1/deployments/{id}/destroys", apiAuth(audit("deployment.destroy")(http.HandlerFunc(mutations.PostDestroy))))
		mux.Handle("POST /api/v1/deployments/{id}/drifts", apiAuth(audit("deployment.drift")(http.HandlerFunc(mutations.PostDrift))))
		mux.Handle("GET /api/v1/deployments/{id}/events", apiAuth(http.HandlerFunc(sse.Handle)))
	}

	return mux, nil
}
