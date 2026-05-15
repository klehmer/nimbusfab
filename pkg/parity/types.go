// Package parity defines normalized cloud-resource attribute shapes that
// the parity engine and cost estimator share. The full engine ships in
// its own spec/phase; this file is the type contract referenced from
// pkg/cloud/adapter.go's Profile method.
package parity

// ResourceProfile is the normalized shape adapters return from Profile().
// At least one of Compute/Storage/Database/Network MUST be non-nil for a
// valid profile (the engine asserts this in its own tests).
type ResourceProfile struct {
	Class    string
	Compute  *ComputeProfile
	Storage  *StorageProfile
	Database *DatabaseProfile
	Network  *NetworkProfile
	Features map[string]bool
	SKU      string
	Notes    []string
}

type ComputeProfile struct {
	VCPU         int
	MemoryGB     float64
	Architecture string
	NetworkGbps  float64
}

type StorageProfile struct {
	SizeGB         int
	IOPS           int
	ThroughputMBps int
	Class          string
	Encrypted      bool
}

type DatabaseProfile struct {
	Engine   string
	Version  string
	Compute  ComputeProfile
	Storage  StorageProfile
	Replicas int
	HA       bool
}

type NetworkProfile struct {
	CIDR          string
	BandwidthGbps float64
	IPv6          bool
	NAT           bool
}
