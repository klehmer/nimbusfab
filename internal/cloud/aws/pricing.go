package aws

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
	region := regionFromID(p.ID)
	switch p.TofuType {
	case "aws_instance":
		instanceType, _ := p.Attributes["instance_type"].(string)
		return map[string]any{
			"service":         "AmazonEC2",
			"instanceType":    instanceType,
			"region":          region,
			"tenancy":         "Shared",
			"operatingSystem": "Linux",
			"preInstalledSw":  "NA",
			"capacitystatus":  "Used",
		}, nil
	case "aws_db_instance":
		engine, _ := p.Attributes["engine"].(string)
		deployment := "Single-AZ"
		if mz, _ := p.Attributes["multi_az"].(bool); mz {
			deployment = "Multi-AZ"
		}
		instanceClass, _ := p.Attributes["instance_class"].(string)
		return map[string]any{
			"service":          "AmazonRDS",
			"instanceType":     instanceClass,
			"region":           region,
			"engineCode":       engine,
			"deploymentOption": deployment,
			"licenseModel":     "No license required",
		}, nil
	case "aws_s3_bucket":
		return map[string]any{
			"service":      "AmazonS3",
			"region":       region,
			"storageClass": "Standard",
		}, nil
	default:
		// Free primitives (vpc / subnet / igw / rt / sg / etc.).
		return nil, nil
	}
}

// regionFromID extracts the region from a primitive ID like
// "<component>.aws-<region>.<localname>".
func regionFromID(id string) string {
	for i := 0; i < len(id); i++ {
		if i+5 <= len(id) && id[i:i+5] == ".aws-" {
			rest := id[i+5:]
			for j := 0; j < len(rest); j++ {
				if rest[j] == '.' {
					return rest[:j]
				}
			}
			return rest
		}
	}
	return ""
}
