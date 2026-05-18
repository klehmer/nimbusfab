package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) OutputBindings(ctx context.Context, target ir.DeploymentTarget, primitives []ir.ResourcePrimitive) (map[string]string, error) {
	t, _ := target.Spec["__type"].(string)
	switch t {
	case "network":
		return gcpOutputsNetwork(primitives), nil
	case "compute":
		return gcpOutputsCompute(primitives), nil
	case "database":
		return gcpOutputsDatabase(primitives), nil
	case "storage":
		return gcpOutputsStorage(primitives), nil
	}
	return map[string]string{}, nil
}

func gcpOutputsNetwork(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var subnetNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "google_compute_network":
			out["vpc_id"] = fmt.Sprintf("${google_compute_network.%s.id}", p.TofuName)
		case "google_compute_subnetwork":
			subnetNames = append(subnetNames, p.TofuName)
		}
	}
	sort.Strings(subnetNames)
	out["subnet_ids"] = gcpListExpr("google_compute_subnetwork", subnetNames, "id")
	out["route_table_ids"] = "[]"
	return out
}

func gcpOutputsCompute(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	var instNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "google_compute_instance":
			instNames = append(instNames, p.TofuName)
		case "google_compute_firewall":
			if _, set := out["security_group_id"]; !set {
				out["security_group_id"] = fmt.Sprintf("${google_compute_firewall.%s.id}", p.TofuName)
			}
		}
	}
	sort.Strings(instNames)
	out["instance_ids"] = gcpListExpr("google_compute_instance", instNames, "id")
	out["private_ips"] = gcpListExpr("google_compute_instance", instNames, "network_interface.0.network_ip")
	return out
}

func gcpOutputsDatabase(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		if p.TofuType == "google_sql_database_instance" {
			out["endpoint"] = fmt.Sprintf("${google_sql_database_instance.%s.public_ip_address}", p.TofuName)
			out["port"] = "5432"
			out["db_name"] = fmt.Sprintf("${google_sql_database_instance.%s.name}", p.TofuName)
		}
	}
	return out
}

func gcpOutputsStorage(primitives []ir.ResourcePrimitive) map[string]string {
	out := map[string]string{}
	for _, p := range primitives {
		if p.TofuType == "google_storage_bucket" {
			out["bucket_name"] = fmt.Sprintf("${google_storage_bucket.%s.name}", p.TofuName)
			out["bucket_arn"] = fmt.Sprintf("${google_storage_bucket.%s.id}", p.TofuName)
			out["bucket_url"] = fmt.Sprintf("${google_storage_bucket.%s.url}", p.TofuName)
		}
	}
	return out
}

func gcpListExpr(resourceType string, names []string, attr string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("${%s.%s.%s}", resourceType, n, attr)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
