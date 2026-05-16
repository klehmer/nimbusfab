// Package api implements the JSON REST handlers for /api/v1/*. HTTP
// Phase 1 ships read-only GETs (projects, deployments, runs); mutating
// endpoints and SSE land in HTTP Phase 2.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// Handlers groups the JSON read handlers and their dependencies.
type Handlers struct {
	Repo  inventory.Repo
	OrgID string
}

// ListProjects → GET /api/v1/projects
func (h *Handlers) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.Repo.Projects().List(r.Context(), h.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ErrInventory", err.Error())
		return
	}
	writeData(w, map[string]any{"projects": projectsJSON(projects)})
}

// GetProject → GET /api/v1/projects/{id}
func (h *Handlers) GetProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := h.Repo.Projects().Get(r.Context(), h.OrgID, id)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "ErrNotFound", "project not found: "+id)
		return
	}
	stacks, _ := h.Repo.Stacks().List(r.Context(), h.OrgID, id)
	var components []inventory.Component
	for _, s := range stacks {
		cs, _ := h.Repo.Components().ListByStack(r.Context(), h.OrgID, id, s.ID)
		components = append(components, cs...)
	}
	deployments, _ := h.Repo.Deployments().ListByProject(r.Context(), h.OrgID, id, 20)
	writeData(w, map[string]any{
		"project":     projectJSON(*p),
		"stacks":      stacksJSON(stacks),
		"components":  componentsJSON(components),
		"deployments": deploymentsJSON(deployments),
	})
}

// GetDeployment → GET /api/v1/deployments/{id}
func (h *Handlers) GetDeployment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := h.Repo.Deployments().Get(r.Context(), h.OrgID, id)
	if err != nil || d == nil {
		writeError(w, http.StatusNotFound, "ErrNotFound", "deployment not found: "+id)
		return
	}
	targets, _ := h.Repo.DeploymentTargets().ListByDeployment(r.Context(), h.OrgID, id)
	writeData(w, map[string]any{
		"deployment": deploymentJSON(*d),
		"targets":    targetsJSON(targets),
	})
}

// GetRun → GET /api/v1/runs/{id}
func (h *Handlers) GetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.Repo.Runs().Get(r.Context(), h.OrgID, id)
	if err != nil || run == nil {
		writeError(w, http.StatusNotFound, "ErrNotFound", "run not found: "+id)
		return
	}
	writeData(w, map[string]any{"run": runJSON(*run)})
}

// --- JSON shape helpers ---
// Inventory Go types are converted to camelCase map[string]any so the API
// surface doesn't leak field names from the Go structs (lets us evolve the
// internal shape without breaking the wire format).

func projectsJSON(ps []inventory.Project) []map[string]any {
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, projectJSON(p))
	}
	return out
}

func projectJSON(p inventory.Project) map[string]any {
	return map[string]any{
		"id":        p.ID,
		"orgId":     p.OrgID,
		"name":      p.Name,
		"createdAt": p.CreatedAt.Format(time.RFC3339),
	}
}

func stacksJSON(ss []inventory.Stack) []map[string]any {
	out := make([]map[string]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, map[string]any{
			"id":               s.ID,
			"name":             s.Name,
			"projectId":        s.ProjectID,
			"stateBackendKind": s.StateBackendKind,
		})
	}
	return out
}

func componentsJSON(cs []inventory.Component) []map[string]any {
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{
			"id":        c.ID,
			"name":      c.Name,
			"type":      c.Type,
			"stackId":   c.StackID,
			"updatedAt": c.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func deploymentsJSON(ds []inventory.Deployment) []map[string]any {
	out := make([]map[string]any, 0, len(ds))
	for _, d := range ds {
		out = append(out, deploymentJSON(d))
	}
	return out
}

func deploymentJSON(d inventory.Deployment) map[string]any {
	m := map[string]any{
		"id":                   d.ID,
		"orgId":                d.OrgID,
		"projectId":            d.ProjectID,
		"stackId":              d.StackID,
		"status":               d.Status,
		"partialFailurePolicy": d.PartialFailurePolicy,
		"startedAt":            d.StartedAt.Format(time.RFC3339),
	}
	if d.FinishedAt != nil {
		m["finishedAt"] = d.FinishedAt.Format(time.RFC3339)
	}
	return m
}

func targetsJSON(ts []inventory.DeploymentTarget) []map[string]any {
	out := make([]map[string]any, 0, len(ts))
	for _, t := range ts {
		m := map[string]any{
			"id":            t.ID,
			"deploymentId":  t.DeploymentID,
			"componentName": t.ComponentName,
			"cloud":         t.Cloud,
			"region":        t.Region,
			"status":        t.Status,
			"startedAt":     t.StartedAt.Format(time.RFC3339),
		}
		if t.FinishedAt != nil {
			m["finishedAt"] = t.FinishedAt.Format(time.RFC3339)
		}
		out = append(out, m)
	}
	return out
}

func runJSON(r inventory.Run) map[string]any {
	m := map[string]any{
		"id":                 r.ID,
		"deploymentTargetId": r.DeploymentTargetID,
		"kind":               r.Kind,
		"status":             r.Status,
		"exitCode":           r.ExitCode,
		"startedAt":          r.StartedAt.Format(time.RFC3339),
	}
	if r.FinishedAt != nil {
		m["finishedAt"] = r.FinishedAt.Format(time.RFC3339)
	}
	return m
}

// --- envelope helpers ---

func writeData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": msg},
	})
}
