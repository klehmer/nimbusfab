package inventory_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestRunMigrations_FreshDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	if err := inventory.RunMigrations(context.Background(), db, inventory.FlavorSQLite); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n < 1 {
		t.Errorf("schema_migrations count = %d, want >= 1", n)
	}
	// Every table both backends expose should exist after a fresh migration.
	for _, tbl := range []string{
		"orgs", "users", "api_tokens",
		"projects", "stacks", "components", "compositions",
		"deployments", "deployment_targets", "runs",
		"run_logs", "drift_status",
		"cost_estimates", "cost_actuals",
		"secrets_refs", "audit_log",
	} {
		if _, err := db.Exec("SELECT * FROM " + tbl + " LIMIT 0"); err != nil {
			t.Errorf("table %q missing after migration: %v", tbl, err)
		}
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	_, _ = db.Exec("PRAGMA foreign_keys = ON")
	ctx := context.Background()
	if err := inventory.RunMigrations(ctx, db, inventory.FlavorSQLite); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := inventory.RunMigrations(ctx, db, inventory.FlavorSQLite); err != nil {
		t.Fatalf("second (should be no-op): %v", err)
	}
}
