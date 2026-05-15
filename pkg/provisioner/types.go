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
