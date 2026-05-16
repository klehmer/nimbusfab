package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type stackRepo struct{ db *sql.DB }

func (r *stackRepo) Get(ctx context.Context, orgID, id string) (*inventory.Stack, error) {
	return r.scanOne(ctx,
		"SELECT id, org_id, project_id, name, COALESCE(state_backend_kind,''), COALESCE(state_backend_cfg,'') FROM stacks WHERE org_id = ? AND id = ?",
		orgID, id)
}

func (r *stackRepo) GetByName(ctx context.Context, orgID, projectID, name string) (*inventory.Stack, error) {
	return r.scanOne(ctx,
		"SELECT id, org_id, project_id, name, COALESCE(state_backend_kind,''), COALESCE(state_backend_cfg,'') FROM stacks WHERE org_id = ? AND project_id = ? AND name = ?",
		orgID, projectID, name)
}

func (r *stackRepo) List(ctx context.Context, orgID, projectID string) ([]inventory.Stack, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, org_id, project_id, name, COALESCE(state_backend_kind,''), COALESCE(state_backend_cfg,'') FROM stacks WHERE org_id = ? AND project_id = ? ORDER BY name",
		orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("stacks.List: %w", err)
	}
	defer rows.Close()
	var out []inventory.Stack
	for rows.Next() {
		var s inventory.Stack
		var cfg string
		if err := rows.Scan(&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.StateBackendKind, &cfg); err != nil {
			return nil, err
		}
		s.StateBackendCfg = []byte(cfg)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *stackRepo) Upsert(ctx context.Context, s inventory.Stack) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO stacks (id, org_id, project_id, name, state_backend_kind, state_backend_cfg)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(project_id, name) DO UPDATE SET
            state_backend_kind = excluded.state_backend_kind,
            state_backend_cfg  = excluded.state_backend_cfg
    `, s.ID, s.OrgID, s.ProjectID, s.Name, s.StateBackendKind, string(s.StateBackendCfg))
	if err != nil {
		return fmt.Errorf("stacks.Upsert: %w", err)
	}
	return nil
}

func (r *stackRepo) scanOne(ctx context.Context, query string, args ...any) (*inventory.Stack, error) {
	var s inventory.Stack
	var cfg string
	err := r.db.QueryRowContext(ctx, query, args...).
		Scan(&s.ID, &s.OrgID, &s.ProjectID, &s.Name, &s.StateBackendKind, &cfg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stacks: %w", err)
	}
	s.StateBackendCfg = []byte(cfg)
	return &s, nil
}
