// Package aws implements pkg/cloud.Adapter for Amazon Web Services.
// Phase 1 supports only the `network` component type, emitting a single
// aws_vpc resource per DeploymentTarget. Phases 3+ extend this.
package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Adapter is the AWS implementation of cloud.Adapter.
type Adapter struct{}

// New returns a configured AWS Adapter.
func New() *Adapter { return &Adapter{} }

// Compile-time check that Adapter satisfies cloud.Adapter.
var _ cloud.Adapter = (*Adapter)(nil)

func (*Adapter) Name() string                      { return "aws" }
func (*Adapter) SupportedAPIVersions() []string    { return []string{ir.APIVersionV1Alpha1} }
func (*Adapter) SupportedComponentTypes() []string {
	return []string{"network", "compute", "database", "storage"}
}
func (*Adapter) TierOneSchema() []byte             { return tierOneSchema }

func (*Adapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
	if target.Region == "" {
		return []ir.Issue{{
			Severity: ir.SeverityError,
			Code:     "ErrAdapterAWSRegionMissing",
			Message:  "AWS targets must declare a region",
			Path:     "target.region",
		}}
	}
	return nil
}

func (*Adapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
	return ir.StateBackend{
		Kind: "s3",
		Config: map[string]any{
			"bucket":  "nimbusfab-state",
			"key":     fmt.Sprintf("aws/%s/terraform.tfstate", target.Region),
			"region":  target.Region,
			"encrypt": true,
		},
	}, nil
}

func (*Adapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, _ cloud.Credentials) (map[string]any, error) {
	return map[string]any{
		"aws": map[string]any{
			"region": target.Region,
		},
	}, nil
}

// Profile and PricingKey live in profile.go and pricing.go respectively.
// BillingQuery / FetchBilling remain stubs until the cost-collector phase.

func (*Adapter) BillingQuery(ctx context.Context, _ cloud.Credentials, _, _ time.Time) (cloud.BillingQueryParams, error) {
	return nil, cloud.ErrNotImplementedYet
}

func (*Adapter) FetchBilling(ctx context.Context, _ cloud.Credentials, _ cloud.BillingQueryParams) ([]cloud.NormalizedCostRow, error) {
	return nil, cloud.ErrNotImplementedYet
}
