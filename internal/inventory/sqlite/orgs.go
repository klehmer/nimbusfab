package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type orgRepo struct{ db *sql.DB }

func (r *orgRepo) Get(ctx context.Context, id string) (*inventory.Org, error) {
	var o inventory.Org
	var createdAt string
	err := r.db.QueryRowContext(ctx, "SELECT id, name, created_at FROM orgs WHERE id = ?", id).
		Scan(&o.ID, &o.Name, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("orgs.Get: %w", err)
	}
	o.CreatedAt = mustParseTime(createdAt)
	return &o, nil
}

func (r *orgRepo) List(ctx context.Context) ([]inventory.Org, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, name, created_at FROM orgs ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("orgs.List: %w", err)
	}
	defer rows.Close()
	var out []inventory.Org
	for rows.Next() {
		var o inventory.Org
		var createdAt string
		if err := rows.Scan(&o.ID, &o.Name, &createdAt); err != nil {
			return nil, err
		}
		o.CreatedAt = mustParseTime(createdAt)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *orgRepo) Create(ctx context.Context, o inventory.Org) error {
	_, err := r.db.ExecContext(ctx, "INSERT INTO orgs (id, name) VALUES (?, ?)", o.ID, o.Name)
	if err != nil {
		return fmt.Errorf("orgs.Create: %w", err)
	}
	return nil
}
