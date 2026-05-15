package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"
)

// A minimal type used to exercise the reflection path. The real run targets
// pkg/ir types, but we don't import them here to keep the tool free of cycles.
type sampleProject struct {
	APIVersion string `json:"apiVersion" jsonschema:"required,enum=infra.dev/v1alpha1"`
	Name       string `json:"name"       jsonschema:"required,pattern=^[a-z][a-z0-9-]*$,maxLength=63"`
}

func TestGenerateSchema(t *testing.T) {
	r := &jsonschema.Reflector{
		Anonymous:                 false,
		DoNotReference:            false,
		AllowAdditionalProperties: false,
	}
	schema := r.Reflect(&sampleProject{})
	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"apiVersion"`) {
		t.Errorf("schema missing apiVersion property:\n%s", out)
	}
	if !strings.Contains(string(out), `"required"`) {
		t.Errorf("schema missing required block:\n%s", out)
	}
	if !strings.Contains(string(out), `"enum"`) {
		t.Errorf("schema missing enum constraint:\n%s", out)
	}
}
