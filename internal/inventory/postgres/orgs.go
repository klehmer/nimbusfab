package postgres

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
	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, created_at FROM orgs WHERE id = $1", id).
		Scan(&o.ID, &o.Name, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("orgs.Get: %w", err)
	}
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
		if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *orgRepo) Create(ctx context.Context, o inventory.Org) error {
	_, err := r.db.ExecContext(ctx, "INSERT INTO orgs (id, name) VALUES ($1, $2)", o.ID, o.Name)
	if err != nil {
		return fmt.Errorf("orgs.Create: %w", err)
	}
	return nil
}
