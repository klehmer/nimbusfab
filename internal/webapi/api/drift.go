package api

import (
	"net/http"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// GetDrift → GET /api/v1/drift
//
// Returns every drift_status record for the org, enriched with deployment-
// target metadata so the UI can render rows without per-record lookups.
//
// Shape:
//
//	{
//	  "data": {
//	    "summary": {"total": N, "drifted": M, "clean": N-M},
//	    "records": [{
//	      "deploymentTargetId": "...",
//	      "deploymentId": "...",
//	      "componentName": "web-app",
//	      "cloud": "aws", "region": "us-east-1",
//	      "hasDrift": true,
//	      "detectedAt": "RFC3339"
//	    }]
//	  }
//	}
func (h *Handlers) GetDrift(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	records, err := h.Repo.DriftStatus().ListByOrg(ctx, h.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ErrInventory", err.Error())
		return
	}
	// Look up each target's metadata. N+1 acceptable at v1 scale; future
	// optimization could replace with a JOIN-flavored repo method.
	targets := map[string]inventory.DeploymentTarget{}
	for _, rec := range records {
		t, err := h.Repo.DeploymentTargets().Get(ctx, h.OrgID, rec.DeploymentTargetID)
		if err != nil || t == nil {
			continue
		}
		targets[rec.DeploymentTargetID] = *t
	}

	type recordOut struct {
		DeploymentTargetID string `json:"deploymentTargetId"`
		DeploymentID       string `json:"deploymentId"`
		ComponentName      string `json:"componentName"`
		Cloud              string `json:"cloud"`
		Region             string `json:"region"`
		HasDrift           bool   `json:"hasDrift"`
		DetectedAt         string `json:"detectedAt"`
	}
	out := make([]recordOut, 0, len(records))
	drifted := 0
	for _, rec := range records {
		t, ok := targets[rec.DeploymentTargetID]
		if !ok {
			// Skip orphaned drift rows (target was deleted).
			continue
		}
		if rec.HasDrift {
			drifted++
		}
		out = append(out, recordOut{
			DeploymentTargetID: rec.DeploymentTargetID,
			DeploymentID:       t.DeploymentID,
			ComponentName:      t.ComponentName,
			Cloud:              t.Cloud,
			Region:             t.Region,
			HasDrift:           rec.HasDrift,
			DetectedAt:         rec.DetectedAt.Format(time.RFC3339),
		})
	}

	writeData(w, map[string]any{
		"summary": map[string]any{
			"total":   len(out),
			"drifted": drifted,
			"clean":   len(out) - drifted,
		},
		"records": out,
	})
}
