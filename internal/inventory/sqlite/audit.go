package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

type auditRepo struct{ db *sql.DB }

// Append writes one audit entry. The auto-increment id is assigned by SQLite;
// timestamp defaults to now() when caller supplies the zero value.
func (r *auditRepo) Append(ctx context.Context, e inventory.AuditEntry) error {
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO audit_log (org_id, actor_user_id, verb, target, payload_json, timestamp)
        VALUES (?, ?, ?, ?, ?, ?)
    `, e.OrgID, nullableStr(e.ActorUserID), e.Verb, nullableStr(e.Target),
		nullableStr(string(e.PayloadJSON)), formatTime(ts))
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
        SELECT org_id, COALESCE(actor_user_id, ''), verb, COALESCE(target, ''), COALESCE(payload_json, ''), timestamp
        FROM audit_log
        WHERE org_id = ? AND timestamp >= ? AND timestamp <= ?
        ORDER BY timestamp DESC, id DESC
        LIMIT ?
    `, orgID, formatTime(since), formatTime(until), limit)
	if err != nil {
		return nil, fmt.Errorf("audit_log.Query: %w", err)
	}
	defer rows.Close()
	var out []inventory.AuditEntry
	for rows.Next() {
		var e inventory.AuditEntry
		var ts, payload string
		if err := rows.Scan(&e.OrgID, &e.ActorUserID, &e.Verb, &e.Target, &payload, &ts); err != nil {
			return nil, err
		}
		e.PayloadJSON = []byte(payload)
		e.Timestamp = mustParseTime(ts)
		out = append(out, e)
	}
	return out, rows.Err()
}

// nullableStr returns nil for empty strings so the column stores NULL rather
// than '' (matters for COALESCE reads and indexing semantics).
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

