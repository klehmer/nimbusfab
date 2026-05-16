package azure

import (
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

func profileImpl(p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	switch p.TofuType {
	case "azurerm_virtual_network":
		cidr := ""
		if addrs, ok := p.Attributes["address_space"].([]any); ok && len(addrs) > 0 {
			cidr, _ = addrs[0].(string)
		}
		return parity.ResourceProfile{
			Class:   "network",
			Network: &parity.NetworkProfile{CIDR: cidr, IPv6: false, NAT: false},
			SKU:     "azurerm_virtual_network",
		}, nil
	case "azurerm_linux_virtual_machine":
		size, _ := p.Attributes["size"].(string)
		compute := lookupVMProfile(size)
		storageGB := 30
		if disks, ok := p.Attributes["os_disk"].([]any); ok && len(disks) > 0 {
			if d, ok := disks[0].(map[string]any); ok {
				switch t := d["disk_size_gb"].(type) {
				case int:
					storageGB = t
				case float64:
					storageGB = int(t)
				}
			}
		}
		return parity.ResourceProfile{
			Class:   "compute",
			Compute: &compute,
			Storage: &parity.StorageProfile{SizeGB: storageGB, Class: "ssd", Encrypted: true},
			SKU:     size,
		}, nil
	case "azurerm_postgresql_flexible_server":
		sku, _ := p.Attributes["sku_name"].(string)
		version, _ := p.Attributes["version"].(string)
		compute := lookupDBComputeProfile(sku)
		storageMB := 0
		switch t := p.Attributes["storage_mb"].(type) {
		case int:
			storageMB = t
		case float64:
			storageMB = int(t)
		}
		hasHA := false
		if ha, ok := p.Attributes["high_availability"].([]any); ok && len(ha) > 0 {
			hasHA = true
		}
		return parity.ResourceProfile{
			Class: "database",
			Database: &parity.DatabaseProfile{
				Engine:  "postgres",
				Version: version,
				Compute: compute,
				Storage: parity.StorageProfile{SizeGB: storageMB / 1024, Class: "ssd", Encrypted: true},
				HA:      hasHA,
			},
			Features: map[string]bool{
				"pointInTimeRestore": true, // Azure Flexible Server includes PITR
				"multiAZ":            hasHA,
			},
			SKU: sku,
		}, nil
	case "azurerm_mysql_flexible_server", "azurerm_mariadb_server":
		engine := "mysql"
		if p.TofuType == "azurerm_mariadb_server" {
			engine = "mariadb"
		}
		sku, _ := p.Attributes["sku_name"].(string)
		version, _ := p.Attributes["version"].(string)
		compute := lookupDBComputeProfile(sku)
		return parity.ResourceProfile{
			Class: "database",
			Database: &parity.DatabaseProfile{
				Engine: engine, Version: version,
				Compute: compute,
				Storage: parity.StorageProfile{Class: "ssd", Encrypted: true},
			},
			SKU: sku,
		}, nil
	case "azurerm_storage_account":
		return parity.ResourceProfile{
			Class:   "storage",
			Storage: &parity.StorageProfile{Class: "tiered", Encrypted: true},
			Features: map[string]bool{
				"versioning":   true,
				"publicAccess": false,
			},
			SKU: "Standard_LRS",
		}, nil
	default:
		return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
	}
}

func lookupVMProfile(sku string) parity.ComputeProfile {
	knownSKUs := map[string]parity.ComputeProfile{
		"Standard_B2s":    {VCPU: 2, MemoryGB: 4, Architecture: "x86_64", NetworkGbps: 12.5},
		"Standard_B2ms":   {VCPU: 2, MemoryGB: 8, Architecture: "x86_64", NetworkGbps: 12.5},
		"Standard_B4ms":   {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 12.5},
		"Standard_D4s_v5": {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 12.5},
	}
	if p, ok := knownSKUs[sku]; ok {
		return p
	}
	return parity.ComputeProfile{Architecture: "x86_64"}
}

func lookupDBComputeProfile(sku string) parity.ComputeProfile {
	knownSKUs := map[string]parity.ComputeProfile{
		"Standard_B1ms":   {VCPU: 1, MemoryGB: 2, Architecture: "x86_64"},
		"Standard_B2s":    {VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
		"Standard_B2ms":   {VCPU: 2, MemoryGB: 8, Architecture: "x86_64"},
		"Standard_D2s_v3": {VCPU: 2, MemoryGB: 8, Architecture: "x86_64"},
		"Standard_D4s_v3": {VCPU: 4, MemoryGB: 16, Architecture: "x86_64"},
	}
	if p, ok := knownSKUs[sku]; ok {
		return p
	}
	return parity.ComputeProfile{Architecture: "x86_64"}
}
