package postgres

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
		"SELECT id, org_id, email, COALESCE(display_name,''), is_local, COALESCE(oidc_provider,''), COALESCE(oidc_subject,''), password_hash, created_at FROM users WHERE org_id = $1 AND id = $2",
		orgID, id)
}

func (r *userRepo) GetByEmail(ctx context.Context, orgID, email string) (*inventory.User, error) {
	return r.scanOne(ctx,
		"SELECT id, org_id, email, COALESCE(display_name,''), is_local, COALESCE(oidc_provider,''), COALESCE(oidc_subject,''), password_hash, created_at FROM users WHERE org_id = $1 AND email = $2",
		orgID, email)
}

func (r *userRepo) Create(ctx context.Context, u inventory.User) error {
	var dn, op, os any
	if u.DisplayName != "" {
		dn = u.DisplayName
	}
	if u.OIDCProvider != "" {
		op = u.OIDCProvider
	}
	if u.OIDCSubject != "" {
		os = u.OIDCSubject
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO users (id, org_id, email, display_name, is_local, oidc_provider, oidc_subject, password_hash)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `, u.ID, u.OrgID, u.Email, dn, u.IsLocal, op, os, u.PasswordHash)
	if err != nil {
		return fmt.Errorf("users.Create: %w", err)
	}
	return nil
}

func (r *userRepo) UpdatePasswordHash(ctx context.Context, orgID, id string, hash []byte) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE users SET password_hash = $1 WHERE org_id = $2 AND id = $3",
		hash, orgID, id)
	if err != nil {
		return fmt.Errorf("users.UpdatePasswordHash: %w", err)
	}
	return nil
}

func (r *userRepo) scanOne(ctx context.Context, query string, args ...any) (*inventory.User, error) {
	var u inventory.User
	var pwHash []byte
	err := r.db.QueryRowContext(ctx, query, args...).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &u.IsLocal,
			&u.OIDCProvider, &u.OIDCSubject, &pwHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	u.PasswordHash = pwHash
	return &u, nil
}
