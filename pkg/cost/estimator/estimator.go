// Package estimator computes pre-deploy cost estimates from a planned set of
// ResourcePrimitives. Each cloud adapter supplies a PricingKey per primitive;
// the estimator hands that to a PricingProvider (typically the pricing cache,
// which falls through to live cloud pricing APIs or a bundled snapshot), then
// multiplies by usage assumptions to produce per-primitive subtotals.
package estimator

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Estimator turns a plan-shaped slice of primitives into a cost estimate.
type Estimator interface {
	Estimate(ctx context.Context, in EstimateInput) (Estimate, error)
}

// EstimateInput carries everything the estimator needs. Adapters resolve their
// own primitives' pricing keys; the estimator never inspects cloud specifics.
type EstimateInput struct {
	Targets []TargetInput
}

// TargetInput is per-(component, cloud, region).
type TargetInput struct {
	DeploymentTargetID string
	Cloud              string
	Region             string
	Adapter            cloud.Adapter
	Primitives         []ir.ResourcePrimitive
	Usage              map[string]any // overrides from component spec.usage
}

// Estimate is the aggregate cost tree.
type Estimate struct {
	Currency string
	Period   string // "month" by default
	Total    float64
	Targets  []TargetEstimate
	Warnings []string
}

// TargetEstimate breaks costs down per target.
type TargetEstimate struct {
	DeploymentTargetID string
	Cloud              string
	Region             string
	Subtotal           float64
	Primitives         []PrimitiveEstimate
}

// PrimitiveEstimate is the per-resource leaf.
type PrimitiveEstimate struct {
	PrimitiveID    string
	PricingKey     map[string]any
	UnitPrice      float64
	Units          float64
	UnitOfMeasure  string
	Subtotal       float64
}

// PricingProvider answers price lookups given an adapter-supplied key. The
// cache implementation falls through to live APIs and bundled snapshots.
type PricingProvider interface {
	Price(ctx context.Context, cloudName string, key map[string]any) (UnitPrice, error)
}

// UnitPrice is one price-list row.
type UnitPrice struct {
	UnitPrice     float64
	UnitOfMeasure string
	Currency      string
	Source        string // "live" | "snapshot" | "cache"
}
