package gcp

import (
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

var dbTiers = map[string]string{
	"small":  "db-f1-micro",
	"medium": "db-g1-small",
	"large":  "db-custom-2-7680",
	"xlarge": "db-custom-4-15360",
}

var dbStorageDefaults = map[string]int{
	"small":  10,
	"medium": 20,
	"large":  100,
	"xlarge": 200,
}

func emitDatabaseImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	_ = refs
	engine, _ := target.Spec["engine"].(string)
	if engine == "" {
		engine = "postgres"
	}
	if engine == "mariadb" {
		return nil, fmt.Errorf("gcp.emitDatabase: %w", ErrAdapterGCPMariaDBUnsupported)
	}
	if engine != "postgres" && engine != "mysql" {
		return nil, fmt.Errorf("gcp.emitDatabase: %w (engine=%q)", ErrAdapterGCPUnsupportedEngine, engine)
	}

	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "database"
	}
	deploymentID, _ := target.Spec["__deployment_id"].(string)
	size, _ := target.Spec["size"].(string)
	if size == "" {
		size = "small"
	}
	tier, ok := dbTiers[size]
	if !ok {
		return nil, fmt.Errorf("gcp.emitDatabase: unknown size %q", size)
	}
	if compute, ok := target.Spec["compute"].(map[string]any); ok {
		if t, _ := compute["tier"].(string); t != "" {
			tier = t
		}
	}
	storageGB := dbStorageDefaults[size]
	if storage, ok := target.Spec["storage"].(map[string]any); ok {
		storageGB = intFromSpec(storage, "sizeGB", storageGB)
	}

	version := dbVersion(engine, target.Spec)
	availability := "ZONAL"
	if boolFromSpec(target.Spec, "multiAZ", false) {
		availability = "REGIONAL"
	}

	name := tofuIdent(component)
	instanceName := gcpResourceName(component, deploymentID) + "-sql"

	return []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.gcp-%s.instance", component, target.Region),
			Cloud:    "gcp",
			TofuType: "google_sql_database_instance",
			TofuName: name,
			Attributes: map[string]any{
				"name":             instanceName,
				"region":           target.Region,
				"database_version": version,
				"settings": []any{map[string]any{
					"tier":              tier,
					"availability_type": availability,
					"disk_size":         storageGB,
					"disk_type":         "PD_SSD",
				}},
				"deletion_protection": false,
			},
		},
		{
			ID:       fmt.Sprintf("%s.gcp-%s.db_default", component, target.Region),
			Cloud:    "gcp",
			TofuType: "google_sql_database",
			TofuName: name + "_default",
			Attributes: map[string]any{
				"name":     "default",
				"instance": "${google_sql_database_instance." + name + ".name}",
			},
		},
	}, nil
}

func dbVersion(engine string, spec map[string]any) string {
	if v, _ := spec["version"].(string); v != "" {
		return v
	}
	switch engine {
	case "postgres":
		return "POSTGRES_16"
	case "mysql":
		return "MYSQL_8_0"
	}
	return ""
}
