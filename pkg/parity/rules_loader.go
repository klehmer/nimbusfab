package parity

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParityFileDoc mirrors the parity.yaml top-level shape.
type ParityFileDoc struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Parity     ParityRulesDoc `yaml:"parity"`
}

// ParityRulesDoc mirrors the `parity:` block.
type ParityRulesDoc struct {
	Default    ModeRulesDoc                 `yaml:"default,omitempty"`
	Components map[string]ComponentRulesDoc `yaml:"components,omitempty"`
}

// ModeRulesDoc is the yaml shape for default rules.
type ModeRulesDoc struct {
	Mode     string  `yaml:"mode"`
	MinScore float64 `yaml:"minScore"`
}

// ComponentRulesDoc is the yaml shape for per-component rules.
type ComponentRulesDoc struct {
	Mode       string                        `yaml:"mode,omitempty"`
	MinScore   float64                       `yaml:"minScore,omitempty"`
	Attributes map[string]AttributePolicyDoc `yaml:"attributes,omitempty"`
}

// AttributePolicyDoc is the yaml shape for an attribute policy.
type AttributePolicyDoc struct {
	Policy   string  `yaml:"policy"`
	MaxRatio float64 `yaml:"maxRatio,omitempty"`
}

// LoadRulesFromFile parses parity.yaml from disk. Returns empty ProjectRules
// when the file is missing; that is the parity-default "informative-only" mode.
func LoadRulesFromFile(path string) (ProjectRules, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ProjectRules{}, nil
		}
		return ProjectRules{}, fmt.Errorf("parity.LoadRulesFromFile: %w", err)
	}
	return LoadRules(body)
}

// LoadRules parses parity.yaml from bytes.
func LoadRules(body []byte) (ProjectRules, error) {
	var doc ParityFileDoc
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return ProjectRules{}, fmt.Errorf("parity.LoadRules: %w", err)
	}
	return convertRulesDoc(doc.Parity), nil
}

func convertRulesDoc(doc ParityRulesDoc) ProjectRules {
	rules := ProjectRules{
		Default: ModeRules{Mode: doc.Default.Mode, MinScore: doc.Default.MinScore},
	}
	if len(doc.Components) > 0 {
		rules.Components = map[string]ComponentRules{}
		for name, c := range doc.Components {
			cr := ComponentRules{Mode: c.Mode, MinScore: c.MinScore}
			if len(c.Attributes) > 0 {
				cr.Attributes = map[string]AttributePolicy{}
				for attr, pol := range c.Attributes {
					cr.Attributes[attr] = AttributePolicy{Policy: pol.Policy, MaxRatio: pol.MaxRatio}
				}
			}
			rules.Components[name] = cr
		}
	}
	return rules
}
