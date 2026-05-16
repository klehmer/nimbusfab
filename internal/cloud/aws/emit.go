package aws

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Emit dispatches on target.Spec["__type"] (set by the provisioner). Each
// supported type's emit logic lives in its own file: network.go, compute.go,
// database.go, storage.go.
func (a *Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	compType, _ := target.Spec["__type"].(string)
	switch compType {
	case "network", "":
		// Empty == back-compat for Phase-1 tests that don't set __type.
		return a.emitNetwork(ctx, target, refs)
	case "compute":
		return a.emitCompute(ctx, target, refs)
	case "database":
		return a.emitDatabase(ctx, target, refs)
	case "storage":
		return a.emitStorage(ctx, target, refs)
	default:
		return nil, fmt.Errorf("aws: unsupported component type %q", compType)
	}
}

var tofuIdentRe = regexp.MustCompile(`[^a-z0-9_]`)

// tofuIdentifier turns a DSL identifier into a Tofu-safe local name.
func tofuIdentifier(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "_")
	s = tofuIdentRe.ReplaceAllString(s, "_")
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "_" + s
	}
	return s
}
