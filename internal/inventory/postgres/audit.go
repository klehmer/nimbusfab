package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type auditRepo struct{ db *sql.DB }

// Append writes one audit entry. The BIGSERIAL id is assigned by Postgres;
// timestamp defaults to now() when caller supplies the zero value.
// actor_user_id and target use NULLIF($N, ”) so Postgres' UUID column
// rejects the empty string in favor of NULL.
func (r *auditRepo) Append(ctx context.Context, e inventory.AuditEntry) error {
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	// actor_user_id is a UUID column → can't accept ''. Pass nil when empty.
	var actor any
	if e.ActorUserID != "" {
		actor = e.ActorUserID
	}
	var target any
	if e.Target != "" {
		target = e.Target
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO audit_log (org_id, actor_user_id, verb, target, payload_json, timestamp)
        VALUES ($1, $2, $3, $4, $5::jsonb, $6)
    `, e.OrgID, actor, e.Verb, target, jsonOrEmpty(e.PayloadJSON), ts)
	if err != nil {
		return fmt.Errorf("audit_log.Append: %w", err)
	}
	return nil
}

// Query returns entries in [since, until] for the given org, newest first.
// limit caps the result; 0 / negative defaults to 100.
func (r *auditRepo) Query(ctx context.Context, orgID string, since, until time.Time, limit int) ([]inventory.AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
        SELECT org_id, COALESCE(actor_user_id::text, ''), verb, COALESCE(target, ''),
               COALESCE(payload_json::text, ''), timestamp
        FROM audit_log
        WHERE org_id = $1 AND timestamp >= $2 AND timestamp <= $3
        ORDER BY timestamp DESC, id DESC
        LIMIT $4
    `, orgID, since, until, limit)
	if err != nil {
		return nil, fmt.Errorf("audit_log.Query: %w", err)
	}
	defer rows.Close()
	var out []inventory.AuditEntry
	for rows.Next() {
		var e inventory.AuditEntry
		var payload string
		if err := rows.Scan(&e.OrgID, &e.ActorUserID, &e.Verb, &e.Target, &payload, &e.Timestamp); err != nil {
			return nil, err
		}
		e.PayloadJSON = []byte(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}
