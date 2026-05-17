package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type userRepo struct{ db *sql.DB }

func (r *userRepo) Get(ctx context.Context, orgID, id string) (*inventory.User, error) {
	return r.scanOne(ctx,
		"SELECT id, org_id, email, COALESCE(display_name,''), is_local, COALESCE(oidc_provider,''), COALESCE(oidc_subject,''), password_hash, created_at FROM users WHERE org_id = ? AND id = ?",
		orgID, id)
}

func (r *userRepo) GetByEmail(ctx context.Context, orgID, email string) (*inventory.User, error) {
	return r.scanOne(ctx,
		"SELECT id, org_id, email, COALESCE(display_name,''), is_local, COALESCE(oidc_provider,''), COALESCE(oidc_subject,''), password_hash, created_at FROM users WHERE org_id = ? AND email = ?",
		orgID, email)
}

func (r *userRepo) Create(ctx context.Context, u inventory.User) error {
	isLocal := 0
	if u.IsLocal {
		isLocal = 1
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO users (id, org_id, email, display_name, is_local, oidc_provider, oidc_subject, password_hash)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, u.ID, u.OrgID, u.Email, nullableStr(u.DisplayName), isLocal,
		nullableStr(u.OIDCProvider), nullableStr(u.OIDCSubject), u.PasswordHash)
	if err != nil {
		return fmt.Errorf("users.Create: %w", err)
	}
	return nil
}

func (r *userRepo) UpdatePasswordHash(ctx context.Context, orgID, id string, hash []byte) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE users SET password_hash = ? WHERE org_id = ? AND id = ?",
		hash, orgID, id)
	if err != nil {
		return fmt.Errorf("users.UpdatePasswordHash: %w", err)
	}
	return nil
}

func (r *userRepo) scanOne(ctx context.Context, query string, args ...any) (*inventory.User, error) {
	var u inventory.User
	var createdAt string
	var isLocal int
	var pwHash []byte
	err := r.db.QueryRowContext(ctx, query, args...).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &isLocal,
			&u.OIDCProvider, &u.OIDCSubject, &pwHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	u.IsLocal = isLocal != 0
	u.PasswordHash = pwHash
	u.CreatedAt = mustParseTime(createdAt)
	return &u, nil
}
