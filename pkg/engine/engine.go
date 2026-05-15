// Package engine is the top-level surface that every frontend (CLI, web
// backend, future GitOps daemon) consumes. It wires together the subsystems
// (DSL, provisioner, cost, inventory) but does not itself contain domain
// logic; subsystems are isolated behind their own interfaces.
//
// Construct an Engine via New(cfg). Every method takes a context so callers
// can cancel long-running operations; Apply is async and returns a run ID
// that callers poll via GetRun or stream via StreamRun.
package engine

import (
	"context"
	"time"

	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// PlanResult is what Engine.Plan returns. Aliased to provisioner.PlanResult
// so the engine surface and the provisioner output stay in lockstep.
type PlanResult = provisioner.PlanResult

// TargetPlan mirrors provisioner.TargetPlan for the same reason.
type TargetPlan = provisioner.TargetPlan

// PartialFailurePolicy mirrors provisioner.PartialFailurePolicy.
type PartialFailurePolicy = provisioner.PartialFailurePolicy

const (
	PartialFailureLeave       = provisioner.PartialFailureLeave
	PartialFailureRollback    = provisioner.PartialFailureRollback
	PartialFailureRetryFailed = provisioner.PartialFailureRetryFailed
)

// Engine is the only interface a frontend should depend on.
type Engine interface {
	LoadProject(ctx context.Context, path string) (*ir.Project, error)
	Validate(ctx context.Context, project *ir.Project) (*ValidationReport, error)
	Plan(ctx context.Context, project *ir.Project, stack string, opts PlanOpts) (*PlanResult, error)
	Apply(ctx context.Context, planID string, opts ApplyOpts) (runID string, err error)
	Destroy(ctx context.Context, deploymentID string, opts DestroyOpts) (runID string, err error)
	Import(ctx context.Context, project *ir.Project, mapping ImportMap) (*ImportResult, error)
	GetRun(ctx context.Context, runID string) (*Run, error)
	StreamRun(ctx context.Context, runID string) (<-chan RunEvent, error)
	EstimateCost(ctx context.Context, plan *PlanResult) (*CostEstimate, error)
	GetCostActuals(ctx context.Context, query CostQuery) (*CostReport, error)
	DetectDrift(ctx context.Context, deploymentID string) (*DriftReport, error)
}

// ValidationReport collects all schema and semantic diagnostics for a Project.
//
// Deprecated: prefer ir.ValidationReport. The engine package will switch
// to that type when the validator subsystem lands (DSL/IR Phase 1 Task 9).
type ValidationReport struct {
	OK     bool
	Issues []Issue
}

// Issue is a single diagnostic returned by validation. Severity discriminates
// blocking errors from advisory warnings.
//
// Deprecated: prefer ir.Issue. The engine package will switch to that type
// when the validator subsystem lands (DSL/IR Phase 1 Task 9).
type Issue struct {
	Severity Severity
	Code     string
	Message  string
	Path     string // dotted IR path, e.g. "components[0].targets[1].region"
	Source   string // YAML file:line where available
}

// Severity classifies a diagnostic.
//
// Deprecated: prefer ir.Severity. The engine package will switch to that type
// when the validator subsystem lands (DSL/IR Phase 1 Task 9).
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// PlanOpts controls plan generation.
type PlanOpts struct {
	RefreshState   bool
	DetectDrift    bool
	Parallelism    int
	Targets        []provisioner.TargetFilter
	OutputDir      string
	PartialFailure PartialFailurePolicy
}

// ApplyOpts controls Apply behavior.
type ApplyOpts struct {
	AutoApprove    bool
	PartialFailure PartialFailurePolicy
	Detach         bool
	ActorUserID    string
}

// DestroyOpts controls Destroy.
type DestroyOpts struct {
	AutoApprove bool
	Detach      bool
	ActorUserID string
}

// ImportMap is opaque to the engine; cloud adapters interpret it. Roughly:
// {primitiveID -> cloud-native resource ARN/ID}.
type ImportMap map[string]string

// ImportResult reports per-primitive import outcomes.
type ImportResult struct {
	RunID    string
	Imported []string
	Skipped  []string
	Failed   map[string]string // primitiveID -> reason
}

// Run is a single tofu invocation as recorded in the inventory.
type Run struct {
	ID                 string
	DeploymentTargetID string
	Kind               RunKind
	Status             RunStatus
	ExitCode           int
	StartedAt          time.Time
	FinishedAt         *time.Time
	UserID             string
}

// RunKind discriminates plan / apply / destroy.
type RunKind string

const (
	RunKindPlan    RunKind = "plan"
	RunKindApply   RunKind = "apply"
	RunKindDestroy RunKind = "destroy"
)

// RunStatus discriminates lifecycle states.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// RunEvent is one streamed update for a run. StreamRun returns a channel of
// these; consumers must drain it. The channel closes when the run terminates.
type RunEvent struct {
	RunID     string
	Timestamp time.Time
	Kind      RunEventKind
	Message   string
	Data      map[string]any // tofu JSON diagnostic, drift summary, etc.
}

// RunEventKind classifies a RunEvent.
type RunEventKind string

const (
	RunEventLog        RunEventKind = "log"
	RunEventDiagnostic RunEventKind = "diagnostic"
	RunEventProgress   RunEventKind = "progress"
	RunEventStateLock  RunEventKind = "state-lock"
	RunEventTerminal   RunEventKind = "terminal"
)

// CostEstimate is a tree-shaped estimate broken down by target and primitive.
type CostEstimate struct {
	Currency string
	Period   string // "month" | "hour"
	Total    float64
	Targets  []TargetCostEstimate
	Warnings []string // missing pricing data, fallbacks used, etc.
}

// TargetCostEstimate breaks an estimate down per (component, cloud, region).
type TargetCostEstimate struct {
	DeploymentTargetID string
	Component          string
	Cloud              string
	Region             string
	Subtotal           float64
	Primitives         []PrimitiveCostEstimate
}

// PrimitiveCostEstimate is the leaf cost for a single primitive.
type PrimitiveCostEstimate struct {
	PrimitiveID   string
	PricingKey    map[string]any // adapter-supplied; opaque to the engine
	UnitPrice     float64
	Units         float64
	UnitOfMeasure string
	Subtotal      float64
}

// CostQuery selects actuals from the cost_actuals table.
type CostQuery struct {
	OrgID   string
	Since   time.Time
	Until   time.Time
	GroupBy []string // "cloud" | "service" | "component" | "region"
	Filter  map[string]string
}

// CostReport returns the result of a CostQuery.
type CostReport struct {
	Currency string
	Rows     []CostRow
}

// CostRow is one aggregated cost entry as shaped by CostQuery.GroupBy.
type CostRow struct {
	Group  map[string]string
	Period string
	Amount float64
}

// DriftReport summarizes detected drift for a deployment.
type DriftReport struct {
	DeploymentID string
	DetectedAt   time.Time
	Targets      []TargetDrift
}

// TargetDrift is per-target drift detail.
type TargetDrift struct {
	DeploymentTargetID string
	Cloud              string
	HasDrift           bool
	Primitives         []PrimitiveDrift
}

// PrimitiveDrift is the smallest drift unit: the IR thinks it should look
// like X, the cloud actually has Y.
type PrimitiveDrift struct {
	PrimitiveID string
	Field       string
	Expected    any
	Actual      any
}
