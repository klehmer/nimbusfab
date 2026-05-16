package sqlite

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
	var startedAt string
	var finishedAt sql.NullString
	var userID sql.NullString
	err := r.db.QueryRowContext(ctx, `
        SELECT id, org_id, deployment_target_id, kind, status, exit_code, started_at, finished_at, user_id
        FROM runs WHERE org_id = ? AND id = ?
    `, orgID, id).Scan(&run.ID, &run.OrgID, &run.DeploymentTargetID, &run.Kind, &run.Status,
		&exit, &startedAt, &finishedAt, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runs.Get: %w", err)
	}
	run.ExitCode = int(exit.Int64)
	run.StartedAt = mustParseTime(startedAt)
	if finishedAt.Valid {
		t := mustParseTime(finishedAt.String)
		run.FinishedAt = &t
	}
	run.UserID = userID.String
	return &run, nil
}

func (r *runRepo) ListByDeploymentTarget(ctx context.Context, orgID, dtID string) ([]inventory.Run, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, deployment_target_id, kind, status, COALESCE(exit_code,0), started_at, finished_at, COALESCE(user_id,'')
        FROM runs WHERE org_id = ? AND deployment_target_id = ?
        ORDER BY started_at DESC
    `, orgID, dtID)
	if err != nil {
		return nil, fmt.Errorf("runs.ListByDeploymentTarget: %w", err)
	}
	defer rows.Close()
	var out []inventory.Run
	for rows.Next() {
		var run inventory.Run
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&run.ID, &run.OrgID, &run.DeploymentTargetID, &run.Kind, &run.Status,
			&run.ExitCode, &startedAt, &finishedAt, &run.UserID); err != nil {
			return nil, err
		}
		run.StartedAt = mustParseTime(startedAt)
		if finishedAt.Valid {
			t := mustParseTime(finishedAt.String)
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
		finishedAt = formatTime(*run.FinishedAt)
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO runs (id, org_id, deployment_target_id, kind, status, exit_code, started_at, finished_at, user_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, run.ID, run.OrgID, run.DeploymentTargetID, run.Kind, run.Status, run.ExitCode,
		formatTime(run.StartedAt), finishedAt, userID)
	if err != nil {
		return fmt.Errorf("runs.Create: %w", err)
	}
	return nil
}

func (r *runRepo) UpdateStatus(ctx context.Context, orgID, id, status string, exitCode int, finishedAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE runs SET status = ?, exit_code = ?, finished_at = ?
        WHERE org_id = ? AND id = ?
    `, status, exitCode, nullableTime(finishedAt), orgID, id)
	if err != nil {
		return fmt.Errorf("runs.UpdateStatus: %w", err)
	}
	return nil
}
