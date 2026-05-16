package components

import (
	"context"
	_ "embed"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

//go:embed schema/v1alpha1/network.json
var networkSchema []byte

//go:embed schema/v1alpha1/compute.json
var computeSchema []byte

//go:embed schema/v1alpha1/database.json
var databaseSchema []byte

//go:embed schema/v1alpha1/storage.json
var storageSchema []byte

// builtinType is the common implementation for all four v1 types. The Type
// interface's Emit method delegates to the adapter; in-tree types don't add
// per-type Go logic — the adapter's Emit() handles dispatch.
type builtinType struct {
	name            string
	schema          []byte
	supportedClouds []string
	outputs         map[string]OutputType
}

func (t builtinType) Name() string                   { return t.name }
func (t builtinType) SpecSchema() []byte             { return t.schema }
func (t builtinType) SupportedClouds() []string      { return t.supportedClouds }
func (t builtinType) Outputs() map[string]OutputType { return t.outputs }

func (t builtinType) Emit(ctx context.Context, target ir.DeploymentTarget, adapter cloud.Adapter, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
	return adapter.Emit(ctx, target, refs)
}

// Network returns the built-in "network" component type.
func Network() Type {
	return builtinType{
		name: "network", schema: networkSchema,
		supportedClouds: []string{"aws", "azure", "gcp"},
		outputs: map[string]OutputType{
			"vpc_id":          {Kind: "string", Description: "The VPC ID"},
			"subnet_ids":      {Kind: "list<string>", Description: "Subnet IDs in declaration order"},
			"route_table_ids": {Kind: "list<string>", Description: "Route table IDs"},
		},
	}
}

// Compute returns the built-in "compute" component type.
func Compute() Type {
	return builtinType{
		name: "compute", schema: computeSchema,
		supportedClouds: []string{"aws", "azure", "gcp"},
		outputs: map[string]OutputType{
			"instance_ids":      {Kind: "list<string>", Description: "Instance IDs"},
			"private_ips":       {Kind: "list<string>", Description: "Primary private IP per instance"},
			"security_group_id": {Kind: "string", Description: "Security group / firewall ID"},
		},
	}
}

// Database returns the built-in "database" component type.
func Database() Type {
	return builtinType{
		name: "database", schema: databaseSchema,
		supportedClouds: []string{"aws", "azure", "gcp"},
		outputs: map[string]OutputType{
			"endpoint": {Kind: "string", Description: "DB endpoint hostname"},
			"port":     {Kind: "integer", Description: "DB port"},
			"db_name":  {Kind: "string", Description: "Default DB name"},
		},
	}
}

// Storage returns the built-in "storage" component type.
func Storage() Type {
	return builtinType{
		name: "storage", schema: storageSchema,
		supportedClouds: []string{"aws", "azure", "gcp"},
		outputs: map[string]OutputType{
			"bucket_name": {Kind: "string", Description: "S3 / GCS / Blob bucket name"},
			"bucket_arn":  {Kind: "string", Description: "Cloud-native bucket identifier"},
			"bucket_url":  {Kind: "string", Description: "HTTPS endpoint for the bucket"},
		},
	}
}

// DefaultRegistry returns a Registry populated with the four v1 component types.
func DefaultRegistry() Registry {
	r := NewInMemoryRegistry()
	_ = r.Register(Network())
	_ = r.Register(Compute())
	_ = r.Register(Database())
	_ = r.Register(Storage())
	return r
}
