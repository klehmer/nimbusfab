package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type runRepo struct{ db *sql.DB }

func (r *runRepo) Get(ctx context.Context, orgID, id string) (*inventory.Run, error) {
	var run inventory.Run
	var exit sql.NullInt64
	var finishedAt sql.NullTime
	var userID sql.NullString
	err := r.db.QueryRowContext(ctx, `
        SELECT id, org_id, deployment_target_id, kind, status, exit_code, started_at, finished_at, user_id
        FROM runs WHERE org_id = $1 AND id = $2
    `, orgID, id).Scan(&run.ID, &run.OrgID, &run.DeploymentTargetID, &run.Kind, &run.Status,
		&exit, &run.StartedAt, &finishedAt, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runs.Get: %w", err)
	}
	run.ExitCode = int(exit.Int64)
	if finishedAt.Valid {
		t := finishedAt.Time
		run.FinishedAt = &t
	}
	run.UserID = userID.String
	return &run, nil
}

func (r *runRepo) ListByDeploymentTarget(ctx context.Context, orgID, dtID string) ([]inventory.Run, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, deployment_target_id, kind, status, COALESCE(exit_code,0), started_at, finished_at, COALESCE(user_id::text,'')
        FROM runs WHERE org_id = $1 AND deployment_target_id = $2
        ORDER BY started_at DESC
    `, orgID, dtID)
	if err != nil {
		return nil, fmt.Errorf("runs.ListByDeploymentTarget: %w", err)
	}
	defer rows.Close()
	var out []inventory.Run
	for rows.Next() {
		var run inventory.Run
		var finishedAt sql.NullTime
		if err := rows.Scan(&run.ID, &run.OrgID, &run.DeploymentTargetID, &run.Kind, &run.Status,
			&run.ExitCode, &run.StartedAt, &finishedAt, &run.UserID); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			run.FinishedAt = &t
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *runRepo) Create(ctx context.Context, run inventory.Run) error {
	var userID any
	if run.UserID != "" {
		userID = run.UserID
	}
	var finishedAt any
	if run.FinishedAt != nil && !run.FinishedAt.IsZero() {
		finishedAt = *run.FinishedAt
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO runs (id, org_id, deployment_target_id, kind, status, exit_code, started_at, finished_at, user_id)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, run.ID, run.OrgID, run.DeploymentTargetID, run.Kind, run.Status, run.ExitCode,
		run.StartedAt, finishedAt, userID)
	if err != nil {
		return fmt.Errorf("runs.Create: %w", err)
	}
	return nil
}

func (r *runRepo) UpdateStatus(ctx context.Context, orgID, id, status string, exitCode int, finishedAt *time.Time) error {
	var ft any
	if finishedAt != nil {
		ft = *finishedAt
	}
	_, err := r.db.ExecContext(ctx, `
        UPDATE runs SET status = $1, exit_code = $2, finished_at = $3
        WHERE org_id = $4 AND id = $5
    `, status, exitCode, ft, orgID, id)
	if err != nil {
		return fmt.Errorf("runs.UpdateStatus: %w", err)
	}
	return nil
}
