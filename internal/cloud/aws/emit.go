package aws

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	cidr, _ := target.Spec["cidr"].(string)
	if cidr == "" {
		cidr = "10.0.0.0/16"
	}
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "network"
	}
	tofuName := tofuIdentifier(component)
	return []ir.ResourcePrimitive{{
		ID:       fmt.Sprintf("%s.aws-%s.vpc", component, target.Region),
		Cloud:    "aws",
		TofuType: "aws_vpc",
		TofuName: tofuName,
		Attributes: map[string]any{
			"cidr_block":           cidr,
			"enable_dns_support":   true,
			"enable_dns_hostnames": true,
		},
	}}, nil
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
