package ir

import (
	"embed"
	"fmt"
)

//go:embed schema/v1alpha1/*.json
var embeddedSchemaFS embed.FS

// EmbeddedSchema returns the bytes of the named schema file under
// schema/v1alpha1/, e.g. EmbeddedSchema("project.json").
//
// The schemas are generated from the Go types in this package by
// tools/schemagen and committed to git. Run `make generate` to refresh
// them after changing IR struct tags.
func EmbeddedSchema(name string) ([]byte, error) {
	data, err := embeddedSchemaFS.ReadFile("schema/v1alpha1/" + name)
	if err != nil {
		return nil, fmt.Errorf("embedded schema %q: %w", name, err)
	}
	return data, nil
}

// EmbeddedSchemaNames returns the filenames of all embedded schemas.
func EmbeddedSchemaNames() ([]string, error) {
	entries, err := embeddedSchemaFS.ReadDir("schema/v1alpha1")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
