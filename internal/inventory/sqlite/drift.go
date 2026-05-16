package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type driftRepo struct{ db *sql.DB }

func (r *driftRepo) Get(ctx context.Context, orgID, dtID string) (*inventory.DriftRecord, error) {
	var d inventory.DriftRecord
	var summary string
	var detectedAt string
	var hasDrift int
	err := r.db.QueryRowContext(ctx, `
        SELECT deployment_target_id, org_id, detected_at, has_drift, summary_json
        FROM drift_status WHERE org_id = ? AND deployment_target_id = ?
    `, orgID, dtID).Scan(&d.DeploymentTargetID, &d.OrgID, &detectedAt, &hasDrift, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("drift.Get: %w", err)
	}
	d.DetectedAt = mustParseTime(detectedAt)
	d.HasDrift = hasDrift != 0
	d.SummaryJSON = []byte(summary)
	return &d, nil
}

func (r *driftRepo) Upsert(ctx context.Context, d inventory.DriftRecord) error {
	hasDrift := 0
	if d.HasDrift {
		hasDrift = 1
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO drift_status (deployment_target_id, org_id, detected_at, has_drift, summary_json)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(deployment_target_id) DO UPDATE SET
            detected_at = excluded.detected_at,
            has_drift   = excluded.has_drift,
            summary_json = excluded.summary_json
    `, d.DeploymentTargetID, d.OrgID, formatTime(d.DetectedAt), hasDrift, string(d.SummaryJSON))
	if err != nil {
		return fmt.Errorf("drift.Upsert: %w", err)
	}
	return nil
}
