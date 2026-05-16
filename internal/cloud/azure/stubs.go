package azure

import (
	"context"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

// Phase-4 per-type emit stubs — replaced by network.go / compute.go /
// database.go / storage.go as those tasks land.

func (*Adapter) emitCompute(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitComputeImpl(target, refs)
}

func (*Adapter) emitDatabase(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitDatabaseImpl(target, refs)
}

func (*Adapter) emitStorage(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("azure: storage emit not yet implemented")
}

// Profile + PricingKey stubs — replaced by profile.go / pricing.go in Tasks 6-7.

func (*Adapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
}

func (*Adapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
	return nil, nil
}
