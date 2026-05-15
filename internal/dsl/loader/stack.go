package loader

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// StackValues is the parsed `stacks/<stack>/values.yaml` content.
// Var values are stored as untyped any (string, bool, number, list, map);
// type-checking happens in the validator's variable-resolution phase.
type StackValues struct {
	APIVersion         string
	Vars               map[string]any
	DisabledComponents []string
	DisabledTargets    []DisabledTarget
	rawDisabled        map[string]any
}

// DisabledTarget identifies a specific target to remove from this stack.
type DisabledTarget struct {
	Component string `yaml:"component"`
	Cloud     string `yaml:"cloud,omitempty"`
	Region    string `yaml:"region,omitempty"`
}

// UnmarshalYAML captures the apiVersion / vars / disabled fields. The
// `disabled:` block lands in rawDisabled for later typed materialization
// because its shape differs between `components: [...]` (list of strings)
// and `targets: [...]` (list of maps).
func (sv *StackValues) UnmarshalYAML(node *yaml.Node) error {
	type alias struct {
		APIVersion string         `yaml:"apiVersion"`
		Vars       map[string]any `yaml:"vars,omitempty"`
		Disabled   map[string]any `yaml:"disabled,omitempty"`
	}
	var a alias
	if err := node.Decode(&a); err != nil {
		return err
	}
	sv.APIVersion = a.APIVersion
	sv.Vars = a.Vars
	sv.rawDisabled = a.Disabled
	return nil
}

// LoadStackValues reads stacks/<stack>/values.yaml if present. If the file
// does not exist, returns an empty StackValues (not an error) so the
// validator's no-vars path is uniform.
func LoadStackValues(ctx context.Context, root, stack string) (*StackValues, error) {
	_ = ctx
	path := filepath.Join(root, "stacks", stack, "values.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &StackValues{Vars: map[string]any{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	sv := &StackValues{}
	if err := yaml.Unmarshal(body, sv); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if sv.Vars == nil {
		sv.Vars = map[string]any{}
	}
	if err := sv.materializeDisabled(); err != nil {
		return nil, fmt.Errorf("parse %s: disabled: %w", path, err)
	}
	return sv, nil
}

// materializeDisabled splits the untyped `disabled:` map into typed
// component-name and DisabledTarget lists.
func (sv *StackValues) materializeDisabled() error {
	if sv.rawDisabled == nil {
		return nil
	}
	if v, ok := sv.rawDisabled["components"]; ok {
		list, ok := v.([]any)
		if !ok {
			return errors.New("`components:` must be a list")
		}
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("disabled.components entries must be strings, got %T", item)
			}
			sv.DisabledComponents = append(sv.DisabledComponents, s)
		}
	}
	if v, ok := sv.rawDisabled["targets"]; ok {
		list, ok := v.([]any)
		if !ok {
			return errors.New("`targets:` must be a list")
		}
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("disabled.targets entries must be mappings, got %T", item)
			}
			dt := DisabledTarget{}
			if s, ok := m["component"].(string); ok {
				dt.Component = s
			}
			if s, ok := m["cloud"].(string); ok {
				dt.Cloud = s
			}
			if s, ok := m["region"].(string); ok {
				dt.Region = s
			}
			if dt.Component == "" {
				return errors.New("disabled.targets entries require component:")
			}
			sv.DisabledTargets = append(sv.DisabledTargets, dt)
		}
	}
	return nil
}
