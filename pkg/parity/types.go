// Package parity defines normalized cloud-resource attribute shapes that
// adapters return from Profile(), plus the parity Engine that compares
// per-target profiles into cross-cloud ParityReports and evaluates per-project
// parity rules.
package parity

import "time"

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

// TargetProfile is one cloud target's profile data plus identity.
type TargetProfile struct {
	DeploymentTargetID string
	Cloud              string
	Region             string
	Profile            ResourceProfile
}

// AttrComparison is one attribute's cross-target comparison.
type AttrComparison struct {
	Attribute string         // dotted: "compute.vCPU", "features.pointInTimeRestore"
	Kind      string         // "exact" | "numeric" | "boolean"
	Values    map[string]any // keyed by "<cloud>/<region>"
	AllMatch  bool
	MinValue  any
	MaxValue  any
	Score     float64
}

// ParityReport is the complete comparison output for one component.
type ParityReport struct {
	Component   string
	Type        string
	Size        string
	Contract    ContractFloor
	Targets     []TargetProfile
	Comparisons []AttrComparison
	Score       float64
	Warnings    []string
	GeneratedAt time.Time
}

// Violation is one rule-evaluator result.
type Violation struct {
	Component string
	Attribute string
	Policy    string // "exact" | "maxRatio" | "requireAll" | "minScore"
	Detail    string
	Action    string // "warn" | "block"
}

// ProjectRules is the parsed parity.yaml.
type ProjectRules struct {
	Default    ModeRules
	Components map[string]ComponentRules
}

// ModeRules applies when no per-component rule exists.
type ModeRules struct {
	Mode     string
	MinScore float64
}

// ComponentRules is one component's parity ruleset.
type ComponentRules struct {
	Mode       string
	MinScore   float64
	Attributes map[string]AttributePolicy
}

// AttributePolicy is one per-attribute rule.
type AttributePolicy struct {
	Policy   string // "exact" | "maxRatio" | "requireAll"
	MaxRatio float64
}
