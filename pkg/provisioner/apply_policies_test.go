package provisioner_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

func twoTargetProject() *ir.Project {
	return &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "x",
		Stacks:     map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{{
			Name: "web", Type: "network",
			Spec: map[string]any{"cidr": "10.0.0.0/16"},
			Targets: []ir.DeploymentTarget{
				{Cloud: "aws", Region: "us-east-1"},
				{Cloud: "aws", Region: "eu-west-1"},
			},
		}},
	}
}

// flakyRunner fails the first N apply calls then succeeds.
type flakyRunner struct {
	*tofu.FakeRunner
	mu        sync.Mutex
	failCount int
	callCount int
}

func newFlakyRunner(failCount int) *flakyRunner {
	return &flakyRunner{FakeRunner: tofu.NewFakeRunner(), failCount: failCount}
}

func (f *flakyRunner) Apply(ctx context.Context, ws tofu.Workspace, planFile string, opts tofu.ApplyOpts) error {
	f.mu.Lock()
	f.callCount++
	shouldFail := f.callCount <= f.failCount
	f.mu.Unlock()
	if shouldFail {
		return errors.New("scripted apply failure")
	}
	return f.FakeRunner.Apply(ctx, ws, planFile, opts)
}

func TestApply_LeavePolicy_PartialFailureRecorded(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := newFlakyRunner(1)
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

	planRes, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-l",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	res, err := p.Apply(context.Background(), provisioner.ApplyInput{
		PlanResult:     planRes,
		OrgID:          "local",
		PartialFailure: provisioner.PartialFailureLeave,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != provisioner.ApplyPartialFailure {
		t.Errorf("Status = %q, want partial_failure", res.Status)
	}
	var succeeded, failed int
	for _, r := range res.TargetResults {
		switch r.Status {
		case provisioner.RunStatusSucceeded:
			succeeded++
		case provisioner.RunStatusFailed:
			failed++
		}
	}
	if succeeded != 1 || failed != 1 {
		t.Errorf("succeeded=%d failed=%d, want 1 each", succeeded, failed)
	}
}

func TestApply_RetryFailedPolicy(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := newFlakyRunner(1)
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

	planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
		Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-r",
	})
	res, err := p.Apply(context.Background(), provisioner.ApplyInput{
		PlanResult:     planRes,
		OrgID:          "local",
		PartialFailure: provisioner.PartialFailureRetryFailed,
		MaxRetries:     2,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Status != provisioner.ApplySucceeded {
		t.Errorf("Status = %q, want succeeded after retry", res.Status)
	}
}

func TestApply_RollbackPolicy_DestroysSucceeded(t *testing.T) {
	reg := cloud.NewRegistry()
	_ = reg.Register(aws.New())
	runner := newFlakyRunner(1)
	p, _ := provisioner.New(provisioner.Config{WorkRoot: t.TempDir(), Adapters: reg, Runner: runner})

	planRes, _ := p.Plan(context.Background(), provisioner.PlanInput{
		Project: twoTargetProject(), Stack: "dev", OrgID: "local", DeploymentID: "dep-rb",
	})
	res, _ := p.Apply(context.Background(), provisioner.ApplyInput{
		PlanResult:     planRes,
		OrgID:          "local",
		PartialFailure: provisioner.PartialFailureRollback,
	})
	if res.Status != provisioner.ApplyFailed {
		t.Errorf("Status = %q, want failed (rolled back)", res.Status)
	}
	var reverted int
	for _, r := range res.TargetResults {
		if r.Status == provisioner.RunStatusReverted {
			reverted++
		}
	}
	if reverted != 1 {
		t.Errorf("reverted count = %d, want 1", reverted)
	}
	if len(runner.DestroyCalls) != 1 {
		t.Errorf("Destroy calls = %d, want 1 (the succeeded target)", len(runner.DestroyCalls))
	}
}
