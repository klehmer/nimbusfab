package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type apiTokenRepo struct{ db *sql.DB }

func (r *apiTokenRepo) Create(ctx context.Context, t inventory.ApiToken) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO api_tokens (id, org_id, user_id, token_hash, name, prefix)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, t.ID, t.OrgID, t.UserID, t.TokenHash, t.Name, t.Prefix)
	if err != nil {
		return fmt.Errorf("api_tokens.Create: %w", err)
	}
	return nil
}

func (r *apiTokenRepo) GetByPrefix(ctx context.Context, prefix string) (*inventory.ApiToken, error) {
	var t inventory.ApiToken
	var hash []byte
	var lastUsedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
        SELECT id, org_id, user_id, token_hash, name, prefix, created_at, last_used_at
        FROM api_tokens WHERE prefix = $1
    `, prefix).Scan(&t.ID, &t.OrgID, &t.UserID, &hash, &t.Name, &t.Prefix, &t.CreatedAt, &lastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("api_tokens.GetByPrefix: %w", err)
	}
	t.TokenHash = hash
	if lastUsedAt.Valid {
		lt := lastUsedAt.Time
		t.LastUsedAt = &lt
	}
	return &t, nil
}

func (r *apiTokenRepo) ListByUser(ctx context.Context, orgID, userID string) ([]inventory.ApiToken, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, org_id, user_id, token_hash, name, prefix, created_at, last_used_at
        FROM api_tokens WHERE org_id = $1 AND user_id = $2
        ORDER BY created_at DESC
    `, orgID, userID)
	if err != nil {
		return nil, fmt.Errorf("api_tokens.ListByUser: %w", err)
	}
	defer rows.Close()
	var out []inventory.ApiToken
	for rows.Next() {
		var t inventory.ApiToken
		var hash []byte
		var lastUsedAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.OrgID, &t.UserID, &hash, &t.Name, &t.Prefix, &t.CreatedAt, &lastUsedAt); err != nil {
			return nil, err
		}
		t.TokenHash = hash
		if lastUsedAt.Valid {
			lt := lastUsedAt.Time
			t.LastUsedAt = &lt
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *apiTokenRepo) UpdateLastUsed(ctx context.Context, id string, t time.Time) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE api_tokens SET last_used_at = $1 WHERE id = $2",
		t, id)
	if err != nil {
		return fmt.Errorf("api_tokens.UpdateLastUsed: %w", err)
	}
	return nil
}

func (r *apiTokenRepo) Revoke(ctx context.Context, orgID, id string) error {
	_, err := r.db.ExecContext(ctx,
		"DELETE FROM api_tokens WHERE org_id = $1 AND id = $2",
		orgID, id)
	if err != nil {
		return fmt.Errorf("api_tokens.Revoke: %w", err)
	}
	return nil
}
