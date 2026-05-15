package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRun_ProducesExpectedSchemas is a regression test for the schemagen
// tool. It runs the real entry point against a temp directory and asserts
// that the produced JSON Schemas reflect the tags on the pkg/ir types.
// A regression in main.go's reflector configuration (e.g., dropping
// RequiredFromJSONSchemaTags) would change the output and fail this test.
func TestRun_ProducesExpectedSchemas(t *testing.T) {
	out := t.TempDir()
	if err := run(out); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, name := range []string{"project.json", "component.json", "composition.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(out, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if len(data) < 100 {
				t.Fatalf("%s too small (%d bytes); reflector likely produced empty output", name, len(data))
			}
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			if doc["$schema"] == nil {
				t.Errorf("%s: missing $schema", name)
			}
			if doc["$defs"] == nil {
				t.Errorf("%s: missing $defs", name)
			}
		})
	}

	// Validate the Project schema in detail: it must enforce the apiVersion
	// enum and the DNS-1123 name pattern from pkg/ir/types.go.
	data, err := os.ReadFile(filepath.Join(out, "project.json"))
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse project.json: %v", err)
	}
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatal("project.json: $defs is not a map")
	}
	projectDef, ok := defs["Project"].(map[string]any)
	if !ok {
		t.Fatal("project.json: $defs.Project is not a map")
	}

	required, _ := projectDef["required"].([]any)
	requiredSet := map[string]bool{}
	for _, r := range required {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}
	for _, want := range []string{"apiVersion", "name", "stacks"} {
		if !requiredSet[want] {
			t.Errorf("project.json: required missing %q (got %v)", want, required)
		}
	}

	props, ok := projectDef["properties"].(map[string]any)
	if !ok {
		t.Fatal("project.json: properties is not a map")
	}
	apiVer, ok := props["apiVersion"].(map[string]any)
	if !ok {
		t.Fatal("project.json: properties.apiVersion missing")
	}
	enum, _ := apiVer["enum"].([]any)
	foundEnum := false
	for _, e := range enum {
		if s, ok := e.(string); ok && s == "infra.dev/v1alpha1" {
			foundEnum = true
		}
	}
	if !foundEnum {
		t.Errorf("project.json: apiVersion enum missing infra.dev/v1alpha1 (got %v)", enum)
	}

	name, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatal("project.json: properties.name missing")
	}
	if pat, _ := name["pattern"].(string); pat != "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" {
		t.Errorf("project.json: name.pattern = %q, want DNS-1123 regex", pat)
	}
	if ml, _ := name["maxLength"].(float64); int(ml) != 63 {
		t.Errorf("project.json: name.maxLength = %v, want 63", ml)
	}

	// AdditionalProperties: false must be respected on Project.
	if ap, ok := projectDef["additionalProperties"].(bool); !ok || ap {
		t.Errorf("project.json: additionalProperties should be false, got %v", projectDef["additionalProperties"])
	}
}
