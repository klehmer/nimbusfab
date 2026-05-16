// Package sqlite implements pkg/inventory.Repo against modernc.org/sqlite.
// All cross-cutting concerns (connection setup, foreign-key pragma, helpers)
// live here; each sub-repo lives in its own file.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// Repo is the SQLite Repo implementation.
type Repo struct {
	db *sql.DB
}

// init registers the sqlite scheme with the inventory dispatcher so
// inventory.Open(ctx, "sqlite:...") routes here automatically.
func init() {
	inventory.RegisterBackend("sqlite", func(ctx context.Context, dsn string) (inventory.Repo, error) {
		r, err := Open(dsn)
		if err != nil {
			return nil, err
		}
		if err := r.Migrate(ctx); err != nil {
			_ = r.Close()
			return nil, err
		}
		return r, nil
	})
}

// Open returns a SQLite Repo from a DSN like "sqlite:///path/to/file.db" or
// "sqlite::memory:". Foreign keys are enabled.
func Open(dsn string) (*Repo, error) {
	path, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("foreign_keys pragma: %w", err)
	}
	return &Repo{db: db}, nil
}

// parseDSN turns "sqlite:///path" or "sqlite::memory:" into the path
// modernc.org/sqlite expects. Plain paths are passed through.
func parseDSN(dsn string) (string, error) {
	if strings.HasPrefix(dsn, "sqlite::memory:") {
		return ":memory:", nil
	}
	if strings.HasPrefix(dsn, "sqlite://") {
		return strings.TrimPrefix(dsn, "sqlite://"), nil
	}
	if strings.HasPrefix(dsn, "sqlite:") {
		return strings.TrimPrefix(dsn, "sqlite:"), nil
	}
	if _, err := url.Parse(dsn); err != nil {
		return "", fmt.Errorf("invalid DSN: %w", err)
	}
	return dsn, nil
}

func (r *Repo) Migrate(ctx context.Context) error {
	return inventory.RunMigrations(ctx, r.db, inventory.FlavorSQLite)
}

func (r *Repo) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *Repo) Close() error { return r.db.Close() }

// Verify the Repo satisfies the inventory contract at compile time.
var _ inventory.Repo = (*Repo)(nil)

func (r *Repo) Orgs() inventory.OrgRepo                           { return &orgRepo{db: r.db} }
func (r *Repo) Users() inventory.UserRepo                         { return errUsers{} }
func (r *Repo) Projects() inventory.ProjectRepo                   { return &projectRepo{db: r.db} }
func (r *Repo) Stacks() inventory.StackRepo                       { return &stackRepo{db: r.db} }
func (r *Repo) Components() inventory.ComponentRepo               { return &componentRepo{db: r.db} }
func (r *Repo) Compositions() inventory.CompositionRepo           { return errCompositions{} }
func (r *Repo) Deployments() inventory.DeploymentRepo             { return &deploymentRepo{db: r.db} }
func (r *Repo) DeploymentTargets() inventory.DeploymentTargetRepo { return &targetRepo{db: r.db} }
func (r *Repo) Runs() inventory.RunRepo                           { return &runRepo{db: r.db} }
func (r *Repo) RunLogs() inventory.RunLogRepo                     { return errRunLogs{} }
func (r *Repo) DriftStatus() inventory.DriftStatusRepo            { return &driftRepo{db: r.db} }
func (r *Repo) CostEstimates() inventory.CostEstimateRepo         { return errCostEst{} }
func (r *Repo) CostActuals() inventory.CostActualRepo             { return errCostAct{} }
func (r *Repo) SecretsRefs() inventory.SecretsRefRepo             { return errSecrets{} }
func (r *Repo) AuditLog() inventory.AuditLogRepo                  { return errAudit{} }
