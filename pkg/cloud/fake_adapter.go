package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

// FakeAdapter is a deterministic in-memory Adapter used by tests.
// It emits one ResourcePrimitive of type "fake_resource" per target.
type FakeAdapter struct {
	name string
}

// NewFakeAdapter returns a FakeAdapter with the given Name.
func NewFakeAdapter(name string) *FakeAdapter { return &FakeAdapter{name: name} }

func (f *FakeAdapter) Name() string                      { return f.name }
func (f *FakeAdapter) SupportedAPIVersions() []string    { return []string{ir.APIVersionV1Alpha1} }
func (f *FakeAdapter) SupportedComponentTypes() []string { return []string{"network"} }
func (f *FakeAdapter) TierOneSchema() []byte {
	return []byte(`{"type":"object","additionalProperties":true}`)
}
func (f *FakeAdapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
	return nil
}

func (f *FakeAdapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return []ir.ResourcePrimitive{{
		ID:       fmt.Sprintf("%s.%s.fake", f.name, target.Region),
		Cloud:    f.name,
		TofuType: "fake_resource",
		TofuName: "fake",
		Attributes: map[string]any{
			"region": target.Region,
		},
	}}, nil
}

func (f *FakeAdapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	return parity.ResourceProfile{}, ErrProfileUnavailable
}
func (f *FakeAdapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
	return map[string]any{"adapter": f.name}, nil
}
func (f *FakeAdapter) BillingQuery(ctx context.Context, _ Credentials, _, _ time.Time) (BillingQueryParams, error) {
	return BillingQueryParams{}, nil
}
func (f *FakeAdapter) FetchBilling(ctx context.Context, _ Credentials, _ BillingQueryParams) ([]NormalizedCostRow, error) {
	return nil, nil
}
func (f *FakeAdapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
	return ir.StateBackend{Kind: "local"}, nil
}
func (f *FakeAdapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, _ Credentials) (map[string]any, error) {
	return map[string]any{f.name: map[string]any{"region": target.Region}}, nil
}

func (a *FakeAdapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]any, error) {
	return map[string]any{}, nil
}

// Compile-time check that FakeAdapter satisfies Adapter.
var _ Adapter = (*FakeAdapter)(nil)
