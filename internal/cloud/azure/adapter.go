// Package azure implements pkg/cloud.Adapter for Microsoft Azure. Phase 4
// supports the four v1 component types (network, compute, database, storage)
// via the hashicorp/azurerm provider.
package azure

import (
	"context"
	"fmt"
	"time"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Adapter is the Azure implementation of cloud.Adapter.
type Adapter struct{}

// New returns a configured Azure Adapter.
func New() *Adapter { return &Adapter{} }

// Compile-time check that Adapter satisfies cloud.Adapter.
var _ cloud.Adapter = (*Adapter)(nil)

func (*Adapter) Name() string                   { return "azure" }
func (*Adapter) SupportedAPIVersions() []string { return []string{ir.APIVersionV1Alpha1} }

// TofuProviderVersion pins hashicorp/azurerm v4 (current major release).
// The renderer's default "~> 5.0" would not resolve.
func (*Adapter) TofuProviderVersion() string { return "~> 4.0" }
func (*Adapter) SupportedComponentTypes() []string {
	return []string{"network", "compute", "database", "storage"}
}
func (*Adapter) TierOneSchema() []byte { return tierOneSchema }

func (*Adapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
	if target.Region == "" {
		return []ir.Issue{{
			Severity: ir.SeverityError, Code: "ErrAdapterAzureRegionInvalid",
			Message: "Azure targets must declare a region (use Azure location names like 'eastus', not AWS-style 'us-east-1')",
			Path:    "target.region",
		}}
	}
	if hasHyphen(target.Region) {
		return []ir.Issue{{
			Severity: ir.SeverityError, Code: "ErrAdapterAzureRegionInvalid",
			Message: fmt.Sprintf("Azure region %q looks like an AWS region name; use Azure format (e.g. 'eastus' not 'us-east-1')", target.Region),
			Path:    "target.region",
		}}
	}
	return nil
}

func hasHyphen(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			return true
		}
	}
	return false
}

func (*Adapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
	return ir.StateBackend{
		Kind: "azurerm",
		Config: map[string]any{
			"resource_group_name":  "nimbusfab-state",
			"storage_account_name": "nimbusfabstate",
			"container_name":       "terraform-state",
			"key":                  fmt.Sprintf("azure/%s/terraform.tfstate", target.Region),
		},
	}, nil
}

func (*Adapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, _ cloud.Credentials) (map[string]any, error) {
	return map[string]any{
		"azurerm": map[string]any{
			"features": map[string]any{},
		},
	}, nil
}

// BillingQuery / FetchBilling stubs until the Cost Collector phase.

func (*Adapter) BillingQuery(ctx context.Context, _ cloud.Credentials, _, _ time.Time) (cloud.BillingQueryParams, error) {
	return nil, cloud.ErrNotImplementedYet
}

func (*Adapter) FetchBilling(ctx context.Context, _ cloud.Credentials, _ cloud.BillingQueryParams) ([]cloud.NormalizedCostRow, error) {
	return nil, cloud.ErrNotImplementedYet
}
