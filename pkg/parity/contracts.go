package parity

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed contracts/*.yaml
var contractFS embed.FS

// ContractFloor is the resolved minimum-guarantees record for one
// (component type, T-shirt size) pair.
type ContractFloor struct {
	Type     string
	Size     string
	Compute  *ComputeFloor
	Storage  *StorageFloor
	Network  *NetworkFloor
	Features map[string]string
}

// ComputeFloor expresses minimums per the spec.
type ComputeFloor struct {
	MinVCPU     int     `yaml:"minVCPU"`
	MinMemoryGB float64 `yaml:"minMemoryGB"`
}

// StorageFloor expresses storage minimums.
type StorageFloor struct {
	MinSizeGB int      `yaml:"minSizeGB"`
	MinIOPS   int      `yaml:"minIOPS"`
	ClassIn   []string `yaml:"classIn"`
	Encrypted string   `yaml:"encrypted"`
}

// NetworkFloor expresses network minimums.
type NetworkFloor struct {
	MinSubnets int `yaml:"minSubnets"`
}

type contractDoc struct {
	APIVersion  string               `yaml:"apiVersion"`
	Kind        string               `yaml:"kind"`
	Type        string               `yaml:"type"`
	Description string               `yaml:"description"`
	Sizes       map[string]sizeEntry `yaml:"sizes"`
}

type sizeEntry struct {
	Compute  *ComputeFloor     `yaml:"compute,omitempty"`
	Storage  *StorageFloor     `yaml:"storage,omitempty"`
	Network  *NetworkFloor     `yaml:"network,omitempty"`
	Features map[string]string `yaml:"features,omitempty"`
}

// Contracts holds the parsed catalog keyed by (type, size).
type Contracts struct {
	floors map[string]map[string]ContractFloor
}

// LoadContracts parses the embedded YAML files.
func LoadContracts() (*Contracts, error) {
	out := &Contracts{floors: map[string]map[string]ContractFloor{}}
	entries, err := contractFS.ReadDir("contracts")
	if err != nil {
		return nil, fmt.Errorf("parity.LoadContracts: %w", err)
	}
	for _, e := range entries {
		body, err := contractFS.ReadFile("contracts/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("parity.LoadContracts: read %s: %w", e.Name(), err)
		}
		var doc contractDoc
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("parity.LoadContracts: parse %s: %w", e.Name(), err)
		}
		if doc.Type == "" {
			return nil, fmt.Errorf("parity.LoadContracts: %s missing 'type'", e.Name())
		}
		sizes := map[string]ContractFloor{}
		for sizeName, entry := range doc.Sizes {
			sizes[sizeName] = ContractFloor{
				Type: doc.Type, Size: sizeName,
				Compute: entry.Compute, Storage: entry.Storage, Network: entry.Network,
				Features: entry.Features,
			}
		}
		out.floors[doc.Type] = sizes
	}
	return out, nil
}

// Lookup returns the floor for (type, size). Both must be non-empty.
func (c *Contracts) Lookup(componentType, size string) (ContractFloor, bool) {
	if c == nil {
		return ContractFloor{}, false
	}
	sizes, ok := c.floors[componentType]
	if !ok {
		return ContractFloor{}, false
	}
	f, ok := sizes[size]
	return f, ok
}

// SizesFor returns the list of sizes defined for a type.
func (c *Contracts) SizesFor(componentType string) []string {
	sizes := c.floors[componentType]
	out := make([]string, 0, len(sizes))
	for k := range sizes {
		out = append(out, k)
	}
	return out
}
