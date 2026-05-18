package sqlite

import (
	"context"
	"testing"
)

// TestMigration_0004_DriftIntervalColumn verifies that after RunMigrations the
// deployments table has the drift_interval_seconds column added by migration 0004.
// Uses an internal (white-box) test so it can access the raw *sql.DB.
func TestMigration_0004_DriftIntervalColumn(t *testing.T) {
	r, err := Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	ctx := context.Background()
	if err := r.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// A zero-row query is sufficient to confirm the column exists.
	_, err = r.db.QueryContext(ctx, `SELECT drift_interval_seconds FROM deployments LIMIT 0`)
	if err != nil {
		t.Fatalf("drift_interval_seconds column missing after migration: %v", err)
	}
}
