package validator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// phase4TypeSpecImpl validates each component's spec against the JSON Schema
// declared by its registered Type. Failures become ir.Issues at
// components[i].spec.<field>. Unknown type names become a single
// ErrValidatorUnknownType issue per component.
func phase4TypeSpecImpl(proj *ir.Project, registry components.Registry, report *ir.ValidationReport) error {
	if registry == nil {
		return fmt.Errorf("validator phase 4: nil registry (call validator.New with components.DefaultRegistry())")
	}
	cache := map[string]*jsonschema.Schema{}

	for i, comp := range proj.Components {
		t, ok := registry.Type(comp.Type)
		if !ok {
			report.Issues = append(report.Issues, ir.Issue{
				Severity: ir.SeverityError,
				Code:     "ErrValidatorUnknownType",
				Message:  fmt.Sprintf("component type %q is not registered (known: %s)", comp.Type, strings.Join(registry.Types(), ", ")),
				Path:     fmt.Sprintf("components[%d].type", i),
			})
			continue
		}
		schema, err := lookupOrCompile(cache, comp.Type, t.SpecSchema())
		if err != nil {
			return fmt.Errorf("phase 4: compile %s schema: %w", comp.Type, err)
		}
		specBytes, err := json.Marshal(comp.Spec)
		if err != nil {
			return fmt.Errorf("phase 4: marshal component %q spec: %w", comp.Name, err)
		}
		var doc any
		if err := json.Unmarshal(specBytes, &doc); err != nil {
			return fmt.Errorf("phase 4: unmarshal component %q spec: %w", comp.Name, err)
		}
		if doc == nil {
			doc = map[string]any{}
		}
		if err := schema.Validate(doc); err != nil {
			appendTypeSpecIssues(report, err, i)
		}
	}
	return nil
}

func lookupOrCompile(cache map[string]*jsonschema.Schema, name string, schemaBytes []byte) (*jsonschema.Schema, error) {
	if s, ok := cache[name]; ok {
		return s, nil
	}
	compiler := jsonschema.NewCompiler()
	resourceURL := "spec-" + name + ".json"
	if err := compiler.AddResource(resourceURL, strings.NewReader(string(schemaBytes))); err != nil {
		return nil, err
	}
	s, err := compiler.Compile(resourceURL)
	if err != nil {
		return nil, err
	}
	cache[name] = s
	return s, nil
}

// appendTypeSpecIssues mirrors appendSchemaIssues from phase 3 but prefixes
// the path with components[i].spec and uses ErrValidatorTypeSpec as the code.
func appendTypeSpecIssues(report *ir.ValidationReport, err error, componentIdx int) {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrValidatorTypeSpec",
			Message:  err.Error(),
			Path:     fmt.Sprintf("components[%d].spec", componentIdx),
		})
		return
	}
	prefix := fmt.Sprintf("components[%d].spec", componentIdx)
	for _, leaf := range collectLeaves(ve) {
		sub := pointerToPath(leaf.InstanceLocation)
		path := prefix
		if sub != "" {
			path = prefix + "." + sub
		}
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrValidatorTypeSpec",
			Message:  leaf.Message,
			Path:     path,
		})
	}
}
