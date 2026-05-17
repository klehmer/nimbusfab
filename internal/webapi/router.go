// Package webapi composes the HTTP surface of nimbusfab-server. UI Phase 1
// mounts a read-only HTML UI at /ui/* plus /assets/*, /healthz, /readyz,
// and a / → /ui/projects redirect. HTTP Phase 1 adds JSON GETs under
// /api/v1/* with optional bearer-token auth.
package webapi

import (
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
	Repo     inventory.Repo
	OrgID    string // until Auth Phase 1; "default" in disabled-auth mode
	APIToken string // optional; empty = no auth on /api/v1/* (dev mode)
	// Engine is required for HTTP Phase 2 mutating endpoints + SSE. When
	// nil, /applies, /destroys, /drifts, and /events routes are not
	// mounted; the read-only API + UI still work.
	Engine engine.Engine
}

// New returns an http.Handler mounting all UI Phase 1 routes.
func New(cfg Config) (http.Handler, error) {
	if cfg.Repo == nil {
		return nil, fmt.Errorf("webapi: Config.Repo is required")
	}
	if cfg.OrgID == "" {
		cfg.OrgID = "default"
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
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		// Phase 1 readiness: just confirm the repo accessor returns.
		// Real readiness (DB ping, OIDC discovery, etc.) lands later.
		if cfg.Repo.Orgs() != nil {
			_, _ = w.Write([]byte("ready"))
			return
		}
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/projects", http.StatusFound)
	})
	mux.HandleFunc("GET /ui/projects", renderer.ListProjects)
	mux.HandleFunc("GET /ui/projects/{id}", renderer.ProjectDetail)
	mux.HandleFunc("GET /ui/deployments/{id}", renderer.DeploymentDetail)
	mux.HandleFunc("GET /ui/runs/{id}", renderer.RunDetail)

	// /api/v1/* JSON endpoints. Registered individually so the GET-method
	// patterns don't conflict with the root "GET /" redirect (Go's
	// ServeMux refuses ambiguous overlaps between path-only and
	// method-specific patterns). Bearer-token middleware wraps each
	// handler so UI routes stay unauthenticated in Phase 1.
	apiHandlers := &api.Handlers{Repo: cfg.Repo, OrgID: cfg.OrgID}
	apiAuth := middleware.BearerToken(cfg.APIToken)
	mux.Handle("GET /api/v1/projects", apiAuth(http.HandlerFunc(apiHandlers.ListProjects)))
	mux.Handle("GET /api/v1/projects/{id}", apiAuth(http.HandlerFunc(apiHandlers.GetProject)))
	mux.Handle("GET /api/v1/deployments/{id}", apiAuth(http.HandlerFunc(apiHandlers.GetDeployment)))
	mux.Handle("GET /api/v1/runs/{id}", apiAuth(http.HandlerFunc(apiHandlers.GetRun)))
	mux.Handle("GET /api/v1/deployments/{id}/costs", apiAuth(http.HandlerFunc(apiHandlers.GetDeploymentCosts)))

	// HTTP Phase 2: mutating endpoints + SSE. Mounted only when an Engine
	// is configured so test setups without an engine still work.
	if cfg.Engine != nil {
		broker := runner.NewRunBroker(64)
		mutations := &api.Mutations{Engine: cfg.Engine, Broker: broker, OrgID: cfg.OrgID}
		sse := &api.SSEEvents{Broker: broker}
		mux.Handle("POST /api/v1/deployments/{id}/applies", apiAuth(http.HandlerFunc(mutations.PostApply)))
		mux.Handle("POST /api/v1/deployments/{id}/destroys", apiAuth(http.HandlerFunc(mutations.PostDestroy)))
		mux.Handle("POST /api/v1/deployments/{id}/drifts", apiAuth(http.HandlerFunc(mutations.PostDrift)))
		mux.Handle("GET /api/v1/deployments/{id}/events", apiAuth(http.HandlerFunc(sse.Handle)))
	}

	return mux, nil
}
