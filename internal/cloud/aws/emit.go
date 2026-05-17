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

var awsResourceNameRe = regexp.MustCompile(`[^a-z0-9-]`)

// awsResourceName turns a DSL identifier into a hyphen-only lowercase name
// suitable for AWS API-facing attributes (e.g. aws_db_instance.identifier,
// aws_db_subnet_group.name) which reject underscores. Distinct from
// tofuIdentifier, which uses underscores for Tofu local-name compatibility.
func awsResourceName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = awsResourceNameRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "n"
	}
	return s
}
