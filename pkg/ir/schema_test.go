package ir

import (
	"strings"
	"testing"
)

func TestEmbeddedSchemas(t *testing.T) {
	for _, name := range []string{"project.json", "component.json", "composition.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := EmbeddedSchema(name)
			if err != nil {
				t.Fatalf("EmbeddedSchema(%q): %v", name, err)
			}
			if !strings.Contains(string(data), `"$schema"`) {
				t.Errorf("schema %q missing $schema:\n%s", name, data)
			}
			if !strings.Contains(string(data), `"properties"`) {
				t.Errorf("schema %q missing properties:\n%s", name, data)
			}
		})
	}
}

func TestEmbeddedSchema_NotFound(t *testing.T) {
	if _, err := EmbeddedSchema("does-not-exist.json"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
