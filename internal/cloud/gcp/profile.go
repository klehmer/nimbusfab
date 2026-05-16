package gcp

import (
	"strings"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

func profileImpl(p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	switch p.TofuType {
	case "google_compute_network":
		return parity.ResourceProfile{
			Class:   "network",
			Network: &parity.NetworkProfile{IPv6: false, NAT: false},
			SKU:     "google_compute_network",
		}, nil
	case "google_compute_instance":
		mt, _ := p.Attributes["machine_type"].(string)
		compute := lookupMachineProfile(mt)
		storageGB := 30
		if disks, ok := p.Attributes["boot_disk"].([]any); ok && len(disks) > 0 {
			if d, ok := disks[0].(map[string]any); ok {
				if init, ok := d["initialize_params"].([]any); ok && len(init) > 0 {
					if ip, ok := init[0].(map[string]any); ok {
						switch t := ip["size"].(type) {
						case int:
							storageGB = t
						case float64:
							storageGB = int(t)
						}
					}
				}
			}
		}
		return parity.ResourceProfile{
			Class:   "compute",
			Compute: &compute,
			Storage: &parity.StorageProfile{SizeGB: storageGB, Class: "ssd", Encrypted: true},
			SKU:     mt,
		}, nil
	case "google_sql_database_instance":
		tier := sqlTier(p)
		version, _ := p.Attributes["database_version"].(string)
		engine := strings.ToLower(normalizeEngine(version))
		compute := lookupSQLComputeProfile(tier)
		storageGB := 0
		availability := "ZONAL"
		if settings, ok := p.Attributes["settings"].([]any); ok && len(settings) > 0 {
			if s, ok := settings[0].(map[string]any); ok {
				switch t := s["disk_size"].(type) {
				case int:
					storageGB = t
				case float64:
					storageGB = int(t)
				}
				if a, _ := s["availability_type"].(string); a != "" {
					availability = a
				}
			}
		}
		hasHA := availability == "REGIONAL"
		return parity.ResourceProfile{
			Class: "database",
			Database: &parity.DatabaseProfile{
				Engine:  engine,
				Version: version,
				Compute: compute,
				Storage: parity.StorageProfile{SizeGB: storageGB, Class: "ssd", Encrypted: true},
				HA:      hasHA,
			},
			Features: map[string]bool{
				"pointInTimeRestore": true,
				"multiAZ":            hasHA,
			},
			SKU: tier,
		}, nil
	case "google_storage_bucket":
		versioning := false
		if v, ok := p.Attributes["versioning"].([]any); ok && len(v) > 0 {
			if vm, ok := v[0].(map[string]any); ok {
				if b, ok := vm["enabled"].(bool); ok {
					versioning = b
				}
			}
		}
		prevention, _ := p.Attributes["public_access_prevention"].(string)
		return parity.ResourceProfile{
			Class:   "storage",
			Storage: &parity.StorageProfile{Class: "object", Encrypted: true},
			Features: map[string]bool{
				"versioning":   versioning,
				"publicAccess": prevention != "enforced",
			},
			SKU: "STANDARD",
		}, nil
	default:
		return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
	}
}

func lookupMachineProfile(mt string) parity.ComputeProfile {
	known := map[string]parity.ComputeProfile{
		"e2-small":      {VCPU: 2, MemoryGB: 2, Architecture: "x86_64", NetworkGbps: 1},
		"e2-medium":     {VCPU: 2, MemoryGB: 4, Architecture: "x86_64", NetworkGbps: 2},
		"e2-standard-2": {VCPU: 2, MemoryGB: 8, Architecture: "x86_64", NetworkGbps: 4},
		"n2-standard-4": {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 10},
	}
	if p, ok := known[mt]; ok {
		return p
	}
	return parity.ComputeProfile{Architecture: "x86_64"}
}

func lookupSQLComputeProfile(tier string) parity.ComputeProfile {
	known := map[string]parity.ComputeProfile{
		"db-f1-micro":       {VCPU: 1, MemoryGB: 0.6, Architecture: "x86_64"},
		"db-g1-small":       {VCPU: 1, MemoryGB: 1.7, Architecture: "x86_64"},
		"db-custom-2-7680":  {VCPU: 2, MemoryGB: 7.5, Architecture: "x86_64"},
		"db-custom-4-15360": {VCPU: 4, MemoryGB: 15, Architecture: "x86_64"},
	}
	if p, ok := known[tier]; ok {
		return p
	}
	return parity.ComputeProfile{Architecture: "x86_64"}
}
