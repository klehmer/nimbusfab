package azure

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
	name := tofuIdent(component)
	rgName := resourceGroupName(component, target.Region)

	subnetCIDRs, err := splitCIDR(cidr, subnetCount)
	if err != nil {
		return nil, fmt.Errorf("azure.emitNetwork: %w", err)
	}

	out := []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.azure-%s.rg", component, target.Region),
			Cloud:    "azure",
			TofuType: "azurerm_resource_group",
			TofuName: name,
			Attributes: map[string]any{
				"name":     rgName,
				"location": target.Region,
			},
		},
		{
			ID:       fmt.Sprintf("%s.azure-%s.vnet", component, target.Region),
			Cloud:    "azure",
			TofuType: "azurerm_virtual_network",
			TofuName: name,
			Attributes: map[string]any{
				"name":                name + "-vnet",
				"address_space":       []any{cidr},
				"location":            target.Region,
				"resource_group_name": "${azurerm_resource_group." + name + ".name}",
			},
		},
		{
			ID:       fmt.Sprintf("%s.azure-%s.nsg", component, target.Region),
			Cloud:    "azure",
			TofuType: "azurerm_network_security_group",
			TofuName: name,
			Attributes: map[string]any{
				"name":                name + "-nsg",
				"location":            target.Region,
				"resource_group_name": "${azurerm_resource_group." + name + ".name}",
			},
		},
	}
	for i := 0; i < subnetCount; i++ {
		subnetName := fmt.Sprintf("%s_%d", name, i)
		out = append(out, ir.ResourcePrimitive{
			ID:       fmt.Sprintf("%s.azure-%s.subnet_%d", component, target.Region, i),
			Cloud:    "azure",
			TofuType: "azurerm_subnet",
			TofuName: subnetName,
			NoTags:   true,
			Attributes: map[string]any{
				"name":                 subnetName,
				"resource_group_name":  "${azurerm_resource_group." + name + ".name}",
				"virtual_network_name": "${azurerm_virtual_network." + name + ".name}",
				"address_prefixes":     []any{subnetCIDRs[i]},
			},
		})
	}
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

func splitCIDR(parent string, n int) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(parent)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", parent, err)
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("only IPv4 supported (got %q)", parent)
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
