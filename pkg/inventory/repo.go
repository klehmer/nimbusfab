// Package inventory defines the repository interfaces over the inventory DB.
// Two implementations exist: SQLite (for solo CLI use) and Postgres (for the
// web backend and the future GitOps daemon). Every row-bearing entity carries
// org_id so multi-tenant SaaS lands without schema migration.
package inventory

import (
	"context"
	"time"
)

// Repo is the union interface that the engine depends on. Implementations
// satisfy all sub-repos.
type Repo interface {
	Orgs() OrgRepo
	Users() UserRepo
	ApiTokens() ApiTokenRepo
	Projects() ProjectRepo
	Stacks() StackRepo
	Components() ComponentRepo
	Compositions() CompositionRepo
	Deployments() DeploymentRepo
	DeploymentTargets() DeploymentTargetRepo
	Runs() RunRepo
	RunLogs() RunLogRepo
	DriftStatus() DriftStatusRepo
	CostEstimates() CostEstimateRepo
	CostActuals() CostActualRepo
	SecretsRefs() SecretsRefRepo
	AuditLog() AuditLogRepo

	// Migrate applies any outstanding schema migrations. Idempotent.
	Migrate(ctx context.Context) error

	// Ping is a liveness probe for the underlying DB connection.
	Ping(ctx context.Context) error

	// Close releases connections / file handles.
	Close() error
}

// Org is a tenant root. SQLite installations have exactly one row.
type Org struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// OrgRepo manages Org rows.
type OrgRepo interface {
	Get(ctx context.Context, id string) (*Org, error)
	List(ctx context.Context) ([]Org, error)
	Create(ctx context.Context, o Org) error
}

// User is a local or OIDC-linked user.
type User struct {
	ID           string
	OrgID        string
	Email        string
	DisplayName  string
	IsLocal      bool
	OIDCProvider string
	OIDCSubject  string
	PasswordHash []byte // bcrypt; nil for OIDC-only users
	CreatedAt    time.Time
}

// UserRepo manages User rows.
type UserRepo interface {
	Get(ctx context.Context, orgID, id string) (*User, error)
	GetByEmail(ctx context.Context, orgID, email string) (*User, error)
	Create(ctx context.Context, u User) error
	UpdatePasswordHash(ctx context.Context, orgID, id string, hash []byte) error
}

// ApiToken is a personal access token (PAT) for API auth. The plaintext
// token is never stored — only its argon2id hash. Prefix is the short
// public portion used for efficient lookup.
type ApiToken struct {
	ID         string
	OrgID      string
	UserID     string
	Prefix     string // 8 chars; URL-safe; unique per row
	TokenHash  []byte // argon2id over the secret part
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// ApiTokenRepo manages ApiToken rows (PATs).
type ApiTokenRepo interface {
	Create(ctx context.Context, t ApiToken) error
	GetByPrefix(ctx context.Context, prefix string) (*ApiToken, error)
	ListByUser(ctx context.Context, orgID, userID string) ([]ApiToken, error)
	UpdateLastUsed(ctx context.Context, id string, t time.Time) error
	Revoke(ctx context.Context, orgID, id string) error
}

// Project is a user's project — one directory of YAML, one inventory scope.
type Project struct {
	ID        string
	OrgID     string
	Name      string
	SourceURI string // git URL, local path, etc.
	CreatedAt time.Time
}

// ProjectRepo manages Project rows.
type ProjectRepo interface {
	Get(ctx context.Context, orgID, id string) (*Project, error)
	List(ctx context.Context, orgID string) ([]Project, error)
	Create(ctx context.Context, p Project) error
}

// Stack is a named environment within a project.
type Stack struct {
	ID               string
	OrgID            string
	ProjectID        string
	Name             string
	StateBackendKind string
	StateBackendCfg  []byte // JSON
}

// StackRepo manages Stack rows.
type StackRepo interface {
	Get(ctx context.Context, orgID, id string) (*Stack, error)
	GetByName(ctx context.Context, orgID, projectID, name string) (*Stack, error)
	List(ctx context.Context, orgID, projectID string) ([]Stack, error)
	Upsert(ctx context.Context, s Stack) error
}

// Component is the latest declared IR snapshot of one component for one
// (project, stack). Full IR JSON is stored for audit and replay.
type Component struct {
	ID        string
	OrgID     string
	ProjectID string
	StackID   string
	Name      string
	Type      string
	IRJSON    []byte
	UpdatedAt time.Time
}

// ComponentRepo manages Component rows.
type ComponentRepo interface {
	Get(ctx context.Context, orgID, id string) (*Component, error)
	ListByStack(ctx context.Context, orgID, projectID, stackID string) ([]Component, error)
	Upsert(ctx context.Context, c Component) error
}

// CompositionRecord stores a user-defined Composition.
type CompositionRecord struct {
	ID        string
	OrgID     string
	ProjectID string
	Kind      string
	IRJSON    []byte
	UpdatedAt time.Time
}

// CompositionRepo manages CompositionRecord rows.
type CompositionRepo interface {
	ListByProject(ctx context.Context, orgID, projectID string) ([]CompositionRecord, error)
	Upsert(ctx context.Context, c CompositionRecord) error
}

// Deployment is one Engine.Apply invocation.
type Deployment struct {
	ID                   string
	OrgID                string
	ProjectID            string
	StackID              string
	RequestedByUserID    string
	Status               string // "running" | "succeeded" | "failed" | "partial_failure"
	PartialFailurePolicy string
	StartedAt            time.Time
	FinishedAt           *time.Time
}

// DeploymentRepo manages Deployment rows.
type DeploymentRepo interface {
	Get(ctx context.Context, orgID, id string) (*Deployment, error)
	Create(ctx context.Context, d Deployment) error
	UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error
	ListByProject(ctx context.Context, orgID, projectID string, limit int) ([]Deployment, error)
}

// DeploymentTarget is per-(deployment, cloud, region).
type DeploymentTarget struct {
	ID            string
	OrgID         string
	DeploymentID  string
	ComponentName string
	Cloud         string
	Region        string
	CredentialRef string
	WorkspacePath string
	PlanFile      string // path to saved tofu plan binary; used by Apply-by-ID
	StateBackend  []byte // JSON
	Status        string
	StartedAt     time.Time
	FinishedAt    *time.Time
}

// DeploymentTargetRepo manages DeploymentTarget rows.
type DeploymentTargetRepo interface {
	Get(ctx context.Context, orgID, id string) (*DeploymentTarget, error)
	ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]DeploymentTarget, error)
	Create(ctx context.Context, t DeploymentTarget) error
	UpdateStatus(ctx context.Context, orgID, id, status string, finishedAt *time.Time) error
}

// Run is a single `tofu` invocation.
type Run struct {
	ID                 string
	OrgID              string
	DeploymentTargetID string
	Kind               string // "plan" | "apply" | "destroy"
	Status             string
	ExitCode           int
	StartedAt          time.Time
	FinishedAt         *time.Time
	UserID             string
}

// RunRepo manages Run rows.
type RunRepo interface {
	Get(ctx context.Context, orgID, id string) (*Run, error)
	ListByDeploymentTarget(ctx context.Context, orgID, dtID string) ([]Run, error)
	Create(ctx context.Context, r Run) error
	UpdateStatus(ctx context.Context, orgID, id, status string, exitCode int, finishedAt *time.Time) error
}

// RunLogLine is a single streamed log line.
type RunLogLine struct {
	RunID     string
	OrgID     string
	Seq       int64
	Timestamp time.Time
	Stream    string // "stdout" | "stderr" | "event"
	Body      string
}

// RunLogRepo manages RunLogLine rows. Server-mode implementations may
// off-load older logs to object storage; the interface stays the same.
type RunLogRepo interface {
	Append(ctx context.Context, lines []RunLogLine) error
	Read(ctx context.Context, orgID, runID string, sinceSeq int64) ([]RunLogLine, error)
}

// DriftRecord is the most recent drift summary for one deployment target.
type DriftRecord struct {
	DeploymentTargetID string
	OrgID              string
	DetectedAt         time.Time
	HasDrift           bool
	SummaryJSON        []byte
}

// DriftStatusRepo manages DriftRecord rows (upsert-by-target).
type DriftStatusRepo interface {
	Get(ctx context.Context, orgID, dtID string) (*DriftRecord, error)
	Upsert(ctx context.Context, d DriftRecord) error
	// ListByOrg returns every drift record for the org, newest detected
	// first. Used by the drift overview UI / API.
	ListByOrg(ctx context.Context, orgID string) ([]DriftRecord, error)
}

// CostEstimate is one estimated line item.
type CostEstimate struct {
	RunID          string
	OrgID          string
	PrimitiveID    string
	Currency       string
	UnitPrice      float64
	Units          float64
	UnitOfMeasure  string
	Subtotal       float64
	PricingKeyJSON []byte
}

// CostEstimateRepo manages CostEstimate rows.
type CostEstimateRepo interface {
	BulkInsert(ctx context.Context, items []CostEstimate) error
	ListByRun(ctx context.Context, orgID, runID string) ([]CostEstimate, error)
	// ListByDeployment returns every estimate attached to any run of any
	// target of the given deployment — JOINs runs → deployment_targets to
	// filter. Used by the per-deployment cost view.
	ListByDeployment(ctx context.Context, orgID, deploymentID string) ([]CostEstimate, error)
}

// CostActual is one normalized actual-cost row.
type CostActual struct {
	OrgID       string
	Cloud       string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Service     string
	ResourceID  string
	Region      string
	Amount      float64
	Currency    string
	TagsJSON    []byte
	TagSetHash  string
}

// CostActualRepo manages CostActual rows. Aggregations live in higher layers.
type CostActualRepo interface {
	Upsert(ctx context.Context, rows []CostActual) error
	Query(ctx context.Context, q CostActualQuery) ([]CostActual, error)
}

// CostActualQuery selects cost_actuals rows.
type CostActualQuery struct {
	OrgID  string
	Cloud  string
	Since  time.Time
	Until  time.Time
	Filter map[string]string
}

// SecretsRef maps a logical credential name to a pluggable backend descriptor.
// No secret values are stored here.
type SecretsRef struct {
	Name        string
	OrgID       string
	BackendKind string
	BackendCfg  []byte // JSON
}

// SecretsRefRepo manages SecretsRef rows.
type SecretsRefRepo interface {
	Get(ctx context.Context, orgID, name string) (*SecretsRef, error)
	List(ctx context.Context, orgID string) ([]SecretsRef, error)
	Upsert(ctx context.Context, r SecretsRef) error
	Delete(ctx context.Context, orgID, name string) error
}

// AuditEntry is one append-only audit-log row.
type AuditEntry struct {
	OrgID       string
	ActorUserID string
	Verb        string
	Target      string
	PayloadJSON []byte
	Timestamp   time.Time
}

// AuditLogRepo appends and queries the audit log.
type AuditLogRepo interface {
	Append(ctx context.Context, e AuditEntry) error
	Query(ctx context.Context, orgID string, since, until time.Time, limit int) ([]AuditEntry, error)
}
