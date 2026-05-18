package aws

import (
	"context"
	"fmt"
	"net"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) emitNetwork(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	cidr, _ := target.Spec["cidr"].(string)
	if cidr == "" {
		cidr = "10.0.0.0/16"
	}
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "network"
	}
	subnetCount := intFromSpec(target.Spec, "subnetCount", 3)
	if subnetCount < 1 {
		subnetCount = 1
	}

	name := tofuIdentifier(component)
	azs := defaultAZsForRegion(target.Region, subnetCount)
	subnetCIDRs, err := splitCIDR(cidr, subnetCount)
	if err != nil {
		return nil, fmt.Errorf("aws.emitNetwork: %w", err)
	}

	out := []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.aws-%s.vpc", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_vpc",
			TofuName: name,
			Attributes: map[string]any{
				"cidr_block":           cidr,
				"enable_dns_support":   true,
				"enable_dns_hostnames": true,
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.igw", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_internet_gateway",
			TofuName: name,
			Attributes: map[string]any{
				"vpc_id": "${aws_vpc." + name + ".id}",
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.rt", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_route_table",
			TofuName: name,
			Attributes: map[string]any{
				"vpc_id": "${aws_vpc." + name + ".id}",
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.route_default", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_route",
			TofuName: name + "_default",
			TagAttribute: "-",
			Attributes: map[string]any{
				"route_table_id":         "${aws_route_table." + name + ".id}",
				"destination_cidr_block": "0.0.0.0/0",
				"gateway_id":             "${aws_internet_gateway." + name + ".id}",
			},
		},
	}
	for i := 0; i < subnetCount; i++ {
		subnetName := fmt.Sprintf("%s_%d", name, i)
		out = append(out, ir.ResourcePrimitive{
			ID:       fmt.Sprintf("%s.aws-%s.subnet_%d", component, target.Region, i),
			Cloud:    "aws",
			TofuType: "aws_subnet",
			TofuName: subnetName,
			Attributes: map[string]any{
				"vpc_id":                  "${aws_vpc." + name + ".id}",
				"cidr_block":              subnetCIDRs[i],
				"availability_zone":       azs[i%len(azs)],
				"map_public_ip_on_launch": true,
			},
		})
		out = append(out, ir.ResourcePrimitive{
			ID:       fmt.Sprintf("%s.aws-%s.rta_%d", component, target.Region, i),
			Cloud:    "aws",
			TofuType: "aws_route_table_association",
			TofuName: subnetName,
			TagAttribute: "-",
			Attributes: map[string]any{
				"subnet_id":      "${aws_subnet." + subnetName + ".id}",
				"route_table_id": "${aws_route_table." + name + ".id}",
			},
		})
	}
	return out, nil
}

// Per-type stubs that Tasks 6-8 replace.

func (*Adapter) emitCompute(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitComputeImpl(target, refs)
}

func (*Adapter) emitDatabase(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitDatabaseImpl(target, refs)
}

func (*Adapter) emitStorage(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return emitStorageImpl(target, refs)
}

func intFromSpec(spec map[string]any, key string, def int) int {
	if v, ok := spec[key]; ok {
		switch t := v.(type) {
		case int:
			return t
		case int64:
			return int(t)
		case float64:
			return int(t)
		}
	}
	return def
}

func defaultAZsForRegion(region string, n int) []string {
	az := []string{region + "a", region + "b", region + "c", region + "d", region + "e"}
	if n > len(az) {
		n = len(az)
	}
	return az[:n]
}

// splitCIDR slices a parent IPv4 CIDR into n equal-size child blocks.
// For /16 parent, produces /24 children indexed by third octet.
// For larger parents, scales the prefix appropriately.
func splitCIDR(parent string, n int) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(parent)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", parent, err)
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("only IPv4 supported in Phase 3 (got %q)", parent)
	}
	// Bump prefix by 8 bits (e.g., /16 -> /24). Bound to /30 max.
	newPrefix := ones + 8
	if newPrefix > 30 {
		newPrefix = 30
	}
	out := make([]string, n)
	base := ipNet.IP.To4()
	if base == nil {
		return nil, fmt.Errorf("not IPv4 CIDR: %s", parent)
	}
	for i := 0; i < n; i++ {
		ip := []byte{base[0], base[1], byte(i), 0}
		mask := net.CIDRMask(newPrefix, 32)
		subnet := &net.IPNet{IP: ip, Mask: mask}
		out[i] = subnet.String()
	}
	return out, nil
}
