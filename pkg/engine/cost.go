package engine

import (
	"context"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cost/estimator"
	"github.com/klehmer/nimbusfab/pkg/cost/pricing"
)

// estimateCost is the inventory-agnostic core: takes a PlanResult, builds the
// EstimateInput from per-target primitives via the cloud registry, runs the
// estimator, wraps the result into the engine's CostEstimate.
func (e *runtimeEngine) estimateCost(ctx context.Context, plan *PlanResult) (*CostEstimate, error) {
	if plan == nil {
		return nil, fmt.Errorf("engine.EstimateCost: nil plan")
	}
	cache := pricing.NewCache()
	est := estimator.New(pricing.AsPricingProvider(cache))

	in := estimator.EstimateInput{}
	for _, tp := range plan.Targets {
		adapter, ok := e.cfg.CloudAdapters.Get(tp.Cloud)
		if !ok {
			return nil, fmt.Errorf("engine.EstimateCost: no adapter for %q", tp.Cloud)
		}
		in.Targets = append(in.Targets, estimator.TargetInput{
			DeploymentTargetID: tp.DeploymentTargetID,
			Cloud:              tp.Cloud,
			Region:             tp.Region,
			Adapter:            adapter,
			Primitives:         tp.RawPrimitives,
		})
	}
	out, err := est.Estimate(ctx, in)
	if err != nil {
		return nil, err
	}
	return convertEstimate(out), nil
}

func convertEstimate(e estimator.Estimate) *CostEstimate {
	out := &CostEstimate{
		Currency: e.Currency,
		Period:   e.Period,
		Total:    e.Total,
		Warnings: append([]string{}, e.Warnings...),
	}
	for _, t := range e.Targets {
		te := TargetCostEstimate{
			DeploymentTargetID: t.DeploymentTargetID,
			Cloud:              t.Cloud,
			Region:             t.Region,
			Subtotal:           t.Subtotal,
		}
		for _, p := range t.Primitives {
			te.Primitives = append(te.Primitives, PrimitiveCostEstimate{
				PrimitiveID:   p.PrimitiveID,
				PricingKey:    p.PricingKey,
				UnitPrice:     p.UnitPrice,
				Units:         p.Units,
				UnitOfMeasure: p.UnitOfMeasure,
				Subtotal:      p.Subtotal,
			})
		}
		out.Targets = append(out.Targets, te)
	}
	return out
}
