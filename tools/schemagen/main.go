// Command schemagen produces JSON Schema documents from the pkg/ir Go types
// and writes them under pkg/ir/schema/v1alpha1/. The generated files are
// committed to git and embedded into the binary via go:embed so the runtime
// validator and IDE LSPs consume identical schemas.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invopop/jsonschema"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func main() {
	outDir := flag.String("out", "pkg/ir/schema/v1alpha1", "output directory")
	flag.Parse()

	if err := run(*outDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	cases := []struct {
		filename string
		sample   any
	}{
		{"project.json", &ir.Project{}},
		{"component.json", &ir.Component{}},
		{"composition.json", &ir.Composition{}},
	}

	r := &jsonschema.Reflector{
		Anonymous:                  false,
		DoNotReference:             false,
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: true,
	}

	for _, c := range cases {
		schema := r.Reflect(c.sample)
		buf, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", c.filename, err)
		}
		dest := filepath.Join(outDir, c.filename)
		if err := os.WriteFile(dest, buf, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		fmt.Printf("wrote %s\n", dest)
	}
	return nil
}
