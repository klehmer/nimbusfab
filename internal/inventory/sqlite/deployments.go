package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type deploymentRepo struct{ db *sql.DB }

func (r *deploymentRepo) Get(ctx context.Context, orgID, id string) (*inventory.Deployment, error) {
	var d inventory.Deployment
	var requestedBy sql.NullString
	var startedAt string
	var finishedAt sql.NullString
	err := r.db.QueryRowContext(ctx, `
        SELECT id, org_id, project_id, stack_id, requested_by_user_id, status,
               COALESCE(partial_failure_policy,''), started_at, finished_at,
               drift_interval_seconds
        FROM deployments WHERE org_id = ? AND id = ?
    `, orgID, id).Scan(&d.ID, &d.OrgID, &d.ProjectID, &d.StackID, &requestedBy,
		&d.Status, &d.PartialFailurePolicy, &startedAt, &finishedAt,
		&d.DriftIntervalSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("deployments.Get: %w", err)
	}
	d.RequestedByUserID = requestedBy.String
	d.StartedAt = mustParseTime(startedAt)
	if finishedAt.Valid {
		t := mustParseTime(finishedAt.String)
		d.FinishedAt = &t
	}
	return &d, nil
}

func (r *deploymentRepo) Create(ctx context.Context, d inventory.Deployment) error {
	var requestedBy any
	if d.RequestedByUserID != "" {
		requestedBy = d.RequestedByUserID
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO deployments
            (id, org_id, project_id, stack_id, requested_by_user_id, status,
             partial_failure_policy, started_at, drift_interval_seconds)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, d.ID, d.OrgID, d.ProjectID, d.StackID, requestedBy, d.Status,
		d.PartialFailurePolicy, formatTime(d.StartedAt), d.DriftIntervalSeconds)
	if err != nil {
		return fmt.Errorf("deployments.Create: %w", err)
	}
	return nil
}

func (r *deploymentRepo) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE deployments SET status = ?, finished_at = ? WHERE org_id = ? AND id = ?",
		status, nullableTime(finishedAt), orgID, id)
	if err != nil {
		return fmt.Errorf("deployments.UpdateStatus: %w", err)
	}
	return nil
}

func (r *deploymentRepo) ListByProject(ctx context.Context, orgID, projectID string, limit int) ([]inventory.Deployment, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, project_id, stack_id, COALESCE(requested_by_user_id, ''), status,
               COALESCE(partial_failure_policy,''), started_at, finished_at,
               drift_interval_seconds
        FROM deployments WHERE org_id = ? AND project_id = ?
        ORDER BY started_at DESC LIMIT ?
    `, orgID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("deployments.ListByProject: %w", err)
	}
	defer rows.Close()
	var out []inventory.Deployment
	for rows.Next() {
		var d inventory.Deployment
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&d.ID, &d.OrgID, &d.ProjectID, &d.StackID, &d.RequestedByUserID,
			&d.Status, &d.PartialFailurePolicy, &startedAt, &finishedAt,
			&d.DriftIntervalSeconds); err != nil {
			return nil, err
		}
		d.StartedAt = mustParseTime(startedAt)
		if finishedAt.Valid {
			t := mustParseTime(finishedAt.String)
			d.FinishedAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *deploymentRepo) ListAll(ctx context.Context, orgID string) ([]inventory.Deployment, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, project_id, stack_id, COALESCE(requested_by_user_id, ''), status,
               COALESCE(partial_failure_policy,''), started_at, finished_at,
               drift_interval_seconds
        FROM deployments WHERE org_id = ?
        ORDER BY started_at DESC
    `, orgID)
	if err != nil {
		return nil, fmt.Errorf("deployments.ListAll: %w", err)
	}
	defer rows.Close()
	var out []inventory.Deployment
	for rows.Next() {
		var d inventory.Deployment
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&d.ID, &d.OrgID, &d.ProjectID, &d.StackID, &d.RequestedByUserID,
			&d.Status, &d.PartialFailurePolicy, &startedAt, &finishedAt,
			&d.DriftIntervalSeconds); err != nil {
			return nil, err
		}
		d.StartedAt = mustParseTime(startedAt)
		if finishedAt.Valid {
			t := mustParseTime(finishedAt.String)
			d.FinishedAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
