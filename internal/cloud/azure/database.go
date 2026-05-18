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

// dbSizes maps T-shirt sizes to azurerm_postgresql_flexible_server sku_name
// values. The SKU format is {TierPrefix}_{ComputeFamily}_{VCores}:
//   - B_ = Burstable, GP_ = GeneralPurpose, MO_ = MemoryOptimized
var dbSizes = map[string]dbSizeProfile{
	"small":  {"B_Standard_B1ms", "Burstable", 1, 2, 32},
	"medium": {"B_Standard_B2s", "Burstable", 2, 4, 64},
	"large":  {"GP_Standard_D2s_v3", "GeneralPurpose", 2, 8, 128},
	"xlarge": {"GP_Standard_D4s_v3", "GeneralPurpose", 4, 16, 256},
}

var dbEngineDefaults = map[string]string{
	"postgres":   "16",
	"postgresql": "16",
	"mysql":      "8.0",
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
	// Reject mariadb early: azurerm_mariadb_server was removed in azurerm provider v4.
	if engine == "mariadb" {
		return nil, fmt.Errorf("azure: engine %q is unsupported (azurerm_mariadb_server was removed in azurerm provider v4)", engine)
	}
	defaultVer, ok := dbEngineDefaults[engine]
	if !ok {
		return nil, fmt.Errorf("azure.emitDatabase: unsupported engine %q (supported: postgres, mysql)", engine)
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
	case "postgres", "postgresql":
		serverName := azureCloudResourceName(component) + "-pg"
		serverAttrs := map[string]any{
			"name":                   serverName,
			"location":               target.Region,
			"resource_group_name":    "${azurerm_resource_group." + name + ".name}",
			"version":                version,
			"sku_name":               profile.SKU,
			"storage_mb":             profile.StorageGB * 1024,
			"administrator_login":    "nimbusfab_admin",
			"administrator_password": "DummyPassword123!",
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
				ID:           fmt.Sprintf("%s.azure-%s.db", component, target.Region),
				Cloud:        "azure",
				TofuType:     "azurerm_postgresql_flexible_server_database",
				TofuName:     name,
				TagAttribute: "-", // azurerm_postgresql_flexible_server_database does not support tags
				Attributes: map[string]any{
					"name":      "appdb",
					"server_id": "${azurerm_postgresql_flexible_server." + name + ".id}",
					"collation": "en_US.utf8",
					"charset":   "utf8",
				},
			})
	case "mysql":
		serverName := azureCloudResourceName(component) + "-mysql"
		serverAttrs := map[string]any{
			"name":                   serverName,
			"location":               target.Region,
			"resource_group_name":    "${azurerm_resource_group." + name + ".name}",
			"version":                version,
			"sku_name":               profile.SKU,
			"storage":                []any{map[string]any{"size_gb": profile.StorageGB}},
			"administrator_login":    "nimbusfab_admin",
			"administrator_password": "DummyPassword123!",
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
				ID:           fmt.Sprintf("%s.azure-%s.db", component, target.Region),
				Cloud:        "azure",
				TofuType:     "azurerm_mysql_flexible_server_database",
				TofuName:     name,
				TagAttribute: "-", // azurerm_mysql_flexible_server_database does not support tags
				Attributes: map[string]any{
					"name":                "appdb",
					"resource_group_name": "${azurerm_resource_group." + name + ".name}",
					"server_name":         "${azurerm_mysql_flexible_server." + name + ".name}",
					"charset":             "utf8",
					"collation":           "utf8_unicode_ci",
				},
			})
	default:
		return nil, fmt.Errorf("azure: unknown engine %q", engine)
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
