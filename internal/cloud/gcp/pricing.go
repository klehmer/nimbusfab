package gcp

import (
	"strings"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func pricingKeyImpl(p ir.ResourcePrimitive) (map[string]any, error) {
	region := regionFromID(p.ID)
	switch p.TofuType {
	case "google_compute_instance":
		machineType, _ := p.Attributes["machine_type"].(string)
		return map[string]any{
			"service":        "Compute Engine",
			"resourceFamily": "Compute",
			"resourceGroup":  machineFamilyGroup(machineType),
			"usageType":      "OnDemand",
			"machineType":    machineType,
			"region":         region,
		}, nil
	case "google_sql_database_instance":
		tier := sqlTier(p)
		version, _ := p.Attributes["database_version"].(string)
		return map[string]any{
			"service":        "Cloud SQL",
			"resourceFamily": "ApplicationServices",
			"resourceGroup":  sqlResourceGroup(tier),
			"usageType":      "OnDemand",
			"tier":           tier,
			"region":         region,
			"engine":         normalizeEngine(version),
		}, nil
	case "google_storage_bucket":
		storageClass, _ := p.Attributes["storage_class"].(string)
		if storageClass == "" {
			storageClass = "STANDARD"
		}
		return map[string]any{
			"service":        "Cloud Storage",
			"resourceFamily": "Storage",
			"resourceGroup":  storageResourceGroup(storageClass),
			"usageType":      "OnDemand",
			"storageClass":   storageClass,
			"region":         region,
		}, nil
	}
	return nil, nil
}

// regionFromID extracts the region from a primitive ID like
// "<component>.gcp-<region>.<localname>".
func regionFromID(id string) string {
	const marker = ".gcp-"
	idx := strings.Index(id, marker)
	if idx < 0 {
		return ""
	}
	rest := id[idx+len(marker):]
	if dot := strings.Index(rest, "."); dot >= 0 {
		return rest[:dot]
	}
	return rest
}

func machineFamilyGroup(machineType string) string {
	if strings.HasPrefix(machineType, "e2-") {
		return "E2"
	}
	if strings.HasPrefix(machineType, "n2-") {
		return "N2"
	}
	if strings.HasPrefix(machineType, "n1-") {
		return "N1"
	}
	return "GeneralPurpose"
}

func sqlTier(p ir.ResourcePrimitive) string {
	settings, ok := p.Attributes["settings"].([]any)
	if !ok || len(settings) == 0 {
		return ""
	}
	s, ok := settings[0].(map[string]any)
	if !ok {
		return ""
	}
	tier, _ := s["tier"].(string)
	return tier
}

func sqlResourceGroup(tier string) string {
	switch tier {
	case "db-f1-micro":
		return "SQLGen2InstancesF1Micro"
	case "db-g1-small":
		return "SQLGen2InstancesG1Small"
	}
	if strings.HasPrefix(tier, "db-custom-") {
		return "SQLGen2InstancesCustom"
	}
	return "SQLGen2Instances"
}

func normalizeEngine(version string) string {
	switch {
	case strings.HasPrefix(version, "POSTGRES"):
		return "POSTGRES"
	case strings.HasPrefix(version, "MYSQL"):
		return "MYSQL"
	}
	return ""
}

func storageResourceGroup(class string) string {
	switch class {
	case "STANDARD":
		return "StandardStorage"
	case "NEARLINE":
		return "NearlineStorage"
	case "COLDLINE":
		return "ColdlineStorage"
	case "ARCHIVE":
		return "ArchiveStorage"
	}
	return "StandardStorage"
}
