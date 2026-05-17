// Package ui implements server-rendered HTML pages for the nimbusfab web
// app. UI Phase 1 ships read-only views over the inventory repo; mutating
// actions land in HTTP Phase 2 / UI Phase 2.
package ui

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/middleware"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed assets
var assetsFS embed.FS

// AssetsFS returns the embedded /assets/* sub-filesystem suitable for
// http.FileServerFS.
func AssetsFS() (fs.FS, error) {
	return fs.Sub(assetsFS, "assets")
}

// Renderer holds parsed templates and the engine deps page handlers need.
// Each page name maps to its own *template.Template instance to avoid
// {{define "content"}} collisions across pages (html/template namespaces
// all defines into one set per Template, so distinct sets per page is
// the cleanest isolation).
type Renderer struct {
	Repo  inventory.Repo
	OrgID string
	// APIToken, if non-empty, is rendered into the deployment-detail page's
	// <script data-api-token="..."> attribute so the JS can authenticate
	// /api/v1/* mutating calls. Empty in dev mode. Auth Phase 1 replaces
	// this with cookie sessions that the browser sends automatically.
	APIToken string

	pages map[string]*template.Template
}

// NewRenderer parses each page template alongside layout.html into its
// own Template instance.
func NewRenderer(repo inventory.Repo, orgID, apiToken string) (*Renderer, error) {
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("ui: read templates dir: %w", err)
	}
	pages := map[string]*template.Template{}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "layout.html" {
			continue
		}
		t, err := template.New(e.Name()).Funcs(funcMap()).ParseFS(templatesFS, "templates/layout.html", "templates/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("ui: parse %s: %w", e.Name(), err)
		}
		pages[e.Name()] = t
	}
	return &Renderer{Repo: repo, OrgID: orgID, APIToken: apiToken, pages: pages}, nil
}

// render writes one page by executing its "layout" entry point.
func (r *Renderer) render(w http.ResponseWriter, page string, data any) {
	t, ok := r.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// withUser augments the data map with CurrentUser pulled from the request
// context (attached by the Auth middleware). Handlers that want the nav
// to show "logged in as ..." pass through this helper before render().
func (r *Renderer) withUser(req *http.Request, data map[string]any) map[string]any {
	if u := middleware.UserFromContext(req.Context()); u != nil {
		data["CurrentUser"] = u
	}
	return data
}

func (r *Renderer) renderError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	r.render(w, "error.html", map[string]any{
		"Status":  status,
		"Message": msg,
	})
}

// ListProjects renders the projects table.
func (r *Renderer) ListProjects(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	projects, err := r.Repo.Projects().List(ctx, r.OrgID)
	if err != nil {
		r.renderError(w, http.StatusInternalServerError, "list projects: "+err.Error())
		return
	}
	r.render(w, "projects.html", r.withUser(req, map[string]any{"Projects": projects}))
}

// TargetWithRuns bundles a deployment target with its run history for the
// deployment_detail template.
type TargetWithRuns struct {
	T    inventory.DeploymentTarget
	Runs []inventory.Run
}

// CostSummary is the per-deployment cost rollup the deployment detail page
// renders. HasData is false when no cost estimates are persisted (network-
// only components or pre-Dashboards-Phase-1 deployments).
type CostSummary struct {
	HasData  bool
	Currency string
	Total    float64
	Targets  []TargetCostSummary
}

// TargetCostSummary is one deployment-target's cost rollup.
type TargetCostSummary struct {
	ComponentName string
	Cloud         string
	Region        string
	Subtotal      float64
}

// DeploymentDetail renders one deployment plus its per-target rows + runs.
func (r *Renderer) DeploymentDetail(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	id := req.PathValue("id")
	d, err := r.Repo.Deployments().Get(ctx, r.OrgID, id)
	if err != nil || d == nil {
		r.renderError(w, http.StatusNotFound, "deployment not found: "+id)
		return
	}
	targets, _ := r.Repo.DeploymentTargets().ListByDeployment(ctx, r.OrgID, id)
	enriched := make([]TargetWithRuns, 0, len(targets))
	for _, t := range targets {
		runs, _ := r.Repo.Runs().ListByDeploymentTarget(ctx, r.OrgID, t.ID)
		enriched = append(enriched, TargetWithRuns{T: t, Runs: runs})
	}
	costSummary := r.buildCostSummary(ctx, id, targets)
	r.render(w, "deployment_detail.html", r.withUser(req, map[string]any{
		"Deployment":  d,
		"Targets":     enriched,
		"APIToken":    r.APIToken,
		"CostSummary": costSummary,
	}))
}

// RunDetail renders one tofu run.
func (r *Renderer) RunDetail(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	id := req.PathValue("id")
	run, err := r.Repo.Runs().Get(ctx, r.OrgID, id)
	if err != nil || run == nil {
		r.renderError(w, http.StatusNotFound, "run not found: "+id)
		return
	}
	r.render(w, "run_detail.html", r.withUser(req, map[string]any{
		"Run": run,
	}))
}

// DriftOverview is the row shape the drift.html template iterates over.
type DriftOverview struct {
	DeploymentTargetID string
	DeploymentID       string
	ComponentName      string
	Cloud              string
	Region             string
	HasDrift           bool
	DetectedAt         time.Time
}

// DriftSummary captures the per-org rollup the drift page renders.
type DriftSummary struct {
	Total   int
	Drifted int
	Clean   int
}

// LoginPage renders the login form. The middleware exempts this route.
func (r *Renderer) LoginPage(w http.ResponseWriter, req *http.Request) {
	data := map[string]any{
		"Error": req.URL.Query().Get("error") != "",
	}
	r.render(w, "login.html", data)
}

// Drift renders the org-wide drift overview page.
func (r *Renderer) Drift(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	records, err := r.Repo.DriftStatus().ListByOrg(ctx, r.OrgID)
	if err != nil {
		r.renderError(w, http.StatusInternalServerError, "list drift: "+err.Error())
		return
	}
	out := make([]DriftOverview, 0, len(records))
	drifted := 0
	for _, rec := range records {
		t, _ := r.Repo.DeploymentTargets().Get(ctx, r.OrgID, rec.DeploymentTargetID)
		if t == nil {
			continue
		}
		if rec.HasDrift {
			drifted++
		}
		out = append(out, DriftOverview{
			DeploymentTargetID: rec.DeploymentTargetID,
			DeploymentID:       t.DeploymentID,
			ComponentName:      t.ComponentName,
			Cloud:              t.Cloud,
			Region:             t.Region,
			HasDrift:           rec.HasDrift,
			DetectedAt:         rec.DetectedAt,
		})
	}
	r.render(w, "drift.html", r.withUser(req, map[string]any{
		"HasData": len(out) > 0,
		"Records": out,
		"Summary": DriftSummary{Total: len(out), Drifted: drifted, Clean: len(out) - drifted},
	}))
}

// ProjectDetail renders the per-project page: stacks, components, recent deployments.
func (r *Renderer) ProjectDetail(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	id := req.PathValue("id")
	p, err := r.Repo.Projects().Get(ctx, r.OrgID, id)
	if err != nil || p == nil {
		r.renderError(w, http.StatusNotFound, "project not found: "+id)
		return
	}
	stacks, _ := r.Repo.Stacks().List(ctx, r.OrgID, id)
	var allComponents []inventory.Component
	for _, s := range stacks {
		comps, _ := r.Repo.Components().ListByStack(ctx, r.OrgID, id, s.ID)
		allComponents = append(allComponents, comps...)
	}
	deployments, _ := r.Repo.Deployments().ListByProject(ctx, r.OrgID, id, 20)
	r.render(w, "project_detail.html", r.withUser(req, map[string]any{
		"Project":     p,
		"Stacks":      stacks,
		"Components":  allComponents,
		"Deployments": deployments,
	}))
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"humanStatus": humanStatus,
		"statusBadge": statusBadge,
		"shortID": func(id string) string {
			if len(id) > 14 {
				return id[:14] + "…"
			}
			return id
		},
		"defaultStr": func(s, def string) string {
			if s == "" {
				return def
			}
			return s
		},
	}
}

func humanStatus(s string) string {
	switch s {
	case "succeeded":
		return "succeeded"
	case "failed":
		return "failed"
	case "partial_failure":
		return "partial failure"
	case "planned":
		return "planned"
	case "running":
		return "running"
	}
	return s
}

// statusBadge returns the CSS class for a status badge.
func statusBadge(s string) string {
	switch s {
	case "succeeded":
		return "ok"
	case "failed":
		return "fail"
	case "partial_failure":
		return "warn"
	}
	return ""
}

// buildCostSummary aggregates cost_estimates for the deployment into a
// per-target rollup. HasData is false when no estimates are persisted.
func (r *Renderer) buildCostSummary(ctx context.Context, deploymentID string, targets []inventory.DeploymentTarget) CostSummary {
	rows, err := r.Repo.CostEstimates().ListByDeployment(ctx, r.OrgID, deploymentID)
	if err != nil || len(rows) == 0 {
		return CostSummary{}
	}
	// Build run-id → target lookup.
	runToTarget := map[string]inventory.DeploymentTarget{}
	for _, t := range targets {
		runs, _ := r.Repo.Runs().ListByDeploymentTarget(ctx, r.OrgID, t.ID)
		for _, run := range runs {
			runToTarget[run.ID] = t
		}
	}
	subtotals := map[string]float64{}
	var currency string
	var total float64
	for _, row := range rows {
		t, ok := runToTarget[row.RunID]
		if !ok {
			continue
		}
		subtotals[t.ID] += row.Subtotal
		total += row.Subtotal
		if currency == "" {
			currency = row.Currency
		}
	}
	summary := CostSummary{HasData: true, Currency: currency, Total: total}
	for _, t := range targets {
		if sub, ok := subtotals[t.ID]; ok {
			summary.Targets = append(summary.Targets, TargetCostSummary{
				ComponentName: t.ComponentName,
				Cloud:         t.Cloud,
				Region:        t.Region,
				Subtotal:      sub,
			})
		}
	}
	return summary
}
