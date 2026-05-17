package api

import (
	"net/http"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// GetDeploymentCosts → GET /api/v1/deployments/{id}/costs
//
// Returns the per-target cost-estimate breakdown for the deployment plus
// an aggregate total. Each target row carries the priced primitives that
// rolled up into its subtotal.
//
// Shape (matches the web app spec envelope):
//
//	{
//	  "data": {
//	    "deploymentId": "...",
//	    "currency": "USD",
//	    "total": 60.74,
//	    "targets": [
//	      {
//	        "deploymentTargetId": "...",
//	        "componentName": "web-app",
//	        "cloud": "aws", "region": "us-east-1",
//	        "total": 30.37,
//	        "primitives": [{primitiveId, unitPrice, units, unitOfMeasure, subtotal}]
//	      }
//	    ]
//	  }
//	}
func (h *Handlers) GetDeploymentCosts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()

	d, err := h.Repo.Deployments().Get(ctx, h.OrgID, id)
	if err != nil || d == nil {
		writeError(w, http.StatusNotFound, "ErrNotFound", "deployment not found: "+id)
		return
	}
	rows, err := h.Repo.CostEstimates().ListByDeployment(ctx, h.OrgID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ErrInventory", err.Error())
		return
	}
	targets, err := h.Repo.DeploymentTargets().ListByDeployment(ctx, h.OrgID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ErrInventory", err.Error())
		return
	}

	// Build run-id → target lookup so we can group estimates by target.
	runToTarget := map[string]inventory.DeploymentTarget{}
	for _, t := range targets {
		runs, _ := h.Repo.Runs().ListByDeploymentTarget(ctx, h.OrgID, t.ID)
		for _, run := range runs {
			runToTarget[run.ID] = t
		}
	}

	type primitiveOut struct {
		PrimitiveID   string  `json:"primitiveId"`
		UnitPrice     float64 `json:"unitPrice"`
		Units         float64 `json:"units"`
		UnitOfMeasure string  `json:"unitOfMeasure"`
		Subtotal      float64 `json:"subtotal"`
	}
	type targetOut struct {
		DeploymentTargetID string         `json:"deploymentTargetId"`
		ComponentName      string         `json:"componentName"`
		Cloud              string         `json:"cloud"`
		Region             string         `json:"region"`
		Total              float64        `json:"total"`
		Primitives         []primitiveOut `json:"primitives"`
	}

	byTarget := map[string]*targetOut{}
	var currency string
	var total float64
	for _, row := range rows {
		t, ok := runToTarget[row.RunID]
		if !ok {
			continue
		}
		out, ok := byTarget[t.ID]
		if !ok {
			out = &targetOut{
				DeploymentTargetID: t.ID,
				ComponentName:      t.ComponentName,
				Cloud:              t.Cloud,
				Region:             t.Region,
				Primitives:         []primitiveOut{},
			}
			byTarget[t.ID] = out
		}
		out.Primitives = append(out.Primitives, primitiveOut{
			PrimitiveID:   row.PrimitiveID,
			UnitPrice:     row.UnitPrice,
			Units:         row.Units,
			UnitOfMeasure: row.UnitOfMeasure,
			Subtotal:      row.Subtotal,
		})
		out.Total += row.Subtotal
		total += row.Subtotal
		if currency == "" {
			currency = row.Currency
		}
	}

	// Emit targets in the order they were listed (stable for UI).
	targetsOut := make([]targetOut, 0, len(byTarget))
	for _, t := range targets {
		if out, ok := byTarget[t.ID]; ok {
			targetsOut = append(targetsOut, *out)
		}
	}

	writeData(w, map[string]any{
		"deploymentId": id,
		"currency":     currency,
		"total":        total,
		"targets":      targetsOut,
	})
}
