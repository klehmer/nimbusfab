package engine

import (
	"context"
	"encoding/json"

	"github.com/klehmer/nimbusfab/pkg/cost/estimator"
	"github.com/klehmer/nimbusfab/pkg/cost/pricing"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

// persistCostEstimates is called from persistPlan with one planRunRef per
// target. For each, it runs the estimator over the target's primitives and
// BulkInserts one inventory.CostEstimate row per priced primitive, attached
// to that target's plan-run ID.
//
// Failures bubble back to persistPlan which logs and continues (cost-
// dashboard data is non-critical to plan success). Zero-priced primitives
// (PricingKey returned nil) emit no rows.
func (e *runtimeEngine) persistCostEstimates(ctx context.Context, orgID string, planRuns []planRunRef) error {
	if len(planRuns) == 0 {
		return nil
	}
	cache := pricing.NewCache()
	est := estimator.New(pricing.AsPricingProvider(cache))

	var items []inventory.CostEstimate
	for _, pr := range planRuns {
		adapter, ok := e.cfg.CloudAdapters.Get(pr.Cloud)
		if !ok {
			continue
		}
		in := estimator.EstimateInput{Targets: []estimator.TargetInput{{
			DeploymentTargetID: pr.TargetID,
			Cloud:              pr.Cloud,
			Region:             pr.Region,
			Adapter:            adapter,
			Primitives:         pr.Primitives,
		}}}
		out, err := est.Estimate(ctx, in)
		if err != nil {
			// Per-target estimator failure: skip this run, keep going so
			// the rest of the dashboard still has data.
			continue
		}
		for _, t := range out.Targets {
			for _, p := range t.Primitives {
				keyJSON, _ := json.Marshal(p.PricingKey)
				items = append(items, inventory.CostEstimate{
					RunID:          pr.RunID,
					OrgID:          orgID,
					PrimitiveID:    p.PrimitiveID,
					Currency:       out.Currency,
					UnitPrice:      p.UnitPrice,
					Units:          p.Units,
					UnitOfMeasure:  p.UnitOfMeasure,
					Subtotal:       p.Subtotal,
					PricingKeyJSON: keyJSON,
				})
			}
		}
	}
	return e.cfg.InventoryRepo.CostEstimates().BulkInsert(ctx, items)
}
