package inventory

import (
	"context"
	"time"
)

// NewNullRepo returns a Repo whose writes are no-ops and whose reads return
// ErrInventoryRequired. Used by the engine in --no-inventory mode.
func NewNullRepo() Repo { return nullRepo{} }

type nullRepo struct{}

// isNullRepo marks this implementation; the engine uses it to detect
// no-inventory mode without importing internal types.
func (nullRepo) isNullRepo() bool { return true }

// IsNullRepo reports whether r is the no-inventory implementation.
func IsNullRepo(r Repo) bool {
	x, ok := r.(interface{ isNullRepo() bool })
	return ok && x.isNullRepo()
}

func (nullRepo) Orgs() OrgRepo                           { return nullOrgs{} }
func (nullRepo) Users() UserRepo                         { return nullUsers{} }
func (nullRepo) Projects() ProjectRepo                   { return nullProjects{} }
func (nullRepo) Stacks() StackRepo                       { return nullStacks{} }
func (nullRepo) Components() ComponentRepo               { return nullComponents{} }
func (nullRepo) Compositions() CompositionRepo           { return nullCompositions{} }
func (nullRepo) Deployments() DeploymentRepo             { return nullDeployments{} }
func (nullRepo) DeploymentTargets() DeploymentTargetRepo { return nullTargets{} }
func (nullRepo) Runs() RunRepo                           { return nullRuns{} }
func (nullRepo) RunLogs() RunLogRepo                     { return nullRunLogs{} }
func (nullRepo) DriftStatus() DriftStatusRepo            { return nullDrift{} }
func (nullRepo) CostEstimates() CostEstimateRepo         { return nullCostEst{} }
func (nullRepo) CostActuals() CostActualRepo             { return nullCostAct{} }
func (nullRepo) SecretsRefs() SecretsRefRepo             { return nullSecrets{} }
func (nullRepo) AuditLog() AuditLogRepo                  { return nullAudit{} }
func (nullRepo) Migrate(ctx context.Context) error       { return nil }
func (nullRepo) Ping(ctx context.Context) error          { return nil }
func (nullRepo) Close() error                            { return nil }

type nullOrgs struct{}

func (nullOrgs) Get(ctx context.Context, id string) (*Org, error) { return nil, ErrInventoryRequired }
func (nullOrgs) List(ctx context.Context) ([]Org, error)          { return nil, ErrInventoryRequired }
func (nullOrgs) Create(ctx context.Context, o Org) error          { return nil }

type nullUsers struct{}

func (nullUsers) Get(ctx context.Context, orgID, id string) (*User, error) {
	return nil, ErrInventoryRequired
}
func (nullUsers) GetByEmail(ctx context.Context, orgID, email string) (*User, error) {
	return nil, ErrInventoryRequired
}
func (nullUsers) Create(ctx context.Context, u User) error { return nil }

type nullProjects struct{}

func (nullProjects) Get(ctx context.Context, orgID, id string) (*Project, error) {
	return nil, ErrInventoryRequired
}
func (nullProjects) List(ctx context.Context, orgID string) ([]Project, error) {
	return nil, ErrInventoryRequired
}
func (nullProjects) Create(ctx context.Context, p Project) error { return nil }

type nullStacks struct{}

func (nullStacks) Get(ctx context.Context, orgID, id string) (*Stack, error) {
	return nil, ErrInventoryRequired
}
func (nullStacks) GetByName(ctx context.Context, orgID, projectID, name string) (*Stack, error) {
	return nil, ErrInventoryRequired
}
func (nullStacks) List(ctx context.Context, orgID, projectID string) ([]Stack, error) {
	return nil, ErrInventoryRequired
}
func (nullStacks) Upsert(ctx context.Context, s Stack) error { return nil }

type nullComponents struct{}

func (nullComponents) Get(ctx context.Context, orgID, id string) (*Component, error) {
	return nil, ErrInventoryRequired
}
func (nullComponents) ListByStack(ctx context.Context, orgID, projectID, stackID string) ([]Component, error) {
	return nil, ErrInventoryRequired
}
func (nullComponents) Upsert(ctx context.Context, c Component) error { return nil }

type nullCompositions struct{}

func (nullCompositions) ListByProject(ctx context.Context, orgID, projectID string) ([]CompositionRecord, error) {
	return nil, ErrInventoryRequired
}
func (nullCompositions) Upsert(ctx context.Context, c CompositionRecord) error { return nil }

type nullDeployments struct{}

func (nullDeployments) Get(ctx context.Context, orgID, id string) (*Deployment, error) {
	return nil, ErrInventoryRequired
}
func (nullDeployments) Create(ctx context.Context, d Deployment) error { return nil }
func (nullDeployments) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error {
	return nil
}
func (nullDeployments) ListByProject(ctx context.Context, orgID, projectID string, limit int) ([]Deployment, error) {
	return nil, ErrInventoryRequired
}

type nullTargets struct{}

func (nullTargets) Get(ctx context.Context, orgID, id string) (*DeploymentTarget, error) {
	return nil, ErrInventoryRequired
}
func (nullTargets) ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]DeploymentTarget, error) {
	return nil, ErrInventoryRequired
}
func (nullTargets) Create(ctx context.Context, t DeploymentTarget) error { return nil }
func (nullTargets) UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error {
	return nil
}

type nullRuns struct{}

func (nullRuns) Get(ctx context.Context, orgID, id string) (*Run, error) {
	return nil, ErrInventoryRequired
}
func (nullRuns) ListByDeploymentTarget(ctx context.Context, orgID, dtID string) ([]Run, error) {
	return nil, ErrInventoryRequired
}
func (nullRuns) Create(ctx context.Context, r Run) error { return nil }
func (nullRuns) UpdateStatus(ctx context.Context, orgID, id, status string, exitCode int, finishedAt *time.Time) error {
	return nil
}

type nullRunLogs struct{}

func (nullRunLogs) Append(ctx context.Context, lines []RunLogLine) error { return nil }
func (nullRunLogs) Read(ctx context.Context, orgID, runID string, sinceSeq int64) ([]RunLogLine, error) {
	return nil, ErrInventoryRequired
}

type nullDrift struct{}

func (nullDrift) Get(ctx context.Context, orgID, dtID string) (*DriftRecord, error) {
	return nil, ErrInventoryRequired
}
func (nullDrift) Upsert(ctx context.Context, d DriftRecord) error { return nil }

type nullCostEst struct{}

func (nullCostEst) BulkInsert(ctx context.Context, items []CostEstimate) error { return nil }
func (nullCostEst) ListByRun(ctx context.Context, orgID, runID string) ([]CostEstimate, error) {
	return nil, ErrInventoryRequired
}

type nullCostAct struct{}

func (nullCostAct) Upsert(ctx context.Context, rows []CostActual) error { return nil }
func (nullCostAct) Query(ctx context.Context, q CostActualQuery) ([]CostActual, error) {
	return nil, ErrInventoryRequired
}

type nullSecrets struct{}

func (nullSecrets) Get(ctx context.Context, orgID, name string) (*SecretsRef, error) {
	return nil, ErrInventoryRequired
}
func (nullSecrets) List(ctx context.Context, orgID string) ([]SecretsRef, error) {
	return nil, ErrInventoryRequired
}
func (nullSecrets) Upsert(ctx context.Context, r SecretsRef) error { return nil }
func (nullSecrets) Delete(ctx context.Context, orgID, name string) error { return nil }

type nullAudit struct{}

func (nullAudit) Append(ctx context.Context, e AuditEntry) error { return nil }
func (nullAudit) Query(ctx context.Context, orgID string, since, until time.Time, limit int) ([]AuditEntry, error) {
	return nil, ErrInventoryRequired
}
