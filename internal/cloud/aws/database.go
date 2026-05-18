package aws

import (
	"fmt"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

type dbSizeProfile struct {
	InstanceClass string
	StorageGB     int
	VCPU          int
	MemoryGB      float64
}

var dbSizes = map[string]dbSizeProfile{
	"small":  {InstanceClass: "db.t3.small", StorageGB: 100, VCPU: 2, MemoryGB: 2},
	"medium": {InstanceClass: "db.t3.medium", StorageGB: 250, VCPU: 2, MemoryGB: 4},
	"large":  {InstanceClass: "db.m6i.large", StorageGB: 500, VCPU: 2, MemoryGB: 8},
	"xlarge": {InstanceClass: "db.m6i.xlarge", StorageGB: 1000, VCPU: 4, MemoryGB: 16},
}

var dbEngineDefaults = map[string]string{
	"postgres": "16",
	"mysql":    "8.0",
	"mariadb":  "10.11",
}

func emitDatabaseImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	component, _ := target.Spec["__component"].(string)
	if component == "" {
		component = "database"
	}
	name := tofuIdentifier(component)
	awsName := awsResourceName(component)

	engine, _ := target.Spec["engine"].(string)
	if engine == "" {
		return nil, fmt.Errorf("aws.emitDatabase: spec.engine required")
	}
	defaultVer, ok := dbEngineDefaults[engine]
	if !ok {
		return nil, fmt.Errorf("aws.emitDatabase: unsupported engine %q (supported: postgres, mysql, mariadb)", engine)
	}
	version, _ := target.Spec["version"].(string)
	if version == "" {
		version = defaultVer
	}

	profile, err := resolveDBSize(target.Spec)
	if err != nil {
		return nil, fmt.Errorf("aws.emitDatabase: %w", err)
	}

	subnetIDsAttr, err := subnetIDsFromRefs(refs, "subnetIds")
	if err != nil {
		return nil, err
	}
	multiAZ := boolFromSpec(target.Spec, "multiAZ", false)
	backupRetention := 7
	if !boolFromSpec(target.Spec, "pointInTimeRestore", true) {
		backupRetention = 0
	}

	return []ir.ResourcePrimitive{
		{
			ID:       fmt.Sprintf("%s.aws-%s.subnet_group", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_db_subnet_group",
			TofuName: name,
			Attributes: map[string]any{
				"name":       awsName + "-subnet-group",
				"subnet_ids": subnetIDsAttr,
			},
		},
		{
			ID:       fmt.Sprintf("%s.aws-%s.db", component, target.Region),
			Cloud:    "aws",
			TofuType: "aws_db_instance",
			TofuName: name,
			Attributes: map[string]any{
				"identifier":              awsName,
				"engine":                  engine,
				"engine_version":          version,
				"instance_class":          profile.InstanceClass,
				"allocated_storage":       profile.StorageGB,
				"storage_type":            "gp3",
				"storage_encrypted":       true,
				"db_subnet_group_name":    "${aws_db_subnet_group." + name + ".name}",
				"multi_az":                multiAZ,
				"backup_retention_period": backupRetention,
				"skip_final_snapshot":     true,
				"publicly_accessible":     false,
			},
		},
	}, nil
}

func resolveDBSize(spec map[string]any) (dbSizeProfile, error) {
	if size, ok := spec["size"].(string); ok && size != "" {
		profile, ok := dbSizes[size]
		if !ok {
			return dbSizeProfile{}, fmt.Errorf("unknown size %q (use small/medium/large/xlarge)", size)
		}
		if explicitCompute, hasC := spec["compute"]; hasC && explicitCompute != nil {
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
	if vcpu == 0 || memGB == 0 {
		return dbSizeProfile{}, fmt.Errorf("compute.vCPU and compute.memoryGB required")
	}
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

// subnetIDsFromRefs returns the value for aws_db_subnet_group.subnet_ids.
// The provisioner populates refs["subnetIds"] with either a bare tofu
// interpolation string (evaluated to a list at plan/apply time) or a
// pre-resolved []string/[]any from state. If the ref is absent an error is
// returned — the validator and preflight should prevent this, but we fail
// loudly rather than emitting invalid tofu.
func subnetIDsFromRefs(refs cloud.ResolvedRefs, alias string) (any, error) {
	v, ok := refs[alias]
	if !ok {
		return nil, fmt.Errorf("aws.database: required ref %q not in ResolvedRefs", alias)
	}
	switch x := v.(type) {
	case []string:
		return x, nil
	case []any:
		return x, nil
	case string:
		return x, nil
	default:
		return nil, fmt.Errorf("aws.database: ref %q has unsupported type %T", alias, v)
	}
}

func boolFromSpec(spec map[string]any, key string, def bool) bool {
	if v, ok := spec[key].(bool); ok {
		return v
	}
	return def
}

func intFromMap(m any, key string, def int) int {
	asMap, _ := m.(map[string]any)
	if asMap == nil {
		return def
	}
	if v, ok := asMap[key]; ok {
		switch t := v.(type) {
		case int:
			return t
		case int64:
			return int(t)
		case float64:
			return int(t)
		}
	}
	return def
}

func floatFromMap(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case int:
			return float64(t)
		case int64:
			return float64(t)
		}
	}
	return def
}
