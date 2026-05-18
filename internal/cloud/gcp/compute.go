package gcp

import (
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

var computeMachineTypes = map[string]string{
	"small":  "e2-small",
	"medium": "e2-medium",
	"large":  "e2-standard-2",
	"xlarge": "n2-standard-4",
}

var computeZoneSuffixes = []string{"a", "b", "c"}

func emitComputeImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "compute"
	}
	size, _ := target.Spec["size"].(string)
	if size == "" {
		size = "small"
	}
	machineType := machineTypeFromSpec(target.Spec, size)
	if machineType == "" {
		return nil, fmt.Errorf("gcp.emitCompute: unknown size %q", size)
	}
	replicas := intFromSpec(target.Spec, "replicas", 1)
	if replicas < 1 {
		replicas = 1
	}

	diskGB := 30
	if storage, ok := target.Spec["storage"].(map[string]any); ok {
		diskGB = intFromSpec(storage, "sizeGB", 30)
	}

	image, _ := target.Spec["imageRef"].(string)
	if image == "" {
		image = "ubuntu-os-cloud/ubuntu-2204-lts"
	}

	name := tofuIdent(component)
	resName := gcpResourceNameSimple(component)
	networkRef, _ := refs["networkId"].(string)
	subnetRef, _ := refs["subnetworkId"].(string)

	out := []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.gcp-%s.fw_egress", component, target.Region),
			Cloud:    "gcp",
			TofuType: "google_compute_firewall",
			TofuName: name + "_egress",
			Attributes: map[string]any{
				"name":               resName + "-fw-egress",
				"network":            firewallNetworkRef(networkRef),
				"direction":          "EGRESS",
				"destination_ranges": []any{"0.0.0.0/0"},
				"allow":              []any{map[string]any{"protocol": "all"}},
			},
		},
	}
	for i := 0; i < replicas; i++ {
		zone := target.Region + "-" + computeZoneSuffixes[i%len(computeZoneSuffixes)]
		instName := fmt.Sprintf("%s_%d", name, i)
		nic := map[string]any{}
		if subnetRef != "" {
			nic["subnetwork"] = subnetRef
		} else if networkRef != "" {
			nic["network"] = networkRef
		} else {
			nic["network"] = "default"
		}
		out = append(out, ir.ResourcePrimitive{
			ID:           fmt.Sprintf("%s.gcp-%s.instance_%d", component, target.Region, i),
			Cloud:        "gcp",
			TofuType:     "google_compute_instance",
			TofuName:     instName,
			TagAttribute: "labels",
			Attributes: map[string]any{
				"name":         fmt.Sprintf("%s-%d", resName, i),
				"machine_type": machineType,
				"zone":         zone,
				"boot_disk": []any{map[string]any{
					"initialize_params": []any{map[string]any{
						"image": image,
						"size":  diskGB,
					}},
				}},
				"network_interface": []any{nic},
			},
		})
	}
	return out, nil
}

func machineTypeFromSpec(spec map[string]any, size string) string {
	if compute, ok := spec["compute"].(map[string]any); ok {
		if mt, _ := compute["machineType"].(string); mt != "" {
			return mt
		}
	}
	if mt, ok := computeMachineTypes[size]; ok {
		return mt
	}
	return ""
}

func firewallNetworkRef(ref string) string {
	if ref == "" {
		return "default"
	}
	return ref
}
