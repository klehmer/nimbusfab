// Package postgres implements pkg/inventory.Repo against Postgres via
// github.com/jackc/pgx/v5/stdlib. Mirrors internal/inventory/sqlite's
// structure (one file per table); query syntax adjusted for $N
// placeholders and Postgres-native types (TIMESTAMPTZ → time.Time
// directly, no string-parsing dance).
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// Repo is the Postgres Repo implementation.
type Repo struct {
	db *sql.DB
}

// Open returns a Postgres Repo. Accepts "postgres://..." or
// "postgresql://..." DSNs; pgx parses both.
func Open(dsn string) (*Repo, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	return &Repo{db: db}, nil
}

// Migrate runs the embedded migrations against the Postgres flavor.
func (r *Repo) Migrate(ctx context.Context) error {
	return inventory.RunMigrations(ctx, r.db, inventory.FlavorPostgres)
}

// Ping verifies the connection is alive.
func (r *Repo) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

// Close releases the connection pool.
func (r *Repo) Close() error { return r.db.Close() }

// Verify the Repo satisfies the inventory contract at compile time.
var _ inventory.Repo = (*Repo)(nil)

func (r *Repo) Orgs() inventory.OrgRepo                           { return &orgRepo{db: r.db} }
func (r *Repo) Users() inventory.UserRepo                         { return &userRepo{db: r.db} }
func (r *Repo) ApiTokens() inventory.ApiTokenRepo                 { return &apiTokenRepo{db: r.db} }
func (r *Repo) Projects() inventory.ProjectRepo                   { return &projectRepo{db: r.db} }
func (r *Repo) Stacks() inventory.StackRepo                       { return &stackRepo{db: r.db} }
func (r *Repo) Components() inventory.ComponentRepo               { return &componentRepo{db: r.db} }
func (r *Repo) Compositions() inventory.CompositionRepo           { return errCompositions{} }
func (r *Repo) Deployments() inventory.DeploymentRepo             { return &deploymentRepo{db: r.db} }
func (r *Repo) DeploymentTargets() inventory.DeploymentTargetRepo { return &targetRepo{db: r.db} }
func (r *Repo) Runs() inventory.RunRepo                           { return &runRepo{db: r.db} }
func (r *Repo) RunLogs() inventory.RunLogRepo                     { return errRunLogs{} }
func (r *Repo) DriftStatus() inventory.DriftStatusRepo            { return &driftRepo{db: r.db} }
func (r *Repo) CostEstimates() inventory.CostEstimateRepo         { return &costEstimateRepo{db: r.db} }
func (r *Repo) CostActuals() inventory.CostActualRepo             { return errCostAct{} }
func (r *Repo) SecretsRefs() inventory.SecretsRefRepo             { return errSecrets{} }
func (r *Repo) AuditLog() inventory.AuditLogRepo                  { return &auditRepo{db: r.db} }

// init registers the postgres scheme with the inventory dispatcher.
func init() {
	inventory.RegisterBackend("postgres", func(ctx context.Context, dsn string) (inventory.Repo, error) {
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
