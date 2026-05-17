package provisioner_test

import (
	"context"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/tofu"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// captureAdapter wraps a FakeAdapter and records the target it sees.
type captureAdapter struct {
	*cloud.FakeAdapter
	captured     ir.DeploymentTarget
	capturedRefs cloud.ResolvedRefs
}

func (c *captureAdapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	c.captured = target
	c.capturedRefs = refs
	return c.FakeAdapter.Emit(ctx, target, refs)
}

// Forward the rest of the methods to the embedded FakeAdapter explicitly so
// the wrapper satisfies cloud.Adapter (Go embedded interface satisfaction
// works through promotion, but Emit needs the wrapper's override).
func (c *captureAdapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	return c.FakeAdapter.Profile(ctx, p)
}
func (c *captureAdapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
	return c.FakeAdapter.PricingKey(ctx, p)
}
func (c *captureAdapter) BillingQuery(ctx context.Context, creds cloud.Credentials, since, until time.Time) (cloud.BillingQueryParams, error) {
	return c.FakeAdapter.BillingQuery(ctx, creds, since, until)
}
func (c *captureAdapter) FetchBilling(ctx context.Context, creds cloud.Credentials, p cloud.BillingQueryParams) ([]cloud.NormalizedCostRow, error) {
	return c.FakeAdapter.FetchBilling(ctx, creds, p)
}
func (c *captureAdapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
	return c.FakeAdapter.DefaultStateBackend(ctx, target)
}
func (c *captureAdapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, creds cloud.Credentials) (map[string]any, error) {
	return c.FakeAdapter.ProviderBlock(ctx, target, creds)
}
func (c *captureAdapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
	return c.FakeAdapter.Validate(ctx, target)
}

func TestPlan_StuffsComponentTypeIntoSpec(t *testing.T) {
	capa := &captureAdapter{FakeAdapter: cloud.NewFakeAdapter("aws")}
	reg := cloud.NewRegistry()
	if err := reg.Register(capa); err != nil {
		t.Fatalf("register: %v", err)
	}

	p, _ := provisioner.New(provisioner.Config{
		WorkRoot: t.TempDir(),
		Adapters: reg,
		Runner:   tofu.NewFakeRunner(),
	})
	project := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1, Name: "x",
		Stacks: map[string]ir.Stack{"dev": {Name: "dev", StateBackend: ir.StateBackend{Kind: "local"}}},
		Components: []ir.Component{{
			Name: "orders-db", Type: "database",
			Spec:    map[string]any{"engine": "postgres"},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}},
		}},
	}
	_, err := p.Plan(context.Background(), provisioner.PlanInput{
		Project: project, Stack: "dev", OrgID: "local", DeploymentID: "dep-t",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if capa.captured.Spec["__type"] != "database" {
		t.Errorf("__type = %v, want \"database\"", capa.captured.Spec["__type"])
	}
	if capa.captured.Spec["__component"] != "orders-db" {
		t.Errorf("__component = %v", capa.captured.Spec["__component"])
	}
}
