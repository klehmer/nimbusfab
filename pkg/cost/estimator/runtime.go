package estimator

import (
	"context"
	"fmt"
)

// New returns a runtime Estimator wired against the supplied PricingProvider.
func New(provider PricingProvider) Estimator {
	return &runtime{provider: provider}
}

type runtime struct {
	provider PricingProvider
}

func (r *runtime) Estimate(ctx context.Context, in EstimateInput) (Estimate, error) {
	est := Estimate{
		Currency: "USD", // v1: single currency
		Period:   "month",
	}
	for _, target := range in.Targets {
		tEst := TargetEstimate{
			DeploymentTargetID: target.DeploymentTargetID,
			Cloud:              target.Cloud,
			Region:             target.Region,
		}
		for _, prim := range target.Primitives {
			key, err := target.Adapter.PricingKey(ctx, prim)
			if err != nil || key == nil {
				continue
			}
			unit, perr := r.provider.Price(ctx, target.Cloud, key)
			if perr != nil {
				est.Warnings = append(est.Warnings, fmt.Sprintf(
					"missing pricing for %s (%s): %v", prim.ID, prim.TofuType, perr))
				continue
			}
			units := UnitsFor(prim.TofuType, target.Usage)
			if units == 0 {
				est.Warnings = append(est.Warnings, fmt.Sprintf(
					"no usage assumption for %s (%s); skipping", prim.ID, prim.TofuType))
				continue
			}
			subtotal := unit.UnitPrice * units
			tEst.Primitives = append(tEst.Primitives, PrimitiveEstimate{
				PrimitiveID:   prim.ID,
				PricingKey:    key,
				UnitPrice:     unit.UnitPrice,
				Units:         units,
				UnitOfMeasure: unit.UnitOfMeasure,
				Subtotal:      subtotal,
			})
			tEst.Subtotal += subtotal
		}
		est.Targets = append(est.Targets, tEst)
		est.Total += tEst.Subtotal
	}
	return est, nil
}
