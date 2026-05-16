package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type projectRepo struct{ db *sql.DB }

func (r *projectRepo) Get(ctx context.Context, orgID, id string) (*inventory.Project, error) {
	var p inventory.Project
	var createdAt string
	err := r.db.QueryRowContext(ctx,
		"SELECT id, org_id, name, COALESCE(source_uri, ''), created_at FROM projects WHERE org_id = ? AND id = ?",
		orgID, id).Scan(&p.ID, &p.OrgID, &p.Name, &p.SourceURI, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("projects.Get: %w", err)
	}
	p.CreatedAt = mustParseTime(createdAt)
	return &p, nil
}

func (r *projectRepo) List(ctx context.Context, orgID string) ([]inventory.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, org_id, name, COALESCE(source_uri, ''), created_at FROM projects WHERE org_id = ? ORDER BY name",
		orgID)
	if err != nil {
		return nil, fmt.Errorf("projects.List: %w", err)
	}
	defer rows.Close()
	var out []inventory.Project
	for rows.Next() {
		var p inventory.Project
		var createdAt string
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.SourceURI, &createdAt); err != nil {
			return nil, err
		}
		p.CreatedAt = mustParseTime(createdAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *projectRepo) Create(ctx context.Context, p inventory.Project) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO projects (id, org_id, name, source_uri) VALUES (?, ?, ?, ?)",
		p.ID, p.OrgID, p.Name, p.SourceURI)
	if err != nil {
		return fmt.Errorf("projects.Create: %w", err)
	}
	return nil
}
