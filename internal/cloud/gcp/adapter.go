// Package gcp implements pkg/cloud.Adapter for Google Cloud Platform. Phase 5
// supports the four v1 component types (network, compute, database, storage)
// via the hashicorp/google provider.
package gcp

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Adapter is the GCP implementation of cloud.Adapter.
type Adapter struct{}

// New returns a configured GCP Adapter.
func New() *Adapter { return &Adapter{} }

// Compile-time check that Adapter satisfies cloud.Adapter.
var _ cloud.Adapter = (*Adapter)(nil)

func (*Adapter) Name() string                   { return "gcp" }
func (*Adapter) SupportedAPIVersions() []string { return []string{ir.APIVersionV1Alpha1} }

// TofuProviderVersion pins hashicorp/google v7 (current major release).
// The renderer's default "~> 5.0" would not resolve.
func (*Adapter) TofuProviderVersion() string { return "~> 7.0" }
func (*Adapter) SupportedComponentTypes() []string {
	return []string{"network", "compute", "database", "storage"}
}
func (*Adapter) TierOneSchema() []byte { return tierOneSchema }

var gcpRegionRe = regexp.MustCompile(`^[a-z]+-[a-z]+[0-9]$`)

func (*Adapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
	if target.Region == "" {
		return []ir.Issue{{
			Severity: ir.SeverityError, Code: "ErrAdapterGCPRegionInvalid",
			Message: "GCP targets must declare a region (use GCP region names like 'us-central1', not AWS-style 'us-east-1' or Azure-style 'eastus')",
			Path:    "target.region",
		}}
	}
	if !gcpRegionRe.MatchString(target.Region) {
		return []ir.Issue{{
			Severity: ir.SeverityError, Code: "ErrAdapterGCPRegionInvalid",
			Message: fmt.Sprintf("GCP region %q does not match expected format (e.g. 'us-central1', 'europe-west1')", target.Region),
			Path:    "target.region",
		}}
	}
	return nil
}

func (*Adapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
	return ir.StateBackend{
		Kind: "gcs",
		Config: map[string]any{
			"bucket": "nimbusfab-state",
			"prefix": fmt.Sprintf("gcp/%s", target.Region),
		},
	}, nil
}

func (*Adapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, _ cloud.Credentials) (map[string]any, error) {
	block := map[string]any{
		"region": target.Region,
	}
	if project, _ := target.Spec["project"].(string); project != "" {
		block["project"] = project
	}
	return map[string]any{
		"google": block,
	}, nil
}

// BillingQuery / FetchBilling stubs until the Cost Collector phase.

func (*Adapter) BillingQuery(ctx context.Context, _ cloud.Credentials, _, _ time.Time) (cloud.BillingQueryParams, error) {
	return nil, cloud.ErrNotImplementedYet
}

func (*Adapter) FetchBilling(ctx context.Context, _ cloud.Credentials, _ cloud.BillingQueryParams) ([]cloud.NormalizedCostRow, error) {
	return nil, cloud.ErrNotImplementedYet
}
