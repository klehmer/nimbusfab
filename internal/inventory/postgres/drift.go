package postgres

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
	err := r.db.QueryRowContext(ctx, `
        SELECT deployment_target_id, org_id, detected_at, has_drift, COALESCE(summary_json::text,'')
        FROM drift_status WHERE org_id = $1 AND deployment_target_id = $2
    `, orgID, dtID).Scan(&d.DeploymentTargetID, &d.OrgID, &d.DetectedAt, &d.HasDrift, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("drift.Get: %w", err)
	}
	d.SummaryJSON = []byte(summary)
	return &d, nil
}

func (r *driftRepo) Upsert(ctx context.Context, d inventory.DriftRecord) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO drift_status (deployment_target_id, org_id, detected_at, has_drift, summary_json)
        VALUES ($1, $2, $3, $4, $5::jsonb)
        ON CONFLICT (deployment_target_id) DO UPDATE SET
            detected_at = EXCLUDED.detected_at,
            has_drift   = EXCLUDED.has_drift,
            summary_json = EXCLUDED.summary_json
    `, d.DeploymentTargetID, d.OrgID, d.DetectedAt, d.HasDrift, jsonOrEmpty(d.SummaryJSON))
	if err != nil {
		return fmt.Errorf("drift.Upsert: %w", err)
	}
	return nil
}
