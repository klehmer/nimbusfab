package aws

import (
	"context"
	"fmt"
	"sort"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]any, error) {
	t, _ := target.Spec["__type"].(string)
	switch t {
	case "network":
		return outputsNetwork(primitives), nil
	case "compute":
		return outputsCompute(primitives), nil
	case "database":
		return outputsDatabase(primitives), nil
	case "storage":
		return outputsStorage(primitives), nil
	}
	return map[string]any{}, nil
}

func outputsNetwork(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	var subnetNames []string
	var routeTableNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "aws_vpc":
			out["vpc_id"] = fmt.Sprintf("${aws_vpc.%s.id}", p.TofuName)
		case "aws_subnet":
			subnetNames = append(subnetNames, p.TofuName)
		case "aws_route_table":
			routeTableNames = append(routeTableNames, p.TofuName)
		}
	}
	sort.Strings(subnetNames)
	sort.Strings(routeTableNames)
	out["subnet_ids"] = listExprs("aws_subnet", subnetNames, "id")
	out["route_table_ids"] = listExprs("aws_route_table", routeTableNames, "id")
	return out
}

func outputsCompute(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	var instanceNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "aws_instance":
			instanceNames = append(instanceNames, p.TofuName)
		case "aws_security_group":
			out["security_group_id"] = fmt.Sprintf("${aws_security_group.%s.id}", p.TofuName)
		}
	}
	sort.Strings(instanceNames)
	out["instance_ids"] = listExprs("aws_instance", instanceNames, "id")
	out["private_ips"] = listExprs("aws_instance", instanceNames, "private_ip")
	return out
}

func outputsDatabase(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	for _, p := range primitives {
		if p.TofuType == "aws_db_instance" {
			out["endpoint"] = fmt.Sprintf("${aws_db_instance.%s.address}", p.TofuName)
			out["port"] = fmt.Sprintf("${aws_db_instance.%s.port}", p.TofuName)
			out["db_name"] = fmt.Sprintf("${aws_db_instance.%s.db_name}", p.TofuName)
		}
	}
	return out
}

func outputsStorage(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	for _, p := range primitives {
		if p.TofuType == "aws_s3_bucket" {
			out["bucket_name"] = fmt.Sprintf("${aws_s3_bucket.%s.bucket}", p.TofuName)
			out["bucket_arn"] = fmt.Sprintf("${aws_s3_bucket.%s.arn}", p.TofuName)
			out["bucket_url"] = fmt.Sprintf("${aws_s3_bucket.%s.bucket_regional_domain_name}", p.TofuName)
		}
	}
	return out
}

func listExprs(resourceType string, names []string, attr string) []string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("${%s.%s.%s}", resourceType, n, attr)
	}
	return parts
}
