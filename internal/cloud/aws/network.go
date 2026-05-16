package aws

import (
	"context"
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// emitNetwork is the Phase-1 stub kept until Task 5 expands it. Returns just
// the aws_vpc primitive so back-compat tests pass.
func (*Adapter) emitNetwork(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	cidr, _ := target.Spec["cidr"].(string)
	if cidr == "" {
		cidr = "10.0.0.0/16"
	}
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "network"
	}
	name := tofuIdentifier(component)
	return []ir.ResourcePrimitive{{
		ID:       fmt.Sprintf("%s.aws-%s.vpc", component, target.Region),
		Cloud:    "aws",
		TofuType: "aws_vpc",
		TofuName: name,
		Attributes: map[string]any{
			"cidr_block":           cidr,
			"enable_dns_support":   true,
			"enable_dns_hostnames": true,
		},
	}}, nil
}

// Per-type stubs that Tasks 6-8 replace.

func (*Adapter) emitCompute(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("aws: compute emit not yet implemented")
}

func (*Adapter) emitDatabase(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("aws: database emit not yet implemented")
}

func (*Adapter) emitStorage(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return nil, fmt.Errorf("aws: storage emit not yet implemented")
}
