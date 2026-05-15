package provisioner

import (
	"context"
	"errors"
)

// ErrNotImplementedYet is returned by Provisioner methods that have not yet
// been wired up. Phase 1 implements Plan only.
var ErrNotImplementedYet = errors.New("provisioner: not implemented yet")

// Provisioner orchestrates plan/apply/destroy across DeploymentTargets.
type Provisioner interface {
	Plan(ctx context.Context, in PlanInput) (*PlanResult, error)

	Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error)
	Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error)
}

// ApplyInput / ApplyResult / DestroyInput are reserved-shape stubs so that
// Phase 2 lands without a public-API churn commit.
type ApplyInput struct {
	PlanResult            *PlanResult
	OrgID                 string
	PartialFailure        PartialFailurePolicy
	AutoApprove           bool
	AllowParityViolations bool
}

type ApplyResult struct {
	DeploymentID  string
	TargetResults []TargetApplyResult
	Status        string
}

type TargetApplyResult struct {
	DeploymentTargetID string
	RunID              string
	Status             string
	Outputs            map[string]any
	Error              error
}

type DestroyInput struct {
	DeploymentID   string
	PartialFailure PartialFailurePolicy
	AutoApprove    bool
}

// Config carries the dependencies a real Provisioner needs.
type Config struct {
	WorkRoot string
}

// New returns a stub Provisioner. Task 9 replaces this with a real impl.
func New(cfg Config) (Provisioner, error) {
	return &stubProvisioner{}, nil
}

type stubProvisioner struct{}

func (*stubProvisioner) Plan(ctx context.Context, in PlanInput) (*PlanResult, error) {
	return nil, ErrNotImplementedYet
}
func (*stubProvisioner) Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error) {
	return nil, ErrNotImplementedYet
}
func (*stubProvisioner) Destroy(ctx context.Context, in DestroyInput) (*ApplyResult, error) {
	return nil, ErrNotImplementedYet
}
