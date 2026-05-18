package gcp

import (
	"fmt"
	"net"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func emitNetworkImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	_ = refs
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
	name := tofuIdent(component)
	resName := gcpResourceName(component)

	subnetCIDRs, err := splitCIDR(cidr, subnetCount)
	if err != nil {
		return nil, fmt.Errorf("gcp.emitNetwork: %w", err)
	}

	out := []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.gcp-%s.vpc", component, target.Region),
			Cloud:    "gcp",
			TofuType: "google_compute_network",
			TofuName: name,
			TagAttribute: "",
			Attributes: map[string]any{
				"name":                    resName + "-vpc",
				"auto_create_subnetworks": false,
			},
		},
	}
	for i := 0; i < subnetCount; i++ {
		sname := fmt.Sprintf("%s_%d", name, i)
		out = append(out, ir.ResourcePrimitive{
			ID:       fmt.Sprintf("%s.gcp-%s.subnet_%d", component, target.Region, i),
			Cloud:    "gcp",
			TofuType: "google_compute_subnetwork",
			TofuName: sname,
			TagAttribute: "",
			Attributes: map[string]any{
				"name":          fmt.Sprintf("%s-subnet-%d", resName, i),
				"ip_cidr_range": subnetCIDRs[i],
				"region":        target.Region,
				"network":       "${google_compute_network." + name + ".id}",
			},
		})
	}
	out = append(out,
		ir.ResourcePrimitive{
			ID:       fmt.Sprintf("%s.gcp-%s.fw_internal", component, target.Region),
			Cloud:    "gcp",
			TofuType: "google_compute_firewall",
			TofuName: name + "_internal",
			TagAttribute: "",
			Attributes: map[string]any{
				"name":          resName + "-fw-internal",
				"network":       "${google_compute_network." + name + ".name}",
				"direction":     "INGRESS",
				"source_ranges": []any{cidr},
				"allow":         []any{map[string]any{"protocol": "all"}},
			},
		},
		ir.ResourcePrimitive{
			ID:       fmt.Sprintf("%s.gcp-%s.fw_deny_external", component, target.Region),
			Cloud:    "gcp",
			TofuType: "google_compute_firewall",
			TofuName: name + "_deny_external",
			TagAttribute: "",
			Attributes: map[string]any{
				"name":          resName + "-fw-deny-ext",
				"network":       "${google_compute_network." + name + ".name}",
				"direction":     "INGRESS",
				"priority":      65000,
				"source_ranges": []any{"0.0.0.0/0"},
				"deny":          []any{map[string]any{"protocol": "all"}},
			},
		},
	)
	return out, nil
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

func boolFromSpec(spec map[string]any, key string, def bool) bool {
	if v, ok := spec[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func splitCIDR(parent string, n int) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(parent)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", parent, err)
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("only IPv4 supported in Phase 5 (got %q)", parent)
	}
	newPrefix := ones + 8
	if newPrefix > 30 {
		newPrefix = 30
	}
	out := make([]string, n)
	base := ipNet.IP.To4()
	if base == nil {
		return nil, fmt.Errorf("not IPv4: %s", parent)
	}
	for i := 0; i < n; i++ {
		ip := []byte{base[0], base[1], byte(i), 0}
		mask := net.CIDRMask(newPrefix, 32)
		subnet := &net.IPNet{IP: ip, Mask: mask}
		out[i] = subnet.String()
	}
	return out, nil
}
