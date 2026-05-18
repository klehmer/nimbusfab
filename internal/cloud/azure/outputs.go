package azure

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
		return azureOutputsNetwork(primitives), nil
	case "compute":
		return azureOutputsCompute(primitives), nil
	case "database":
		return azureOutputsDatabase(primitives), nil
	case "storage":
		return azureOutputsStorage(primitives), nil
	}
	return map[string]any{}, nil
}

func azureOutputsNetwork(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	var subnetNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "azurerm_virtual_network":
			out["vpc_id"] = fmt.Sprintf("${azurerm_virtual_network.%s.id}", p.TofuName)
		case "azurerm_subnet":
			subnetNames = append(subnetNames, p.TofuName)
		}
	}
	sort.Strings(subnetNames)
	out["subnet_ids"] = azureListExprs("azurerm_subnet", subnetNames, "id")
	// Azure has no direct route-table-per-subnet primitive in our adapter; empty list.
	out["route_table_ids"] = []string{}
	return out
}

func azureOutputsCompute(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	var vmNames []string
	for _, p := range primitives {
		switch p.TofuType {
		case "azurerm_linux_virtual_machine":
			vmNames = append(vmNames, p.TofuName)
		case "azurerm_network_security_group":
			out["security_group_id"] = fmt.Sprintf("${azurerm_network_security_group.%s.id}", p.TofuName)
		}
	}
	sort.Strings(vmNames)
	out["instance_ids"] = azureListExprs("azurerm_linux_virtual_machine", vmNames, "id")
	out["private_ips"] = azureListExprs("azurerm_linux_virtual_machine", vmNames, "private_ip_address")
	return out
}

func azureOutputsDatabase(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	for _, p := range primitives {
		switch p.TofuType {
		case "azurerm_postgresql_flexible_server":
			out["endpoint"] = fmt.Sprintf("${azurerm_postgresql_flexible_server.%s.fqdn}", p.TofuName)
			out["port"] = "5432"
			out["db_name"] = fmt.Sprintf("${azurerm_postgresql_flexible_server.%s.name}", p.TofuName)
		case "azurerm_mysql_flexible_server":
			out["endpoint"] = fmt.Sprintf("${azurerm_mysql_flexible_server.%s.fqdn}", p.TofuName)
			out["port"] = "3306"
			out["db_name"] = fmt.Sprintf("${azurerm_mysql_flexible_server.%s.name}", p.TofuName)
		case "azurerm_mariadb_server":
			out["endpoint"] = fmt.Sprintf("${azurerm_mariadb_server.%s.fqdn}", p.TofuName)
			out["port"] = "3306"
			out["db_name"] = fmt.Sprintf("${azurerm_mariadb_server.%s.name}", p.TofuName)
		}
	}
	return out
}

func azureOutputsStorage(primitives []ir.ResourcePrimitive) map[string]any {
	out := map[string]any{}
	for _, p := range primitives {
		if p.TofuType == "azurerm_storage_account" {
			out["bucket_name"] = fmt.Sprintf("${azurerm_storage_account.%s.name}", p.TofuName)
			out["bucket_arn"] = fmt.Sprintf("${azurerm_storage_account.%s.id}", p.TofuName)
			out["bucket_url"] = fmt.Sprintf("${azurerm_storage_account.%s.primary_blob_endpoint}", p.TofuName)
		}
	}
	return out
}

func azureListExprs(resourceType string, names []string, attr string) []string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("${%s.%s.%s}", resourceType, n, attr)
	}
	return parts
}
