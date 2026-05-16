package inventory

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

// Flavor selects which migration files apply. SQLite picks `.sqlite.sql`
// where present; Postgres picks the bare `.sql`.
type Flavor string

const (
	FlavorSQLite   Flavor = "sqlite"
	FlavorPostgres Flavor = "postgres"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// RunMigrations applies all pending migrations to db. Records applied versions
// in schema_migrations. Idempotent.
func RunMigrations(ctx context.Context, db *sql.DB, flavor Flavor) error {
	if _, err := db.ExecContext(ctx, schemaMigrationsTable(flavor)); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied, err := loadApplied(ctx, db)
	if err != nil {
		return fmt.Errorf("load applied: %w", err)
	}
	migrations, err := discoverMigrations(flavor)
	if err != nil {
		return fmt.Errorf("discover migrations: %w", err)
	}
	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			return fmt.Errorf("apply %s: %w", m.Version, err)
		}
	}
	return nil
}

type migrationFile struct {
	Version string
	Body    string
}

func discoverMigrations(flavor Flavor) ([]migrationFile, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	byVersion := map[string]string{}
	overridden := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, isFlavor := parseMigrationName(name, flavor)
		if version == "" {
			continue
		}
		if isFlavor {
			body, err := migrationFS.ReadFile("migrations/" + name)
			if err != nil {
				return nil, err
			}
			byVersion[version] = string(body)
			overridden[version] = true
			continue
		}
		if overridden[version] {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		byVersion[version] = string(body)
	}
	var out []migrationFile
	for v, b := range byVersion {
		out = append(out, migrationFile{Version: v, Body: b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// parseMigrationName returns the version slug ("0001_init") and whether the
// file is a flavor-specific override. Returns "" for non-matching files.
func parseMigrationName(name string, flavor Flavor) (version string, isFlavor bool) {
	if strings.HasSuffix(name, ".sqlite.sql") {
		if flavor != FlavorSQLite {
			return "", false
		}
		return strings.TrimSuffix(name, ".sqlite.sql"), true
	}
	if strings.HasSuffix(name, ".postgres.sql") {
		if flavor != FlavorPostgres {
			return "", false
		}
		return strings.TrimSuffix(name, ".postgres.sql"), true
	}
	return strings.TrimSuffix(name, ".sql"), false
}

func loadApplied(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func applyOne(ctx context.Context, db *sql.DB, m migrationFile) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.Body); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))",
		m.Version); err != nil {
		return err
	}
	return tx.Commit()
}

func schemaMigrationsTable(flavor Flavor) string {
	if flavor == FlavorPostgres {
		return `CREATE TABLE IF NOT EXISTS schema_migrations (
            version    TEXT PRIMARY KEY,
            applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
        )`
	}
	return `CREATE TABLE IF NOT EXISTS schema_migrations (
        version    TEXT PRIMARY KEY,
        applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
    )`
}
