package aws

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/parity"
)

func (*Adapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
	switch p.TofuType {
	case "aws_vpc":
		cidr, _ := p.Attributes["cidr_block"].(string)
		return parity.ResourceProfile{
			Class:   "network",
			Network: &parity.NetworkProfile{CIDR: cidr, IPv6: false, NAT: false},
			SKU:     "aws_vpc",
		}, nil
	case "aws_instance":
		instanceType, _ := p.Attributes["instance_type"].(string)
		compute := lookupComputeProfile(instanceType)
		storage := storageProfileFromRootBlock(p.Attributes["root_block_device"])
		return parity.ResourceProfile{
			Class:   "compute",
			Compute: &compute,
			Storage: &storage,
			SKU:     instanceType,
		}, nil
	case "aws_db_instance":
		engine, _ := p.Attributes["engine"].(string)
		version, _ := p.Attributes["engine_version"].(string)
		instanceClass, _ := p.Attributes["instance_class"].(string)
		compute := lookupComputeProfile(stripDBPrefix(instanceClass))
		storageGB := 0
		switch t := p.Attributes["allocated_storage"].(type) {
		case int:
			storageGB = t
		case float64:
			storageGB = int(t)
		}
		multiAZ, _ := p.Attributes["multi_az"].(bool)
		backupRetention := 0
		switch t := p.Attributes["backup_retention_period"].(type) {
		case int:
			backupRetention = t
		case float64:
			backupRetention = int(t)
		}
		return parity.ResourceProfile{
			Class: "database",
			Database: &parity.DatabaseProfile{
				Engine:  engine,
				Version: version,
				Compute: compute,
				Storage: parity.StorageProfile{SizeGB: storageGB, Class: "ssd", Encrypted: true},
				HA:      multiAZ,
			},
			Features: map[string]bool{
				"pointInTimeRestore": backupRetention > 0,
				"multiAZ":            multiAZ,
			},
			SKU: instanceClass,
		}, nil
	case "aws_s3_bucket":
		return parity.ResourceProfile{
			Class:   "storage",
			Storage: &parity.StorageProfile{Class: "tiered", Encrypted: true},
			Features: map[string]bool{
				"versioning":   true,
				"publicAccess": false,
			},
			SKU: "aws_s3_standard",
		}, nil
	default:
		return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
	}
}

func lookupComputeProfile(instanceType string) parity.ComputeProfile {
	knownTypes := map[string]parity.ComputeProfile{
		"t3.small":   {VCPU: 2, MemoryGB: 2, Architecture: "x86_64", NetworkGbps: 5},
		"t3.medium":  {VCPU: 2, MemoryGB: 4, Architecture: "x86_64", NetworkGbps: 5},
		"t3.large":   {VCPU: 2, MemoryGB: 8, Architecture: "x86_64", NetworkGbps: 5},
		"t3.xlarge":  {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 5},
		"m6i.large":  {VCPU: 2, MemoryGB: 8, Architecture: "x86_64", NetworkGbps: 12.5},
		"m6i.xlarge": {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 12.5},
	}
	if p, ok := knownTypes[instanceType]; ok {
		return p
	}
	return parity.ComputeProfile{Architecture: "x86_64"}
}

func stripDBPrefix(instanceClass string) string {
	if len(instanceClass) > 3 && instanceClass[:3] == "db." {
		return instanceClass[3:]
	}
	return instanceClass
}

func storageProfileFromRootBlock(v any) parity.StorageProfile {
	blocks, _ := v.([]any)
	if len(blocks) == 0 {
		return parity.StorageProfile{Class: "ssd", Encrypted: true}
	}
	b, _ := blocks[0].(map[string]any)
	size := 0
	switch t := b["volume_size"].(type) {
	case int:
		size = t
	case float64:
		size = int(t)
	}
	encrypted, _ := b["encrypted"].(bool)
	return parity.StorageProfile{SizeGB: size, Class: "ssd", Encrypted: encrypted}
}
