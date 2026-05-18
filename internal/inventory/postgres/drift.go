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

func (r *driftRepo) ListByOrg(ctx context.Context, orgID string) ([]inventory.DriftRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT deployment_target_id, org_id, detected_at, has_drift, COALESCE(summary_json::text,'')
        FROM drift_status WHERE org_id = $1
        ORDER BY detected_at DESC
    `, orgID)
	if err != nil {
		return nil, fmt.Errorf("drift.ListByOrg: %w", err)
	}
	defer rows.Close()
	var out []inventory.DriftRecord
	for rows.Next() {
		var d inventory.DriftRecord
		var summary string
		if err := rows.Scan(&d.DeploymentTargetID, &d.OrgID, &d.DetectedAt, &d.HasDrift, &summary); err != nil {
			return nil, err
		}
		d.SummaryJSON = []byte(summary)
		out = append(out, d)
	}
	return out, rows.Err()
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

// LatestByDeployment returns the current drift_status row for every
// deployment_target belonging to the given deployment, joined to
// deployment_targets for component metadata.
func (r *driftRepo) LatestByDeployment(ctx context.Context, orgID, deploymentID string) ([]inventory.DriftRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT ds.deployment_target_id, ds.org_id, ds.detected_at, ds.has_drift,
               COALESCE(ds.summary_json::text,''),
               dt.component_name, dt.cloud, dt.region, dt.deployment_id
          FROM drift_status ds
          JOIN deployment_targets dt ON dt.id = ds.deployment_target_id
         WHERE ds.org_id = $1 AND dt.deployment_id = $2
         ORDER BY ds.detected_at DESC
    `, orgID, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("drift.LatestByDeployment: %w", err)
	}
	defer rows.Close()
	var out []inventory.DriftRecord
	for rows.Next() {
		var d inventory.DriftRecord
		var summary string
		if err := rows.Scan(
			&d.DeploymentTargetID, &d.OrgID, &d.DetectedAt, &d.HasDrift, &summary,
			&d.ComponentName, &d.Cloud, &d.Region, &d.DeploymentID,
		); err != nil {
			return nil, err
		}
		d.SummaryJSON = []byte(summary)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListByProject joins drift_status → deployment_targets → deployments to
// return the current drift row for every target in the project.
func (r *driftRepo) ListByProject(ctx context.Context, orgID, projectID string) ([]inventory.DriftRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT ds.deployment_target_id, ds.org_id, ds.detected_at, ds.has_drift,
               COALESCE(ds.summary_json::text,''),
               dt.component_name, dt.cloud, dt.region, dt.deployment_id
          FROM drift_status ds
          JOIN deployment_targets dt ON dt.id = ds.deployment_target_id
          JOIN deployments d ON d.id = dt.deployment_id
         WHERE ds.org_id = $1 AND d.project_id = $2
         ORDER BY ds.detected_at DESC
    `, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("drift.ListByProject: %w", err)
	}
	defer rows.Close()
	var out []inventory.DriftRecord
	for rows.Next() {
		var d inventory.DriftRecord
		var summary string
		if err := rows.Scan(
			&d.DeploymentTargetID, &d.OrgID, &d.DetectedAt, &d.HasDrift, &summary,
			&d.ComponentName, &d.Cloud, &d.Region, &d.DeploymentID,
		); err != nil {
			return nil, err
		}
		d.SummaryJSON = []byte(summary)
		out = append(out, d)
	}
	return out, rows.Err()
}
