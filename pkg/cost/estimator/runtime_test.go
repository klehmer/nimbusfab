package estimator_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/cost/estimator"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

// fakeProvider is a deterministic in-memory PricingProvider for unit tests.
type fakeProvider struct {
	prices map[string]estimator.UnitPrice
}

func (f *fakeProvider) Price(ctx context.Context, cloudName string, key map[string]any) (estimator.UnitPrice, error) {
	k := cloudName
	if t, ok := key["instanceType"].(string); ok {
		k += "/" + t
	}
	if e, ok := f.prices[k]; ok {
		return e, nil
	}
	return estimator.UnitPrice{}, fmt.Errorf("fakeProvider: no price for %s", k)
}

// fakeAdapter satisfies cloud.Adapter just enough for the estimator.
// Returns the primitive's "fake-pricing-key" attribute as the key.
type fakeAdapter struct{}

func (fakeAdapter) Name() string                                             { return "aws" }
func (fakeAdapter) SupportedAPIVersions() []string                           { return []string{ir.APIVersionV1Alpha1} }
func (fakeAdapter) SupportedComponentTypes() []string                        { return []string{"network"} }
func (fakeAdapter) TierOneSchema() []byte                                    { return nil }
func (fakeAdapter) Validate(context.Context, ir.DeploymentTarget) []ir.Issue { return nil }
func (fakeAdapter) Emit(context.Context, ir.DeploymentTarget, cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, nil
}
func (fakeAdapter) Profile(context.Context, ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	return parity.ResourceProfile{}, nil
}
func (fakeAdapter) PricingKey(_ context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
	if p.TofuType == "aws_vpc" {
		// Free primitive.
		return nil, nil
	}
	return map[string]any{"instanceType": p.Attributes["instance_type"]}, nil
}
func (fakeAdapter) BillingQuery(context.Context, cloud.Credentials, time.Time, time.Time) (cloud.BillingQueryParams, error) {
	return nil, nil
}
func (fakeAdapter) FetchBilling(context.Context, cloud.Credentials, cloud.BillingQueryParams) ([]cloud.NormalizedCostRow, error) {
	return nil, nil
}
func (fakeAdapter) DefaultStateBackend(context.Context, ir.DeploymentTarget) (ir.StateBackend, error) {
	return ir.StateBackend{}, nil
}
func (fakeAdapter) ProviderBlock(context.Context, ir.DeploymentTarget, cloud.Credentials) (map[string]any, error) {
	return nil, nil
}
func (fakeAdapter) OutputBindings(context.Context, ir.DeploymentTarget, []ir.ResourcePrimitive) (map[string]string, error) {
	return map[string]string{}, nil
}

func TestEstimate_EmptyInput(t *testing.T) {
	est := estimator.New(&fakeProvider{})
	out, err := est.Estimate(context.Background(), estimator.EstimateInput{})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if out.Total != 0 {
		t.Errorf("Total = %v, want 0", out.Total)
	}
}

func TestEstimate_OneInstance_MonthlyCost(t *testing.T) {
	prov := &fakeProvider{prices: map[string]estimator.UnitPrice{
		"aws/t3.medium": {UnitPrice: 0.0416, UnitOfMeasure: "Hrs", Currency: "USD", Source: "snapshot"},
	}}
	est := estimator.New(prov)
	in := estimator.EstimateInput{Targets: []estimator.TargetInput{{
		DeploymentTargetID: "tgt-1", Cloud: "aws", Region: "us-east-1",
		Adapter: fakeAdapter{},
		Primitives: []ir.ResourcePrimitive{{
			ID: "web.aws-us-east-1.instance_0", TofuType: "aws_instance",
			Attributes: map[string]any{"instance_type": "t3.medium"},
		}},
	}}}
	out, _ := est.Estimate(context.Background(), in)
	want := 0.0416 * estimator.HoursPerMonth
	if abs(out.Total-want) > 0.001 {
		t.Errorf("Total = %v, want %v", out.Total, want)
	}
	if len(out.Targets) != 1 || len(out.Targets[0].Primitives) != 1 {
		t.Fatalf("expected 1 target + 1 primitive: %+v", out)
	}
	if abs(out.Targets[0].Subtotal-want) > 0.001 {
		t.Errorf("Target.Subtotal = %v, want %v", out.Targets[0].Subtotal, want)
	}
}

func TestEstimate_MissingPricing_RecordsWarning(t *testing.T) {
	prov := &fakeProvider{prices: map[string]estimator.UnitPrice{}}
	est := estimator.New(prov)
	out, _ := est.Estimate(context.Background(), estimator.EstimateInput{Targets: []estimator.TargetInput{{
		Cloud: "aws", Adapter: fakeAdapter{},
		Primitives: []ir.ResourcePrimitive{{
			ID: "x", TofuType: "aws_instance", Attributes: map[string]any{"instance_type": "exotic"},
		}},
	}}})
	if out.Total != 0 {
		t.Errorf("expected total = 0 when pricing missing, got %v", out.Total)
	}
	if len(out.Warnings) == 0 {
		t.Error("expected a warning")
	}
}

func TestEstimate_FreePrimitive_Skipped(t *testing.T) {
	prov := &fakeProvider{}
	est := estimator.New(prov)
	out, _ := est.Estimate(context.Background(), estimator.EstimateInput{Targets: []estimator.TargetInput{{
		Cloud: "aws", Adapter: fakeAdapter{},
		Primitives: []ir.ResourcePrimitive{{
			ID: "vpc", TofuType: "aws_vpc",
		}},
	}}})
	if out.Total != 0 {
		t.Errorf("expected 0 total for VPC-only, got %v", out.Total)
	}
	if len(out.Warnings) != 0 {
		t.Errorf("expected no warnings for free primitive, got %v", out.Warnings)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
