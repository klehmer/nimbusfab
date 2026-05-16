package gcp

import (
	"context"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

// Per-type Emit wrappers. Stubbed bodies are replaced by Tasks 2-5.

func (*Adapter) emitNetwork(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("gcp: network emit not yet implemented")
}

func (*Adapter) emitCompute(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("gcp: compute emit not yet implemented")
}

func (*Adapter) emitDatabase(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("gcp: database emit not yet implemented")
}

func (*Adapter) emitStorage(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("gcp: storage emit not yet implemented")
}

// Profile + PricingKey stubs (replaced by Tasks 6-7).

func (*Adapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
}

func (*Adapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
	return nil, nil
}
