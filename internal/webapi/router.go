// Package webapi composes the HTTP surface of nimbusfab-server. UI Phase 1
// mounts a read-only HTML UI at /ui/* plus /assets/*, /healthz, /readyz,
// and a / → /ui/projects redirect.
package webapi

import (
	"fmt"
	"net/http"

	"github.com/klehmer/nimbusfab/internal/webapi/ui"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// Config carries the dependencies the router wires together.
type Config struct {
	Repo  inventory.Repo
	OrgID string // until Auth Phase 1; "default" in disabled-auth mode
}

// New returns an http.Handler mounting all UI Phase 1 routes.
func New(cfg Config) (http.Handler, error) {
	if cfg.Repo == nil {
		return nil, fmt.Errorf("webapi: Config.Repo is required")
	}
	if cfg.OrgID == "" {
		cfg.OrgID = "default"
	}
	renderer, err := ui.NewRenderer(cfg.Repo, cfg.OrgID)
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

	return mux, nil
}
