package aws

import (
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

type computeSizeProfile struct {
	InstanceType string
	VCPU         int
	MemoryGB     float64
}

var computeSizes = map[string]computeSizeProfile{
	"small":  {"t3.small", 2, 2},
	"medium": {"t3.medium", 2, 4},
	"large":  {"t3.large", 2, 8},
	"xlarge": {"t3.xlarge", 4, 16},
}

// Default Amazon Linux 2023 AMI per region (Phase 3; refresh on AMI rotation).
// Values are deliberately stable strings rather than a runtime lookup so emit
// is pure and deterministic.
var amazonLinux2023AMI = map[string]string{
	"us-east-1":      "ami-0c80e2b6cccc3a73c",
	"us-east-2":      "ami-08d8b2eb8bc7e5d2c",
	"us-west-1":      "ami-0c5fa1d2afb39dabe",
	"us-west-2":      "ami-093a4ad9a8cc370f4",
	"eu-west-1":      "ami-0eed1c915ea891ace",
	"eu-central-1":   "ami-04a59bc910beb6f9d",
	"ap-northeast-1": "ami-0c0a44d3a8df36c0e",
	"ap-southeast-2": "ami-0e0aa808e23c2735c",
}

func emitComputeImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "compute"
	}
	name := tofuIdentifier(component)
	replicas := intFromSpec(target.Spec, "replicas", 1)

	instanceType, err := resolveComputeSize(target.Spec)
	if err != nil {
		return nil, fmt.Errorf("aws.emitCompute: %w", err)
	}

	ami, _ := target.Spec["imageRef"].(string)
	if ami == "" {
		if v, ok := amazonLinux2023AMI[target.Region]; ok {
			ami = v
		} else {
			return nil, fmt.Errorf("aws.emitCompute: no default AMI for region %q (specify spec.imageRef)", target.Region)
		}
	}

	subnetID, err := stringRef(refs, "subnetId")
	if err != nil {
		return nil, err
	}
	vpcID, err := stringRef(refs, "vpcId")
	if err != nil {
		return nil, err
	}
	storageGB := intFromMap(target.Spec["storage"], "sizeGB", 30)

	out := []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.aws-%s.sg", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_security_group",
			TofuName: name,
			Attributes: map[string]any{
				"name":        awsResourceName(component) + "-sg",
				"description": "Default SG for " + component,
				"vpc_id":      vpcID,
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.sg_egress", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_vpc_security_group_egress_rule",
			TofuName: name + "_egress_all",
			Attributes: map[string]any{
				"security_group_id": "${aws_security_group." + name + ".id}",
				"cidr_ipv4":         "0.0.0.0/0",
				"ip_protocol":       "-1",
			},
		},
	}
	for i := 0; i < replicas; i++ {
		instanceName := fmt.Sprintf("%s_%d", name, i)
		out = append(out, ir.ResourcePrimitive{
			ID:       fmt.Sprintf("%s.aws-%s.instance_%d", component, target.Region, i),
			Cloud:    "aws",
			TofuType: "aws_instance",
			TofuName: instanceName,
			Attributes: map[string]any{
				"ami":                    ami,
				"instance_type":          instanceType,
				"subnet_id":              subnetID,
				"vpc_security_group_ids": []any{"${aws_security_group." + name + ".id}"},
				"root_block_device": []any{
					map[string]any{
						"volume_size": storageGB,
						"volume_type": "gp3",
						"encrypted":   true,
					},
				},
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
		return p.InstanceType, nil
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
			return p.InstanceType, nil
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
		return "", fmt.Errorf("aws.compute: required ref %q not in ResolvedRefs", alias)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("aws.compute: ref %q has unsupported type %T", alias, v)
	}
	return s, nil
}

func stringFromRefs(refs cloud.ResolvedRefs, key, fallback string) string {
	if v, ok := refs[key].(string); ok && v != "" {
		return v
	}
	return fallback
}
