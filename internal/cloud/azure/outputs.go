package azure

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// OutputBindings returns the tofu expressions for outputs declared by this
// component's Type. STUB for Commit 1 of cross-component planning — real
// implementation lands in the next commit.
func (*Adapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error) {
	return map[string]string{}, nil
}
