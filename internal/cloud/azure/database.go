package azure

import (
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

type dbSizeProfile struct {
	SKU       string
	Tier      string
	VCPU      int
	MemoryGB  float64
	StorageGB int
}

var dbSizes = map[string]dbSizeProfile{
	"small":  {"Standard_B1ms", "Burstable", 1, 2, 100},
	"medium": {"Standard_B2s", "Burstable", 2, 4, 250},
	"large":  {"Standard_D2s_v3", "GeneralPurpose", 2, 8, 500},
	"xlarge": {"Standard_D4s_v3", "GeneralPurpose", 4, 16, 1000},
}

var dbEngineDefaults = map[string]string{
	"postgres": "16",
	"mysql":    "8.0",
	"mariadb":  "10.3",
}

func emitDatabaseImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "database"
	}
	name := tofuIdent(component)
	rgName := resourceGroupName(component, target.Region)

	engine, _ := target.Spec["engine"].(string)
	if engine == "" {
		return nil, fmt.Errorf("azure.emitDatabase: spec.engine required")
	}
	defaultVer, ok := dbEngineDefaults[engine]
	if !ok {
		return nil, fmt.Errorf("azure.emitDatabase: unsupported engine %q (supported: postgres, mysql, mariadb)", engine)
	}
	version, _ := target.Spec["version"].(string)
	if version == "" {
		version = defaultVer
	}

	profile, err := resolveDBSize(target.Spec)
	if err != nil {
		return nil, fmt.Errorf("azure.emitDatabase: %w", err)
	}

	multiAZ := boolFromSpec(target.Spec, "multiAZ", false)

	out := []ir.ResourcePrimitive{{
		ID:       fmt.Sprintf("%s.azure-%s.rg", component, target.Region),
		Cloud:    "azure",
		TofuType: "azurerm_resource_group",
		TofuName: name,
		Attributes: map[string]any{
			"name":     rgName,
			"location": target.Region,
		},
	}}

	switch engine {
	case "postgres":
		serverAttrs := map[string]any{
			"name":                   name + "-pg",
			"location":               target.Region,
			"resource_group_name":    "${azurerm_resource_group." + name + ".name}",
			"version":                version,
			"sku_name":               profile.SKU,
			"storage_mb":             profile.StorageGB * 1024,
			"administrator_login":    "pgadmin",
			"administrator_password": "${var.db_password}",
			"zone":                   "1",
		}
		if multiAZ {
			serverAttrs["high_availability"] = []any{map[string]any{"mode": "ZoneRedundant"}}
		}
		out = append(out,
			ir.ResourcePrimitive{
				ID:         fmt.Sprintf("%s.azure-%s.server", component, target.Region),
				Cloud:      "azure",
				TofuType:   "azurerm_postgresql_flexible_server",
				TofuName:   name,
				Attributes: serverAttrs,
			},
			ir.ResourcePrimitive{
				ID:       fmt.Sprintf("%s.azure-%s.db", component, target.Region),
				Cloud:    "azure",
				TofuType: "azurerm_postgresql_flexible_server_database",
				TofuName: name,
				Attributes: map[string]any{
					"name":      "appdb",
					"server_id": "${azurerm_postgresql_flexible_server." + name + ".id}",
					"collation": "en_US.utf8",
					"charset":   "utf8",
				},
			})
	case "mysql":
		serverAttrs := map[string]any{
			"name":                   name + "-mysql",
			"location":               target.Region,
			"resource_group_name":    "${azurerm_resource_group." + name + ".name}",
			"version":                version,
			"sku_name":               profile.SKU,
			"storage":                []any{map[string]any{"size_gb": profile.StorageGB}},
			"administrator_login":    "mysqladmin",
			"administrator_password": "${var.db_password}",
			"zone":                   "1",
		}
		if multiAZ {
			serverAttrs["high_availability"] = []any{map[string]any{"mode": "ZoneRedundant"}}
		}
		out = append(out,
			ir.ResourcePrimitive{
				ID:         fmt.Sprintf("%s.azure-%s.server", component, target.Region),
				Cloud:      "azure",
				TofuType:   "azurerm_mysql_flexible_server",
				TofuName:   name,
				Attributes: serverAttrs,
			},
			ir.ResourcePrimitive{
				ID:       fmt.Sprintf("%s.azure-%s.db", component, target.Region),
				Cloud:    "azure",
				TofuType: "azurerm_mysql_flexible_server_database",
				TofuName: name,
				Attributes: map[string]any{
					"name":                "appdb",
					"resource_group_name": "${azurerm_resource_group." + name + ".name}",
					"server_name":         "${azurerm_mysql_flexible_server." + name + ".name}",
					"charset":             "utf8",
					"collation":           "utf8_unicode_ci",
				},
			})
	case "mariadb":
		// Classic MariaDB — Azure deprecated MariaDB Flexible Server.
		out = append(out,
			ir.ResourcePrimitive{
				ID:       fmt.Sprintf("%s.azure-%s.server", component, target.Region),
				Cloud:    "azure",
				TofuType: "azurerm_mariadb_server",
				TofuName: name,
				Attributes: map[string]any{
					"name":                          name + "-mariadb",
					"location":                      target.Region,
					"resource_group_name":           "${azurerm_resource_group." + name + ".name}",
					"version":                       version,
					"sku_name":                      "GP_Gen5_2",
					"storage_mb":                    profile.StorageGB * 1024,
					"administrator_login":           "mariaadmin",
					"administrator_login_password":  "${var.db_password}",
					"public_network_access_enabled": false,
				},
			},
			ir.ResourcePrimitive{
				ID:       fmt.Sprintf("%s.azure-%s.db", component, target.Region),
				Cloud:    "azure",
				TofuType: "azurerm_mariadb_database",
				TofuName: name,
				Attributes: map[string]any{
					"name":                "appdb",
					"resource_group_name": "${azurerm_resource_group." + name + ".name}",
					"server_name":         "${azurerm_mariadb_server." + name + ".name}",
					"charset":             "utf8",
					"collation":           "utf8_general_ci",
				},
			})
	}
	return out, nil
}

func resolveDBSize(spec map[string]any) (dbSizeProfile, error) {
	if size, ok := spec["size"].(string); ok && size != "" {
		profile, ok := dbSizes[size]
		if !ok {
			return dbSizeProfile{}, fmt.Errorf("unknown size %q (use small/medium/large/xlarge)", size)
		}
		if _, hasC := spec["compute"]; hasC {
			return dbSizeProfile{}, fmt.Errorf("size and compute are mutually exclusive")
		}
		return profile, nil
	}
	compute, _ := spec["compute"].(map[string]any)
	if compute == nil {
		return dbSizeProfile{}, fmt.Errorf("spec.size or spec.compute required")
	}
	vcpu := intFromMap(compute, "vCPU", 0)
	memGB := floatFromMap(compute, "memoryGB", 0)
	for _, sz := range []string{"small", "medium", "large", "xlarge"} {
		p := dbSizes[sz]
		if p.VCPU >= vcpu && p.MemoryGB >= memGB {
			if s, _ := spec["storage"].(map[string]any); s != nil {
				p.StorageGB = intFromMap(s, "sizeGB", p.StorageGB)
			}
			return p, nil
		}
	}
	return dbSizeProfile{}, fmt.Errorf("no T-shirt size satisfies vCPU>=%d memoryGB>=%v", vcpu, memGB)
}

func boolFromSpec(spec map[string]any, key string, def bool) bool {
	if v, ok := spec[key].(bool); ok {
		return v
	}
	return def
}
