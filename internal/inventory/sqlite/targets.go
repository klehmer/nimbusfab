package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type targetRepo struct{ db *sql.DB }

func (r *targetRepo) Get(ctx context.Context, orgID, id string) (*inventory.DeploymentTarget, error) {
	return r.scanOne(ctx, `
        SELECT id, org_id, deployment_id, component_name, cloud, region, credential_ref,
               COALESCE(workspace_path,''), COALESCE(plan_file,''), COALESCE(state_backend,''),
               status, started_at, finished_at
        FROM deployment_targets WHERE org_id = ? AND id = ?
    `, orgID, id)
}

func (r *targetRepo) ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]inventory.DeploymentTarget, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, deployment_id, component_name, cloud, region, credential_ref,
               COALESCE(workspace_path,''), COALESCE(plan_file,''), COALESCE(state_backend,''),
               status, started_at, finished_at
        FROM deployment_targets WHERE org_id = ? AND deployment_id = ?
        ORDER BY component_name, cloud, region
    `, orgID, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("targets.ListByDeployment: %w", err)
	}
	defer rows.Close()
	var out []inventory.DeploymentTarget
	for rows.Next() {
		t, err := scanTargetRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *targetRepo) Create(ctx context.Context, t inventory.DeploymentTarget) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO deployment_targets
            (id, org_id, deployment_id, component_name, cloud, region, credential_ref,
             workspace_path, plan_file, state_backend, status, started_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, t.ID, t.OrgID, t.DeploymentID, t.ComponentName, t.Cloud, t.Region, t.CredentialRef,
		t.WorkspacePath, t.PlanFile, string(t.StateBackend), t.Status, formatTime(t.StartedAt))
	if err != nil {
		return fmt.Errorf("targets.Create: %w", err)
	}
	return nil
}

func (r *targetRepo) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE deployment_targets SET status = ?, finished_at = ? WHERE org_id = ? AND id = ?",
		status, nullableTime(finishedAt), orgID, id)
	if err != nil {
		return fmt.Errorf("targets.UpdateStatus: %w", err)
	}
	return nil
}

func (r *targetRepo) scanOne(ctx context.Context, query string, args ...any) (*inventory.DeploymentTarget, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return scanTargetRow(rows)
}

func scanTargetRow(rows *sql.Rows) (*inventory.DeploymentTarget, error) {
	var t inventory.DeploymentTarget
	var sb string
	var startedAt string
	var finishedAt sql.NullString
	if err := rows.Scan(&t.ID, &t.OrgID, &t.DeploymentID, &t.ComponentName, &t.Cloud, &t.Region,
		&t.CredentialRef, &t.WorkspacePath, &t.PlanFile, &sb, &t.Status, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	t.StateBackend = []byte(sb)
	t.StartedAt = mustParseTime(startedAt)
	if finishedAt.Valid {
		tt := mustParseTime(finishedAt.String)
		t.FinishedAt = &tt
	}
	return &t, nil
}
