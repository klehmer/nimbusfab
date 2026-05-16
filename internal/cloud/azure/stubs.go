package azure

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

// Per-type Emit wrappers. Implementations live in the per-type files
// (network.go / compute.go / database.go / storage.go).

func (*Adapter) emitCompute(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitComputeImpl(target, refs)
}

func (*Adapter) emitDatabase(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitDatabaseImpl(target, refs)
}

func (*Adapter) emitStorage(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitStorageImpl(target, refs)
}

// Profile + PricingKey delegate to the implementations in profile.go / pricing.go.

func (*Adapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	return profileImpl(p)
}

func (*Adapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
	return pricingKeyImpl(p)
}
