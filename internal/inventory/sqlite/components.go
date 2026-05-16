package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type componentRepo struct{ db *sql.DB }

func (r *componentRepo) Get(ctx context.Context, orgID, id string) (*inventory.Component, error) {
	var c inventory.Component
	var irJSON, updatedAt string
	err := r.db.QueryRowContext(ctx,
		"SELECT id, org_id, project_id, stack_id, name, type, ir_json, updated_at FROM components WHERE org_id = ? AND id = ?",
		orgID, id).Scan(&c.ID, &c.OrgID, &c.ProjectID, &c.StackID, &c.Name, &c.Type, &irJSON, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("components.Get: %w", err)
	}
	c.IRJSON = []byte(irJSON)
	c.UpdatedAt = mustParseTime(updatedAt)
	return &c, nil
}

func (r *componentRepo) ListByStack(ctx context.Context, orgID, projectID, stackID string) ([]inventory.Component, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, org_id, project_id, stack_id, name, type, ir_json, updated_at FROM components WHERE org_id = ? AND project_id = ? AND stack_id = ? ORDER BY name",
		orgID, projectID, stackID)
	if err != nil {
		return nil, fmt.Errorf("components.ListByStack: %w", err)
	}
	defer rows.Close()
	var out []inventory.Component
	for rows.Next() {
		var c inventory.Component
		var irJSON, updatedAt string
		if err := rows.Scan(&c.ID, &c.OrgID, &c.ProjectID, &c.StackID, &c.Name, &c.Type, &irJSON, &updatedAt); err != nil {
			return nil, err
		}
		c.IRJSON = []byte(irJSON)
		c.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *componentRepo) Upsert(ctx context.Context, c inventory.Component) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO components (id, org_id, project_id, stack_id, name, type, ir_json, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
        ON CONFLICT(project_id, stack_id, name) DO UPDATE SET
            type = excluded.type,
            ir_json = excluded.ir_json,
            updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
    `, c.ID, c.OrgID, c.ProjectID, c.StackID, c.Name, c.Type, string(c.IRJSON))
	if err != nil {
		return fmt.Errorf("components.Upsert: %w", err)
	}
	return nil
}
