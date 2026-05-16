package azure

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Emit dispatches on target.Spec["__type"] (set by the provisioner).
func (a *Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	compType, _ := target.Spec["__type"].(string)
	switch compType {
	case "network":
		return a.emitNetwork(ctx, target, refs)
	case "compute":
		return a.emitCompute(ctx, target, refs)
	case "database":
		return a.emitDatabase(ctx, target, refs)
	case "storage":
		return a.emitStorage(ctx, target, refs)
	default:
		return nil, fmt.Errorf("azure: unsupported component type %q", compType)
	}
}

var tofuIdentRe = regexp.MustCompile(`[^a-z0-9_]`)

// tofuIdent sanitizes a component name to a Tofu-safe local identifier.
func tofuIdent(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "_")
	s = tofuIdentRe.ReplaceAllString(s, "_")
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "_" + s
	}
	return s
}

// resourceGroupName derives a stable RG name from (component, region).
func resourceGroupName(component, region string) string {
	return "nimbusfab-" + component + "-" + region
}
