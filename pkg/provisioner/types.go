// Package provisioner takes a validated *ir.Project, asks cloud adapters
// to emit ResourcePrimitives for each DeploymentTarget, materializes a
// canonical OpenTofu workspace per target, and drives the Tofu runner.
//
// The provisioner is the only package that imports both pkg/cloud and
// internal/tofu; this contains the dependency-cycle by design.
package provisioner

import (
	"time"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// PartialFailurePolicy selects how a multi-target Apply handles per-target
// failures. Phase 1 only exercises Plan, but the constant is locked in here
// because PlanInput records the policy that the eventual Apply will honor.
type PartialFailurePolicy string

const (
	PartialFailureLeave       PartialFailurePolicy = "leave"
	PartialFailureRollback    PartialFailurePolicy = "rollback"
	PartialFailureRetryFailed PartialFailurePolicy = "retry-failed"
)

// TargetFilter restricts a Plan/Apply to a subset of DeploymentTargets.
// Empty filter means "all targets in the validated project".
type TargetFilter struct {
	Component string
	Cloud     string
	Region    string
}

// PlanInput is what the engine hands the provisioner.
type PlanInput struct {
	Project        *ir.Project
	Stack          string
	OrgID          string
	DeploymentID   string
	PartialFailure PartialFailurePolicy
	Refresh        bool
	Targets        []TargetFilter
}

// PlanResult is what the provisioner returns to the engine.
type PlanResult struct {
	DeploymentID   string
	Stack          string
	PartialFailure PartialFailurePolicy
	Targets        []TargetPlan
	HasChanges     bool
	Diagnostics    []Diagnostic
	GeneratedAt    time.Time
}

// TargetPlan is one (component, cloud, region) plan slice.
type TargetPlan struct {
	DeploymentTargetID string
	Component          string
	Cloud              string
	Region             string
	WorkspaceDir       string
	PrimitiveCount     int
	PlanFile           string
	JSONPlanPath       string
	HasChanges         bool
	Adds               int
	Changes            int
	Destroys           int
	Tags               map[string]string
}

// Diagnostic is a non-fatal note attached to a PlanResult. Errors are
// returned through the error return; diagnostics are warnings or info.
type Diagnostic struct {
	Severity string
	Code     string
	Message  string
	Target   string
}

// RunStatus discriminates per-target lifecycle outcomes.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusSkipped   RunStatus = "skipped"
	RunStatusReverted  RunStatus = "reverted"
)

// ApplyStatus discriminates the overall outcome of an Apply call.
type ApplyStatus string

const (
	ApplySucceeded      ApplyStatus = "succeeded"
	ApplyPartialFailure ApplyStatus = "partial_failure"
	ApplyFailed         ApplyStatus = "failed"
	ApplyRollbackFailed ApplyStatus = "rollback_failed"
)

// ApplyInput is what the engine hands the provisioner for Apply.
type ApplyInput struct {
	PlanResult            *PlanResult
	OrgID                 string
	PartialFailure        PartialFailurePolicy
	AutoApprove           bool
	AllowParityViolations bool
	MaxRetries            int
	MaxConcurrentTargets  int
	MaxConcurrentPerCloud int
	EventSink             chan<- RunEvent
}

// ApplyResult is what the provisioner returns.
type ApplyResult struct {
	DeploymentID  string
	Status        ApplyStatus
	TargetResults []TargetApplyResult
	GeneratedAt   time.Time
}

// TargetApplyResult is one target's outcome.
type TargetApplyResult struct {
	DeploymentTargetID string
	Component          string
	Cloud              string
	Region             string
	RunID              string
	Status             RunStatus
	Outputs            map[string]any
	State              *StateSnapshot
	Error              error
	StartedAt          time.Time
	FinishedAt         time.Time
}

// DestroyInput targets an existing deployment for teardown.
type DestroyInput struct {
	PlanResult            *PlanResult
	DeploymentID          string
	Stack                 string
	Project               *ir.Project
	OrgID                 string
	PartialFailure        PartialFailurePolicy
	AutoApprove           bool
	MaxConcurrentTargets  int
	MaxConcurrentPerCloud int
	EventSink             chan<- RunEvent
}

// RunEvent is one streamed update produced by per-target work.
type RunEvent struct {
	Timestamp          time.Time
	DeploymentTargetID string
	Component          string
	Cloud              string
	Region             string
	Kind               RunEventKind
	Message            string
	Raw                map[string]any
}

// RunEventKind classifies a RunEvent.
type RunEventKind string

const (
	RunEventStart      RunEventKind = "start"
	RunEventLog        RunEventKind = "log"
	RunEventDiagnostic RunEventKind = "diagnostic"
	RunEventProgress   RunEventKind = "progress"
	RunEventSuccess    RunEventKind = "success"
	RunEventFailure    RunEventKind = "failure"
	RunEventSkip       RunEventKind = "skip"
	RunEventTerminal   RunEventKind = "terminal"
)

// StateSnapshot captures a parsed `tofu show -json` state.
type StateSnapshot struct {
	DeploymentTargetID string
	TofuVersion        string
	SerialNumber       int64
	Resources          []StateResource
	Outputs            map[string]any
	CapturedAt         time.Time
}

// StateResource is one entry from a parsed Tofu state.
type StateResource struct {
	Address         string
	Type            string
	Name            string
	CloudResourceID string
	AttributesHash  string
	Attributes      map[string]any
}

// DriftInput is what DetectDrift takes.
type DriftInput struct {
	PlanResult            *PlanResult
	DeploymentID          string
	Stack                 string
	Project               *ir.Project
	OrgID                 string
	MaxConcurrentTargets  int
	MaxConcurrentPerCloud int
	EventSink             chan<- RunEvent
}

// DriftReport is what DetectDrift returns.
type DriftReport struct {
	DeploymentID  string
	TargetReports []TargetDriftReport
	GeneratedAt   time.Time
}

// TargetDriftReport is per-target drift.
type TargetDriftReport struct {
	DeploymentTargetID string
	Component          string
	Cloud              string
	Region             string
	HasDrift           bool
	Drifted            []DriftedResource
	Gone               []DriftedResource
	Discovered         []DriftedResource
	Error              error
}

// DriftedResource describes one drifted Tofu address.
type DriftedResource struct {
	Address          string
	Kind             string
	AttributesBefore map[string]any
	AttributesAfter  map[string]any
	DiffSummary      string
}
