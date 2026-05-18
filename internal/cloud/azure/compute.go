package azure

import (
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

type computeSizeProfile struct {
	SKU      string
	VCPU     int
	MemoryGB float64
}

var computeSizes = map[string]computeSizeProfile{
	"small":  {"Standard_B2s", 2, 4},
	"medium": {"Standard_B2ms", 2, 8},
	"large":  {"Standard_B4ms", 4, 16},
	"xlarge": {"Standard_D4s_v5", 4, 16},
}

func emitComputeImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "compute"
	}
	name := tofuIdent(component)
	rgName := resourceGroupName(component, target.Region)
	replicas := intFromSpec(target.Spec, "replicas", 1)

	vmSize, err := resolveComputeSize(target.Spec)
	if err != nil {
		return nil, fmt.Errorf("azure.emitCompute: %w", err)
	}

	storageGB := intFromMap(target.Spec["storage"], "sizeGB", 30)
	subnetID, err := stringRef(refs, "subnetId")
	if err != nil {
		return nil, err
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
	for i := 0; i < replicas; i++ {
		idx := fmt.Sprintf("%s_%d", name, i)
		out = append(out,
			ir.ResourcePrimitive{
				ID:       fmt.Sprintf("%s.azure-%s.pip_%d", component, target.Region, i),
				Cloud:    "azure",
				TofuType: "azurerm_public_ip",
				TofuName: idx,
				Attributes: map[string]any{
					"name":                name + fmt.Sprintf("-pip-%d", i),
					"location":            target.Region,
					"resource_group_name": "${azurerm_resource_group." + name + ".name}",
					"allocation_method":   "Static",
					"sku":                 "Standard",
				},
			},
			ir.ResourcePrimitive{
				ID:       fmt.Sprintf("%s.azure-%s.nic_%d", component, target.Region, i),
				Cloud:    "azure",
				TofuType: "azurerm_network_interface",
				TofuName: idx,
				Attributes: map[string]any{
					"name":                name + fmt.Sprintf("-nic-%d", i),
					"location":            target.Region,
					"resource_group_name": "${azurerm_resource_group." + name + ".name}",
					"ip_configuration": []any{
						map[string]any{
							"name":                          "primary",
							"subnet_id":                     subnetID,
							"private_ip_address_allocation": "Dynamic",
							"public_ip_address_id":          "${azurerm_public_ip." + idx + ".id}",
						},
					},
				},
			},
			ir.ResourcePrimitive{
				ID:       fmt.Sprintf("%s.azure-%s.vm_%d", component, target.Region, i),
				Cloud:    "azure",
				TofuType: "azurerm_linux_virtual_machine",
				TofuName: idx,
				Attributes: map[string]any{
					"name":                name + fmt.Sprintf("-vm-%d", i),
					"location":            target.Region,
					"resource_group_name": "${azurerm_resource_group." + name + ".name}",
					"size":                vmSize,
					"admin_username":      "azureuser",
					"network_interface_ids": []any{
						"${azurerm_network_interface." + idx + ".id}",
					},
					"os_disk": []any{map[string]any{
						"caching":              "ReadWrite",
						"storage_account_type": "Standard_LRS",
						"disk_size_gb":         storageGB,
					}},
					"source_image_reference": []any{map[string]any{
						"publisher": "Canonical",
						"offer":     "0001-com-ubuntu-server-jammy",
						"sku":       "22_04-lts",
						"version":   "latest",
					}},
					"disable_password_authentication": true,
				},
			})
	}
	return out, nil
}

func resolveComputeSize(spec map[string]any) (string, error) {
	if size, ok := spec["size"].(string); ok && size != "" {
		p, ok := computeSizes[size]
		if !ok {
			return "", fmt.Errorf("unknown size %q", size)
		}
		if _, hasC := spec["compute"]; hasC {
			return "", fmt.Errorf("size and compute are mutually exclusive")
		}
		return p.SKU, nil
	}
	compute, _ := spec["compute"].(map[string]any)
	if compute == nil {
		return "", fmt.Errorf("spec.size or spec.compute required")
	}
	vcpu := intFromMap(compute, "vCPU", 0)
	memGB := floatFromMap(compute, "memoryGB", 0)
	for _, sz := range []string{"small", "medium", "large", "xlarge"} {
		p := computeSizes[sz]
		if p.VCPU >= vcpu && p.MemoryGB >= memGB {
			return p.SKU, nil
		}
	}
	return "", fmt.Errorf("no T-shirt size satisfies vCPU>=%d memoryGB>=%v", vcpu, memGB)
}

// stringRef returns the string-typed ref under alias; errors when missing or
// when the ref is not a string. Use this for required cross-component refs —
// the validator and preflight should prevent missing refs, but we fail loudly
// rather than emitting invalid tofu.
func stringRef(refs cloud.ResolvedRefs, alias string) (string, error) {
	v, ok := refs[alias]
	if !ok {
		return "", fmt.Errorf("azure.compute: required ref %q not in ResolvedRefs", alias)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("azure.compute: ref %q has unsupported type %T", alias, v)
	}
	return s, nil
}

func stringFromRefs(refs cloud.ResolvedRefs, key, fallback string) string {
	if v, ok := refs[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func intFromMap(m any, key string, def int) int {
	asMap, _ := m.(map[string]any)
	if asMap == nil {
		return def
	}
	if v, ok := asMap[key]; ok {
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

func floatFromMap(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case int:
			return float64(t)
		case int64:
			return float64(t)
		}
	}
	return def
}
