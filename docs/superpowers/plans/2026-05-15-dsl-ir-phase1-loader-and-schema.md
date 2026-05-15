# DSL/IR Phase 1 — Loader and Schema Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land a working `nimbusfab validate` CLI that reads a project directory of YAML files into an unvalidated `*ir.Project`, runs the first three validator phases (YAML well-formedness, APIVersion check, JSON Schema validation), and prints a structured report. End-state: a user can `nimbusfab validate` a sample project and get useful diagnostics for malformed YAML, unknown apiVersions, and schema violations.

**Architecture:** Multi-pass pipeline. The loader (`internal/dsl/loader`) walks the project directory, parses YAML files with line/column provenance, and returns an unvalidated `*ir.Project`. The validator (`internal/dsl/validator`) runs phases sequentially over that tree and returns a `ValidationReport`. JSON Schema is generated from `pkg/ir` Go types at build time by `tools/schemagen`, committed to `pkg/ir/schema/v1alpha1/*.json`, and embedded into the binary via `//go:embed`. The CLI (`cmd/cli`) is Cobra-based; `nimbusfab validate` is the only command this phase wires up.

**Tech Stack:**
- Go 1.22 (existing)
- `gopkg.in/yaml.v3` — YAML parsing with `yaml.Node` for line/column metadata
- `github.com/invopop/jsonschema` — Go struct → JSON Schema generator
- `github.com/santhosh-tekuri/jsonschema/v5` — JSON Schema validator at runtime
- `github.com/spf13/cobra` — CLI framework
- Standard library `embed`, `testing`

**Conventions used throughout this plan:**
- All file paths are relative to the repo root `/home/kurt/git/nimbusfab/`.
- Run all `go` commands from the repo root.
- Each Task ends with a commit; commit messages follow `<area>: <imperative>` style (e.g., `loader: discover components in project dir`).
- Tests live alongside source (`foo.go` and `foo_test.go` in the same package) per Go conventions.
- Use `go test ./...` from the repo root to run all tests after each task.

---

## Task 1: Add Go module dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-generated)
- Create: `tools/tools.go` (Go module convention for tool dependencies)

- [ ] **Step 1: Add runtime dependencies**

Run:
```bash
go get gopkg.in/yaml.v3@v3.0.1
go get github.com/santhosh-tekuri/jsonschema/v5@v5.3.1
go get github.com/spf13/cobra@v1.8.0
```

Expected: `go.mod` now lists these dependencies; `go.sum` populated.

- [ ] **Step 2: Add tool dependency for schema generation**

Run:
```bash
go get github.com/invopop/jsonschema@v0.12.0
```

Then create `tools/tools.go`:

```go
//go:build tools

// Package tools tracks build-time tool dependencies via go.mod.
// `go install` these to build the schemagen tool.
package tools

import (
	_ "github.com/invopop/jsonschema"
)
```

- [ ] **Step 3: Verify and tidy**

Run:
```bash
go mod tidy
go build ./...
```

Expected: clean build, no errors. (`./...` will include the stub packages from prior commits; they should still compile.)

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum tools/tools.go
git commit -m "deps: add yaml.v3, jsonschema, cobra, and invopop/jsonschema"
```

---

## Task 2: Expand IR types with validation + provenance metadata

**Files:**
- Modify: `pkg/ir/types.go`
- Create: `pkg/ir/source.go`
- Create: `pkg/ir/source_test.go`
- Create: `pkg/ir/validation.go`
- Create: `pkg/ir/validation_test.go`

- [ ] **Step 1: Write failing test for `Source` provenance type**

Create `pkg/ir/source_test.go`:

```go
package ir

import "testing"

func TestSource_String(t *testing.T) {
	cases := []struct {
		name string
		src  Source
		want string
	}{
		{"with column", Source{File: "components/orders-db.yaml", Line: 12, Column: 5}, "components/orders-db.yaml:12:5"},
		{"no column", Source{File: "project.yaml", Line: 1}, "project.yaml:1"},
		{"no line", Source{File: "stack.yaml"}, "stack.yaml"},
		{"empty", Source{}, "<unknown>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.src.String(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./pkg/ir/ -run TestSource_String -v
```

Expected: FAIL with `undefined: Source`.

- [ ] **Step 3: Implement `Source`**

Create `pkg/ir/source.go`:

```go
package ir

import "fmt"

// Source identifies the location in a YAML file that produced an IR node.
// The loader attaches Sources to nodes as it parses; the validator quotes
// them in diagnostics.
type Source struct {
	File   string
	Line   int
	Column int
}

// String renders the source as "file:line:col", "file:line", "file", or
// "<unknown>" depending on which fields are set.
func (s Source) String() string {
	switch {
	case s.File == "":
		return "<unknown>"
	case s.Line == 0:
		return s.File
	case s.Column == 0:
		return fmt.Sprintf("%s:%d", s.File, s.Line)
	default:
		return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Column)
	}
}
```

- [ ] **Step 4: Run test, verify pass**

Run:
```bash
go test ./pkg/ir/ -run TestSource_String -v
```

Expected: PASS.

- [ ] **Step 5: Write failing tests for `ValidationReport` and `Issue`**

Create `pkg/ir/validation_test.go`:

```go
package ir

import "testing"

func TestValidationReport_OK(t *testing.T) {
	cases := []struct {
		name   string
		issues []Issue
		want   bool
	}{
		{"empty", nil, true},
		{"only warnings", []Issue{{Severity: SeverityWarning}}, true},
		{"only info", []Issue{{Severity: SeverityInfo}}, true},
		{"one error", []Issue{{Severity: SeverityError}}, false},
		{"mixed", []Issue{{Severity: SeverityWarning}, {Severity: SeverityError}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ValidationReport{Issues: c.issues}
			if got := r.OK(); got != c.want {
				t.Errorf("OK() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIssue_String(t *testing.T) {
	i := Issue{
		Severity: SeverityError,
		Code:     "ErrSchemaRequiredField",
		Message:  "required field missing",
		Path:     "components[0].name",
		Source:   Source{File: "components/orders-db.yaml", Line: 3, Column: 1},
	}
	got := i.String()
	want := "components/orders-db.yaml:3:1: error: ErrSchemaRequiredField: required field missing (at components[0].name)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
```

- [ ] **Step 6: Verify failure**

Run:
```bash
go test ./pkg/ir/ -run TestValidationReport_OK -v
go test ./pkg/ir/ -run TestIssue_String -v
```

Expected: FAIL with `undefined: ValidationReport`, `undefined: Issue`.

- [ ] **Step 7: Implement validation types**

Create `pkg/ir/validation.go`:

```go
package ir

import "fmt"

// Severity classifies a diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Issue is a single diagnostic produced by the validator.
type Issue struct {
	Severity Severity
	Code     string // stable code, e.g. "ErrSchemaRequiredField"
	Message  string // human-readable
	Path     string // dotted IR path, e.g. "components[0].targets[1].region"
	Source   Source // YAML provenance when available
	Hint     string // remediation suggestion
}

// String renders the issue in the canonical "<source>: <severity>: <code>: <message> (at <path>)" form.
func (i Issue) String() string {
	pathPart := ""
	if i.Path != "" {
		pathPart = fmt.Sprintf(" (at %s)", i.Path)
	}
	return fmt.Sprintf("%s: %s: %s: %s%s", i.Source.String(), i.Severity, i.Code, i.Message, pathPart)
}

// ValidationReport is what the validator returns. OK() reports whether any
// blocking errors were found; warnings and info do not block.
type ValidationReport struct {
	Issues []Issue
}

// OK returns true iff no Issue with severity Error is present.
func (r ValidationReport) OK() bool {
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			return false
		}
	}
	return true
}
```

- [ ] **Step 8: Run tests, verify pass**

Run:
```bash
go test ./pkg/ir/ -v
```

Expected: PASS for `TestSource_String`, `TestValidationReport_OK`, `TestIssue_String`.

- [ ] **Step 9: Add JSON Schema struct tags to existing IR types**

Modify `pkg/ir/types.go`. For each exported field, add a `jsonschema:` tag where appropriate to control the generated schema (descriptions, formats, required-ness through the schema generator). Example modifications:

Replace the `Project` struct definition with:

```go
type Project struct {
	APIVersion string             `json:"apiVersion" yaml:"apiVersion" jsonschema:"required,enum=infra.dev/v1alpha1"`
	Name       string             `json:"name"       yaml:"name"       jsonschema:"required,pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$,maxLength=63"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Stacks     map[string]Stack   `json:"stacks"     yaml:"stacks"     jsonschema:"required,minProperties=1"`
	Components []Component        `json:"components,omitempty" yaml:"components,omitempty"`
	Comps      []Composition      `json:"compositions,omitempty" yaml:"compositions,omitempty"`
}
```

Apply the same pattern to `Component`, `DeploymentTarget`, `Composition`, `Stack`, `StateBackend`. Required: `apiVersion`, `name`, `type`, `targets` (Component), `cloud`, `region`, `credentialRef` (DeploymentTarget). Name fields use the DNS-1123 subdomain regex `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`.

- [ ] **Step 10: Build to verify struct tag changes compile**

Run:
```bash
go build ./pkg/ir/
go test ./pkg/ir/
```

Expected: clean build, all tests pass.

- [ ] **Step 11: Commit**

```bash
git add pkg/ir/source.go pkg/ir/source_test.go pkg/ir/validation.go pkg/ir/validation_test.go pkg/ir/types.go
git commit -m "ir: add Source provenance and ValidationReport types"
```

---

## Task 3: Build the schemagen tool

**Files:**
- Create: `tools/schemagen/main.go`
- Create: `tools/schemagen/main_test.go`
- Modify: `Makefile`

- [ ] **Step 1: Write failing test for schema generation against a tiny IR fragment**

Create `tools/schemagen/main_test.go`:

```go
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
		Anonymous:       false,
		DoNotReference:  false,
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
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./tools/schemagen/ -v
```

Expected: FAIL — `package main` doesn't exist yet (no `main.go`).

- [ ] **Step 3: Implement schemagen**

Create `tools/schemagen/main.go`:

```go
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
		Anonymous:                 false,
		DoNotReference:            false,
		AllowAdditionalProperties: false,
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
```

- [ ] **Step 4: Run schemagen locally**

Run:
```bash
go run ./tools/schemagen/
```

Expected: prints `wrote pkg/ir/schema/v1alpha1/project.json`, `wrote pkg/ir/schema/v1alpha1/component.json`, `wrote pkg/ir/schema/v1alpha1/composition.json`. New files exist.

- [ ] **Step 5: Sanity-check the generated schema**

Run:
```bash
cat pkg/ir/schema/v1alpha1/project.json | head -40
```

Expected: a JSON document with `"$schema"`, `"$id"`, `"required"`, `"properties"`, including `apiVersion` constrained to the enum `["infra.dev/v1alpha1"]`.

- [ ] **Step 6: Add the schemagen test to verify reflector configuration**

Run:
```bash
go test ./tools/schemagen/ -v
```

Expected: `TestGenerateSchema` PASSES.

- [ ] **Step 7: Add Makefile target**

Modify `Makefile`. Insert after the existing `tidy` target:

```makefile
schemagen:
	$(GO) run ./tools/schemagen/

generate: schemagen
```

And add `schemagen` and `generate` to the `.PHONY` line at the top:

```makefile
.PHONY: build cli server test lint fmt tidy clean schemagen generate
```

- [ ] **Step 8: Commit**

```bash
git add tools/schemagen/main.go tools/schemagen/main_test.go pkg/ir/schema/v1alpha1/*.json Makefile
git commit -m "schemagen: generate JSON Schema from pkg/ir types"
```

---

## Task 4: Embed generated schemas via `go:embed`

**Files:**
- Modify: `pkg/ir/schema.go`
- Create: `pkg/ir/schema_test.go`

- [ ] **Step 1: Write failing test that loads embedded schemas**

Create `pkg/ir/schema_test.go`:

```go
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
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./pkg/ir/ -run TestEmbeddedSchema -v
```

Expected: FAIL with `undefined: EmbeddedSchema`.

- [ ] **Step 3: Replace `pkg/ir/schema.go` with embedded loader**

Replace contents of `pkg/ir/schema.go`:

```go
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
```

- [ ] **Step 4: Run tests, verify pass**

Run:
```bash
go test ./pkg/ir/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/ir/schema.go pkg/ir/schema_test.go
git commit -m "ir: embed generated schemas via go:embed"
```

---

## Task 5: YAML node parsing with provenance

**Files:**
- Create: `internal/dsl/yamlnode/yamlnode.go`
- Create: `internal/dsl/yamlnode/yamlnode_test.go`

- [ ] **Step 1: Write failing test for `Decode` capturing line/column info**

Create `internal/dsl/yamlnode/yamlnode_test.go`:

```go
package yamlnode

import (
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestDecode_CapturesProvenance(t *testing.T) {
	src := strings.TrimSpace(`
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: {}
`)
	doc, err := Decode("project.yaml", []byte(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if doc.APIVersion != "infra.dev/v1alpha1" {
		t.Errorf("APIVersion = %q", doc.APIVersion)
	}
	if doc.Name != "orders" {
		t.Errorf("Name = %q", doc.Name)
	}
	src1 := doc.SourceFor("apiVersion")
	if src1.File != "project.yaml" || src1.Line != 1 {
		t.Errorf("apiVersion source = %+v, want project.yaml:1", src1)
	}
	src2 := doc.SourceFor("name")
	if src2.Line != 2 {
		t.Errorf("name source = %+v, want line 2", src2)
	}
}

func TestDecode_Malformed(t *testing.T) {
	_, err := Decode("bad.yaml", []byte("apiVersion: : :\n  bad"))
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
	yerr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if yerr.Source.File != "bad.yaml" {
		t.Errorf("Source.File = %q, want bad.yaml", yerr.Source.File)
	}
}
```

(The `ir` import is used by `doc.SourceFor` returning `ir.Source`; no keep-alive needed.)

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./internal/dsl/yamlnode/ -v
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement `yamlnode`**

Create `internal/dsl/yamlnode/yamlnode.go`:

```go
// Package yamlnode wraps gopkg.in/yaml.v3 to give every decoded value a
// Source pointing back at the YAML file and line it came from. The loader
// uses this to attribute validator diagnostics; consumers see the same
// IR types but can ask "where did this field come from?".
package yamlnode

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Document is a decoded YAML document plus a top-level provenance map.
// For the v1 implementation we only track provenance for the top-level
// fields of the document; nested provenance is added in Phase 2.
type Document struct {
	APIVersion string
	Name       string
	Raw        *yaml.Node // root MappingNode, accessible for deeper decoding

	source string                // file path
	prov   map[string]ir.Source  // top-level field -> source
}

// SourceFor returns the Source attached to the named top-level field.
// If the field was not present, an empty Source is returned.
func (d *Document) SourceFor(field string) ir.Source {
	return d.prov[field]
}

// Error wraps a yaml.v3 parse error with a Source so callers can attribute
// it to a file:line.
type Error struct {
	Source ir.Source
	Err    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %v", e.Source, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Decode parses a YAML document and returns a Document. The filename is used
// only for provenance; the bytes are the actual YAML body.
func Decode(filename string, body []byte) (*Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		// yaml.v3 errors include line numbers in their text but not as fields;
		// best-effort extraction:
		return nil, &Error{
			Source: ir.Source{File: filename},
			Err:    err,
		}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, &Error{
			Source: ir.Source{File: filename},
			Err:    errors.New("empty document"),
		}
	}
	body0 := root.Content[0]
	if body0.Kind != yaml.MappingNode {
		return nil, &Error{
			Source: ir.Source{File: filename, Line: body0.Line, Column: body0.Column},
			Err:    fmt.Errorf("expected mapping at document root, got %v", kindString(body0.Kind)),
		}
	}

	doc := &Document{
		Raw:    body0,
		source: filename,
		prov:   map[string]ir.Source{},
	}

	for i := 0; i < len(body0.Content); i += 2 {
		keyNode := body0.Content[i]
		valNode := body0.Content[i+1]
		doc.prov[keyNode.Value] = ir.Source{
			File:   filename,
			Line:   keyNode.Line,
			Column: keyNode.Column,
		}
		switch keyNode.Value {
		case "apiVersion":
			doc.APIVersion = valNode.Value
		case "name":
			doc.Name = valNode.Value
		}
	}
	return doc, nil
}

func kindString(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("kind%d", k)
	}
}
```

- [ ] **Step 4: Run tests, verify pass**

Run:
```bash
go test ./internal/dsl/yamlnode/ -v
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dsl/yamlnode/yamlnode.go internal/dsl/yamlnode/yamlnode_test.go
git commit -m "yamlnode: parse YAML with file:line provenance"
```

---

## Task 6: Single-file project loader

**Files:**
- Modify: `internal/dsl/loader/loader.go`
- Create: `internal/dsl/loader/loader_test.go`
- Create: `internal/dsl/loader/testdata/single-file/project.yaml`

- [ ] **Step 1: Write failing test for `Load` on a single-file project**

Create `internal/dsl/loader/testdata/single-file/project.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: orders
description: small single-file test project
stacks:
  dev:
    stateBackend:
      kind: local
  prod:
    stateBackend:
      kind: s3
      config:
        bucket: nimbusfab-state
        region: us-east-1
components:
  - name: web-network
    type: network
    spec:
      cidrBlock: 10.0.0.0/16
    targets:
      - cloud: aws
        region: us-east-1
        credentialRef: aws-dev
```

Create `internal/dsl/loader/loader_test.go`:

```go
package loader

import (
	"context"
	"testing"
)

func TestLoad_SingleFile(t *testing.T) {
	ctx := context.Background()
	proj, err := New().Load(ctx, "testdata/single-file")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if proj.APIVersion != "infra.dev/v1alpha1" {
		t.Errorf("APIVersion = %q", proj.APIVersion)
	}
	if proj.Name != "orders" {
		t.Errorf("Name = %q", proj.Name)
	}
	if len(proj.Stacks) != 2 {
		t.Errorf("Stacks count = %d, want 2", len(proj.Stacks))
	}
	if len(proj.Components) != 1 {
		t.Fatalf("Components count = %d, want 1", len(proj.Components))
	}
	if proj.Components[0].Name != "web-network" {
		t.Errorf("Component[0].Name = %q", proj.Components[0].Name)
	}
}

func TestLoad_MissingProjectYAML(t *testing.T) {
	_, err := New().Load(context.Background(), "testdata/does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing project.yaml, got nil")
	}
}
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./internal/dsl/loader/ -v
```

Expected: FAIL — loader stub returns "not implemented yet".

- [ ] **Step 3: Implement single-file load**

Replace contents of `internal/dsl/loader/loader.go`:

```go
// Package loader walks a project directory and returns an unvalidated
// *ir.Project. The Phase-1 implementation supports the canonical layout
// (project.yaml + components/ + compositions/) and the single-file
// fallback. Validation is the next subsystem; this package is only
// concerned with file discovery and YAML parsing.
package loader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/klehmer/nimbusfab/internal/dsl/yamlnode"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Loader is the public entry point. Construct with New().
type Loader interface {
	Load(ctx context.Context, root string) (*ir.Project, error)
}

// New returns the default Loader implementation.
func New() Loader { return &fsLoader{} }

type fsLoader struct{}

func (l *fsLoader) Load(ctx context.Context, root string) (*ir.Project, error) {
	_ = ctx
	projectPath := filepath.Join(root, "project.yaml")
	body, err := os.ReadFile(projectPath)
	if err != nil {
		return nil, fmt.Errorf("read project.yaml: %w", err)
	}

	doc, err := yamlnode.Decode(projectPath, body)
	if err != nil {
		return nil, err
	}

	proj := &ir.Project{}
	if err := doc.Raw.Decode(proj); err != nil {
		return nil, &yamlnode.Error{
			Source: ir.Source{File: projectPath, Line: doc.Raw.Line},
			Err:    fmt.Errorf("decode project: %w", err),
		}
	}

	// (Multi-file discovery, includes, stack values arrive in later tasks.)
	return proj, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run:
```bash
go test ./internal/dsl/loader/ -v
```

Expected: `TestLoad_SingleFile` PASS, `TestLoad_MissingProjectYAML` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dsl/loader/loader.go internal/dsl/loader/loader_test.go internal/dsl/loader/testdata/single-file/project.yaml
git commit -m "loader: read single-file project.yaml into ir.Project"
```

---

## Task 7: Multi-file directory discovery

**Files:**
- Modify: `internal/dsl/loader/loader.go`
- Modify: `internal/dsl/loader/loader_test.go`
- Create: `internal/dsl/loader/testdata/multi-file/project.yaml`
- Create: `internal/dsl/loader/testdata/multi-file/components/web-network.yaml`
- Create: `internal/dsl/loader/testdata/multi-file/components/orders-db.yaml`
- Create: `internal/dsl/loader/testdata/multi-file/compositions/tuned-postgres.yaml`

- [ ] **Step 1: Create fixtures**

Create `internal/dsl/loader/testdata/multi-file/project.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: orders-multi
stacks:
  dev:
    stateBackend:
      kind: local
```

Create `internal/dsl/loader/testdata/multi-file/components/web-network.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: web-network
type: network
spec:
  cidrBlock: 10.0.0.0/16
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
```

Create `internal/dsl/loader/testdata/multi-file/components/orders-db.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
name: orders-db
type: database
spec:
  engine: postgres
  size: small
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
```

Create `internal/dsl/loader/testdata/multi-file/compositions/tuned-postgres.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
kind: TunedPostgres
schema:
  type: object
  required: [size]
  properties:
    size:
      type: string
      enum: [small, medium, large]
template:
  resources:
    - name: ${input.name}-db
      type: database
      spec:
        engine: postgres
        size: ${input.size}
```

- [ ] **Step 2: Write failing test for multi-file discovery**

Append to `internal/dsl/loader/loader_test.go`:

```go
func TestLoad_MultiFile(t *testing.T) {
	proj, err := New().Load(context.Background(), "testdata/multi-file")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if proj.Name != "orders-multi" {
		t.Errorf("Name = %q", proj.Name)
	}
	if len(proj.Components) != 2 {
		t.Fatalf("Components = %d, want 2", len(proj.Components))
	}
	names := map[string]bool{}
	for _, c := range proj.Components {
		names[c.Name] = true
	}
	if !names["web-network"] || !names["orders-db"] {
		t.Errorf("missing expected components: %v", names)
	}
	if len(proj.Comps) != 1 {
		t.Fatalf("Compositions = %d, want 1", len(proj.Comps))
	}
	if proj.Comps[0].Kind != "TunedPostgres" {
		t.Errorf("Composition kind = %q", proj.Comps[0].Kind)
	}
}

func TestLoad_DuplicateComponent(t *testing.T) {
	// Same component name appearing in both project.yaml inline and components/*.yaml.
	dir := t.TempDir()
	mustWrite(t, dir+"/project.yaml", []byte(`
apiVersion: infra.dev/v1alpha1
name: dup
stacks:
  dev: {}
components:
  - name: same
    type: network
    spec: { cidrBlock: 10.0.0.0/16 }
    targets:
      - cloud: aws
        region: us-east-1
        credentialRef: aws-dev
`))
	mustMkdir(t, dir+"/components")
	mustWrite(t, dir+"/components/same.yaml", []byte(`
apiVersion: infra.dev/v1alpha1
name: same
type: network
spec: { cidrBlock: 10.1.0.0/16 }
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
`))
	_, err := New().Load(context.Background(), dir)
	if err == nil {
		t.Fatal("expected duplicate-component error, got nil")
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
```

Add `"os"` to the imports of `loader_test.go`.

- [ ] **Step 3: Verify failure**

Run:
```bash
go test ./internal/dsl/loader/ -v
```

Expected: FAIL — multi-file discovery not implemented.

- [ ] **Step 4: Implement multi-file discovery**

Replace the body of `fsLoader.Load` in `internal/dsl/loader/loader.go`:

```go
func (l *fsLoader) Load(ctx context.Context, root string) (*ir.Project, error) {
	projectPath := filepath.Join(root, "project.yaml")
	body, err := os.ReadFile(projectPath)
	if err != nil {
		return nil, fmt.Errorf("read project.yaml: %w", err)
	}
	doc, err := yamlnode.Decode(projectPath, body)
	if err != nil {
		return nil, err
	}
	proj := &ir.Project{}
	if err := doc.Raw.Decode(proj); err != nil {
		return nil, &yamlnode.Error{
			Source: ir.Source{File: projectPath, Line: doc.Raw.Line},
			Err:    fmt.Errorf("decode project: %w", err),
		}
	}

	if err := l.loadComponentsDir(root, proj); err != nil {
		return nil, err
	}
	if err := l.loadCompositionsDir(root, proj); err != nil {
		return nil, err
	}
	if err := assertUniqueComponents(proj); err != nil {
		return nil, err
	}
	if err := assertUniqueCompositions(proj); err != nil {
		return nil, err
	}

	_ = ctx
	return proj, nil
}

func (l *fsLoader) loadComponentsDir(root string, proj *ir.Project) error {
	dir := filepath.Join(root, "components")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read components/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		// Components files contain one Component each (multi-doc support in a later task).
		var comp ir.Component
		if err := yaml.Unmarshal(body, &comp); err != nil {
			return &yamlnode.Error{Source: ir.Source{File: path}, Err: err}
		}
		proj.Components = append(proj.Components, comp)
	}
	return nil
}

func (l *fsLoader) loadCompositionsDir(root string, proj *ir.Project) error {
	dir := filepath.Join(root, "compositions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read compositions/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var comp ir.Composition
		if err := yaml.Unmarshal(body, &comp); err != nil {
			return &yamlnode.Error{Source: ir.Source{File: path}, Err: err}
		}
		proj.Comps = append(proj.Comps, comp)
	}
	return nil
}

func assertUniqueComponents(proj *ir.Project) error {
	seen := map[string]bool{}
	for _, c := range proj.Components {
		if seen[c.Name] {
			return fmt.Errorf("duplicate component %q (declared in both inline and components/)", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

func assertUniqueCompositions(proj *ir.Project) error {
	seen := map[string]bool{}
	for _, c := range proj.Comps {
		if seen[c.Kind] {
			return fmt.Errorf("duplicate composition kind %q", c.Kind)
		}
		seen[c.Kind] = true
	}
	return nil
}
```

- [ ] **Step 5: Run tests, verify pass**

Run:
```bash
go test ./internal/dsl/loader/ -v
```

Expected: all four loader tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/dsl/loader/ internal/dsl/loader/testdata/
git commit -m "loader: discover components and compositions from project subdirs"
```

---

## Task 8: Stack values file loader

**Files:**
- Modify: `internal/dsl/loader/loader.go`
- Modify: `internal/dsl/loader/loader_test.go`
- Create: `internal/dsl/loader/stack.go`
- Create: `internal/dsl/loader/stack_test.go`
- Create: `internal/dsl/loader/testdata/multi-file/stacks/dev/values.yaml`

- [ ] **Step 1: Write failing test for stack values loading**

Create `internal/dsl/loader/testdata/multi-file/stacks/dev/values.yaml`:

```yaml
apiVersion: infra.dev/v1alpha1
vars:
  aws_region: us-east-1
  db_size: small
  pi_enabled: false
disabled:
  components:
    - analytics-warehouse
  targets:
    - component: web-network
      cloud: azure
```

Create `internal/dsl/loader/stack_test.go`:

```go
package loader

import (
	"context"
	"testing"
)

func TestLoadStackValues_Present(t *testing.T) {
	values, err := LoadStackValues(context.Background(), "testdata/multi-file", "dev")
	if err != nil {
		t.Fatalf("LoadStackValues: %v", err)
	}
	if values.Vars["aws_region"] != "us-east-1" {
		t.Errorf("vars[aws_region] = %v", values.Vars["aws_region"])
	}
	if values.Vars["db_size"] != "small" {
		t.Errorf("vars[db_size] = %v", values.Vars["db_size"])
	}
	if v, ok := values.Vars["pi_enabled"].(bool); !ok || v {
		t.Errorf("vars[pi_enabled] should be the bool false, got %v (%T)", values.Vars["pi_enabled"], values.Vars["pi_enabled"])
	}
	if len(values.DisabledComponents) != 1 || values.DisabledComponents[0] != "analytics-warehouse" {
		t.Errorf("DisabledComponents = %v", values.DisabledComponents)
	}
	if len(values.DisabledTargets) != 1 {
		t.Fatalf("DisabledTargets = %v, want 1", values.DisabledTargets)
	}
	if values.DisabledTargets[0].Component != "web-network" || values.DisabledTargets[0].Cloud != "azure" {
		t.Errorf("DisabledTargets[0] = %+v", values.DisabledTargets[0])
	}
}

func TestLoadStackValues_Absent(t *testing.T) {
	values, err := LoadStackValues(context.Background(), "testdata/multi-file", "no-such-stack")
	if err != nil {
		t.Fatalf("LoadStackValues: %v", err)
	}
	if values == nil {
		t.Fatal("expected empty StackValues, got nil")
	}
	if len(values.Vars) != 0 {
		t.Errorf("Vars should be empty when stack file missing, got %v", values.Vars)
	}
}
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./internal/dsl/loader/ -run TestLoadStackValues -v
```

Expected: FAIL — `LoadStackValues` undefined.

- [ ] **Step 3: Implement `StackValues` and `LoadStackValues`**

Create `internal/dsl/loader/stack.go`:

```go
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
	APIVersion         string                 `yaml:"apiVersion"`
	Vars               map[string]any         `yaml:"vars,omitempty"`
	DisabledComponents []string               `yaml:"-"`
	DisabledTargets    []DisabledTarget       `yaml:"-"`
	rawDisabled        map[string]any         `yaml:"disabled,omitempty"`
}

// DisabledTarget identifies a specific target to remove from this stack.
type DisabledTarget struct {
	Component string `yaml:"component"`
	Cloud     string `yaml:"cloud,omitempty"`
	Region    string `yaml:"region,omitempty"`
}

// LoadStackValues reads stacks/<stack>/values.yaml if present. If the file
// does not exist, returns an empty StackValues (not an error) so the
// validator's no-vars path is uniform.
func LoadStackValues(ctx context.Context, root, stack string) (*StackValues, error) {
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
	_ = ctx
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
```

- [ ] **Step 4: Adjust StackValues to capture raw disabled block via UnmarshalYAML**

Replace the StackValues type and add an UnmarshalYAML method so the `disabled:` block is captured into `rawDisabled`. Replace the type and add to `stack.go`:

```go
type StackValues struct {
	APIVersion         string           `yaml:"apiVersion"`
	Vars               map[string]any   `yaml:"vars,omitempty"`
	DisabledComponents []string         `yaml:"-"`
	DisabledTargets    []DisabledTarget `yaml:"-"`
	rawDisabled        map[string]any
}

// UnmarshalYAML implements yaml.Unmarshaler so the `disabled:` block lands
// in rawDisabled for later materialization.
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
```

(Remove the original StackValues struct now that this replacement covers it; keep `DisabledTarget`, `LoadStackValues`, and `materializeDisabled`.)

- [ ] **Step 5: Run tests, verify pass**

Run:
```bash
go test ./internal/dsl/loader/ -v
```

Expected: `TestLoadStackValues_Present` and `TestLoadStackValues_Absent` PASS; previous loader tests still pass.

- [ ] **Step 6: Commit**

```bash
git add internal/dsl/loader/stack.go internal/dsl/loader/stack_test.go internal/dsl/loader/testdata/multi-file/stacks/
git commit -m "loader: parse stacks/<name>/values.yaml with disabled toggles"
```

---

## Task 9: Validator scaffold and Phase 1 (YAML well-formedness)

**Files:**
- Modify: `internal/dsl/validator/validator.go`
- Create: `internal/dsl/validator/phase1_yaml.go`
- Create: `internal/dsl/validator/validator_test.go`

- [ ] **Step 1: Write failing test for the validator's Phase 1 handling of a malformed YAML loader error**

Create `internal/dsl/validator/validator_test.go`:

```go
package validator

import (
	"context"
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/internal/dsl/yamlnode"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestValidator_LiftsLoaderErrorAsIssue(t *testing.T) {
	v := New()
	loaderErr := &yamlnode.Error{
		Source: ir.Source{File: "project.yaml", Line: 3, Column: 5},
		Err:    errors.New("mapping values are not allowed here"),
	}
	report, err := v.ValidateLoaderError(context.Background(), loaderErr)
	if err != nil {
		t.Fatalf("ValidateLoaderError: %v", err)
	}
	if report.OK() {
		t.Fatal("report.OK() = true, want false")
	}
	if len(report.Issues) != 1 {
		t.Fatalf("Issues = %d, want 1", len(report.Issues))
	}
	got := report.Issues[0]
	if got.Severity != ir.SeverityError {
		t.Errorf("Severity = %v", got.Severity)
	}
	if got.Code != "ErrYAMLMalformed" {
		t.Errorf("Code = %q", got.Code)
	}
	if got.Source.File != "project.yaml" || got.Source.Line != 3 {
		t.Errorf("Source = %+v", got.Source)
	}
}
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./internal/dsl/validator/ -v
```

Expected: FAIL — `ValidateLoaderError` undefined.

- [ ] **Step 3: Implement validator scaffold**

Replace contents of `internal/dsl/validator/validator.go`:

```go
// Package validator runs the IR validation phases. Phase 1 turns loader
// errors into Issues; Phases 2 and 3 land in subsequent tasks; later
// phases (interpolation, composition, semantics) are deferred to the
// Phase 2 implementation plan.
package validator

import (
	"context"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Validator runs the configured phases and returns a ValidationReport.
type Validator interface {
	// ValidateLoaderError converts a single loader error into a Report.
	// Phase 1 of the validation pipeline lives here because loader failures
	// abort before later phases can see the IR.
	ValidateLoaderError(ctx context.Context, err error) (*ir.ValidationReport, error)

	// Validate runs the IR through Phases 2+. Phase 1 will already have
	// been satisfied if a non-nil Project reached this point.
	Validate(ctx context.Context, proj *ir.Project) (*ir.ValidationReport, error)
}

// New returns the default Validator.
func New() Validator { return &fsValidator{} }

type fsValidator struct{}
```

Create `internal/dsl/validator/phase1_yaml.go`:

```go
package validator

import (
	"context"
	"errors"

	"github.com/klehmer/nimbusfab/internal/dsl/yamlnode"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func (v *fsValidator) ValidateLoaderError(ctx context.Context, err error) (*ir.ValidationReport, error) {
	_ = ctx
	if err == nil {
		return &ir.ValidationReport{}, nil
	}
	report := &ir.ValidationReport{}

	var ye *yamlnode.Error
	if errors.As(err, &ye) {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrYAMLMalformed",
			Message:  ye.Err.Error(),
			Source:   ye.Source,
		})
		return report, nil
	}

	// Anything else is captured as a generic loader error.
	report.Issues = append(report.Issues, ir.Issue{
		Severity: ir.SeverityError,
		Code:     "ErrLoader",
		Message:  err.Error(),
	})
	return report, nil
}
```

- [ ] **Step 4: Stub `Validate` (Phase 2 fills it in)**

Append to `internal/dsl/validator/validator.go`:

```go
// Validate runs the configured phases. Phase 1 plan covers phases 2 and 3;
// phases 4-9 land in the Phase 2 plan and currently return immediately.
func (v *fsValidator) Validate(ctx context.Context, proj *ir.Project) (*ir.ValidationReport, error) {
	report := &ir.ValidationReport{}
	if proj == nil {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrLoader",
			Message:  "nil project",
		})
		return report, nil
	}
	if err := phase2APIVersion(proj, report); err != nil {
		return nil, err
	}
	if err := phase3Schema(proj, report); err != nil {
		return nil, err
	}
	_ = ctx
	return report, nil
}

func phase2APIVersion(proj *ir.Project, report *ir.ValidationReport) error { return nil }
func phase3Schema(proj *ir.Project, report *ir.ValidationReport) error     { return nil }
```

(`phase2APIVersion` and `phase3Schema` are stubs filled in by the next two tasks.)

- [ ] **Step 5: Run tests, verify pass**

Run:
```bash
go test ./internal/dsl/validator/ -v
```

Expected: `TestValidator_LiftsLoaderErrorAsIssue` PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/dsl/validator/validator.go internal/dsl/validator/phase1_yaml.go internal/dsl/validator/validator_test.go
git commit -m "validator: scaffold + Phase 1 lifts loader errors to issues"
```

---

## Task 10: Validator Phase 2 — APIVersion check

**Files:**
- Create: `internal/dsl/validator/phase2_apiversion.go`
- Modify: `internal/dsl/validator/validator_test.go`

- [ ] **Step 1: Add failing tests**

Append to `internal/dsl/validator/validator_test.go`:

```go
func TestValidate_APIVersionMissing(t *testing.T) {
	proj := &ir.Project{Name: "x", Stacks: map[string]ir.Stack{"dev": {}}}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrMissingAPIVersion") {
		t.Errorf("missing ErrMissingAPIVersion: %+v", report.Issues)
	}
}

func TestValidate_APIVersionUnknown(t *testing.T) {
	proj := &ir.Project{APIVersion: "infra.dev/v999", Name: "x", Stacks: map[string]ir.Stack{"dev": {}}}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrUnknownAPIVersion") {
		t.Errorf("missing ErrUnknownAPIVersion: %+v", report.Issues)
	}
}

func TestValidate_APIVersionOK(t *testing.T) {
	proj := &ir.Project{
		APIVersion: "infra.dev/v1alpha1",
		Name:       "x",
		Stacks:     map[string]ir.Stack{"dev": {}},
	}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if hasCode(report, "ErrMissingAPIVersion") || hasCode(report, "ErrUnknownAPIVersion") {
		t.Errorf("unexpected APIVersion issue: %+v", report.Issues)
	}
}

func hasCode(report *ir.ValidationReport, code string) bool {
	for _, i := range report.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./internal/dsl/validator/ -v
```

Expected: FAIL — Phase 2 is a stub.

- [ ] **Step 3: Implement Phase 2**

Create `internal/dsl/validator/phase2_apiversion.go`:

```go
package validator

import (
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// supportedAPIVersions lists the IR APIVersions the validator accepts. New
// versions get added here as they're introduced; old versions stay in the
// list for at least one minor release after deprecation.
var supportedAPIVersions = map[string]bool{
	ir.APIVersionV1Alpha1: true,
}

func phase2APIVersionImpl(proj *ir.Project, report *ir.ValidationReport) {
	if proj.APIVersion == "" {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrMissingAPIVersion",
			Message:  "project.yaml does not declare apiVersion",
			Path:     "apiVersion",
			Hint:     `add: apiVersion: ` + ir.APIVersionV1Alpha1,
		})
		return
	}
	if !supportedAPIVersions[proj.APIVersion] {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrUnknownAPIVersion",
			Message:  "apiVersion " + proj.APIVersion + " is not supported by this build",
			Path:     "apiVersion",
			Hint:     "supported: " + ir.APIVersionV1Alpha1,
		})
	}
}
```

Replace the stub `phase2APIVersion` in `validator.go` to call this:

```go
func phase2APIVersion(proj *ir.Project, report *ir.ValidationReport) error {
	phase2APIVersionImpl(proj, report)
	return nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run:
```bash
go test ./internal/dsl/validator/ -v
```

Expected: all four validator tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dsl/validator/phase2_apiversion.go internal/dsl/validator/validator.go internal/dsl/validator/validator_test.go
git commit -m "validator: Phase 2 checks apiVersion presence and support"
```

---

## Task 11: Validator Phase 3 — JSON Schema validation

**Files:**
- Create: `internal/dsl/validator/phase3_schema.go`
- Create: `internal/dsl/validator/phase3_schema_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/dsl/validator/phase3_schema_test.go`:

```go
package validator

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestValidate_Schema_MissingRequiredName(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Stacks:     map[string]ir.Stack{"dev": {}},
		// Name intentionally absent.
	}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrSchemaRequiredField") {
		t.Errorf("missing ErrSchemaRequiredField: %+v", report.Issues)
	}
}

func TestValidate_Schema_InvalidName(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "Has Spaces",
		Stacks:     map[string]ir.Stack{"dev": {}},
	}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrSchemaPattern") {
		t.Errorf("missing ErrSchemaPattern: %+v", report.Issues)
	}
}

func TestValidate_Schema_OK(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "orders",
		Stacks:     map[string]ir.Stack{"dev": {}},
	}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if hasCode(report, "ErrSchemaRequiredField") || hasCode(report, "ErrSchemaPattern") {
		t.Errorf("unexpected schema issue: %+v", report.Issues)
	}
}
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./internal/dsl/validator/ -v
```

Expected: FAIL — Phase 3 is still a stub.

- [ ] **Step 3: Implement Phase 3**

Create `internal/dsl/validator/phase3_schema.go`:

```go
package validator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// phase3SchemaImpl validates the *ir.Project against the embedded JSON
// Schema and translates schema errors into ir.Issues.
func phase3SchemaImpl(proj *ir.Project, report *ir.ValidationReport) error {
	schemaBytes, err := ir.EmbeddedSchema("project.json")
	if err != nil {
		return fmt.Errorf("load embedded project schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("project.json", strings.NewReader(string(schemaBytes))); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}
	schema, err := compiler.Compile("project.json")
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}

	// Marshal the project to a JSON map so the validator can walk it.
	docBytes, err := json.Marshal(proj)
	if err != nil {
		return fmt.Errorf("marshal project: %w", err)
	}
	var doc any
	if err := json.Unmarshal(docBytes, &doc); err != nil {
		return fmt.Errorf("unmarshal project: %w", err)
	}

	if err := schema.Validate(doc); err != nil {
		appendSchemaIssues(report, err)
	}
	return nil
}

// appendSchemaIssues walks a jsonschema.ValidationError and emits one Issue
// per terminal cause. Code mapping is best-effort: "required" -> Required,
// "pattern" -> Pattern, anything else -> Generic.
func appendSchemaIssues(report *ir.ValidationReport, err error) {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrSchemaGeneric",
			Message:  err.Error(),
		})
		return
	}
	for _, leaf := range collectLeaves(ve) {
		code := classifySchemaError(leaf.KeywordLocation)
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     code,
			Message:  leaf.Message,
			Path:     pointerToPath(leaf.InstanceLocation),
		})
	}
}

func collectLeaves(ve *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return []*jsonschema.ValidationError{ve}
	}
	var out []*jsonschema.ValidationError
	for _, c := range ve.Causes {
		out = append(out, collectLeaves(c)...)
	}
	return out
}

func classifySchemaError(keywordLoc string) string {
	switch {
	case strings.HasSuffix(keywordLoc, "/required"):
		return "ErrSchemaRequiredField"
	case strings.HasSuffix(keywordLoc, "/pattern"):
		return "ErrSchemaPattern"
	case strings.HasSuffix(keywordLoc, "/enum"):
		return "ErrSchemaEnum"
	case strings.HasSuffix(keywordLoc, "/type"):
		return "ErrSchemaType"
	case strings.HasSuffix(keywordLoc, "/maxLength"):
		return "ErrSchemaTooLong"
	case strings.HasSuffix(keywordLoc, "/additionalProperties"):
		return "WarnUnknownField"
	default:
		return "ErrSchemaGeneric"
	}
}

func pointerToPath(ptr string) string {
	// "/components/0/name" -> "components[0].name"
	if ptr == "" || ptr == "/" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(ptr, "/"), "/")
	out := ""
	for i, p := range parts {
		if isDigits(p) {
			out += "[" + p + "]"
		} else {
			if i > 0 {
				out += "."
			}
			out += p
		}
	}
	return out
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
```

Replace the `phase3Schema` stub in `validator.go`:

```go
func phase3Schema(proj *ir.Project, report *ir.ValidationReport) error {
	return phase3SchemaImpl(proj, report)
}
```

- [ ] **Step 4: Run tests, verify pass**

Run:
```bash
go test ./internal/dsl/validator/ -v
```

Expected: all validator tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dsl/validator/phase3_schema.go internal/dsl/validator/phase3_schema_test.go internal/dsl/validator/validator.go
git commit -m "validator: Phase 3 validates project against embedded JSON Schema"
```

---

## Task 12: Wire up the `nimbusfab validate` CLI command

**Files:**
- Modify: `cmd/cli/main.go`
- Create: `cmd/cli/validate.go`
- Create: `cmd/cli/validate_test.go`

- [ ] **Step 1: Write a CLI-level integration test**

Create `cmd/cli/validate_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate_HappyPath(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{root})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("stdout missing OK marker: %s", stdout.String())
	}
}

func TestValidate_RejectsBadAPIVersion(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v999
name: orders
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{root})
	if exit == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "ErrUnknownAPIVersion") {
		t.Errorf("stderr missing ErrUnknownAPIVersion: %s", stderr.String())
	}
}

func TestValidate_RejectsBadName(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: "Has Spaces"
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{root})
	if exit == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "ErrSchema") {
		t.Errorf("output missing ErrSchema: %s", combined)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
```

- [ ] **Step 2: Verify failure**

Run:
```bash
go test ./cmd/cli/ -v
```

Expected: FAIL — `runValidate` undefined.

- [ ] **Step 3: Implement the `validate` subcommand**

Create `cmd/cli/validate.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/klehmer/nimbusfab/internal/dsl/loader"
	"github.com/klehmer/nimbusfab/internal/dsl/validator"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

// runValidate is the testable entry point. main() wraps it for production
// use, but tests invoke runValidate directly to capture stdout/stderr.
func runValidate(stdout, stderr io.Writer, args []string) int {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	ctx := context.Background()

	proj, loaderErr := loader.New().Load(ctx, root)
	if loaderErr != nil {
		// Lift the loader error into a Phase-1 report.
		report, _ := validator.New().ValidateLoaderError(ctx, loaderErr)
		printReport(stdout, stderr, report)
		return 1
	}

	report, err := validator.New().Validate(ctx, proj)
	if err != nil {
		fmt.Fprintln(stderr, "validator failed:", err)
		return 2
	}
	printReport(stdout, stderr, report)
	if !report.OK() {
		return 1
	}
	return 0
}

func printReport(stdout, stderr io.Writer, report *ir.ValidationReport) {
	if report == nil {
		return
	}
	if report.OK() && len(report.Issues) == 0 {
		fmt.Fprintln(stdout, "OK")
		return
	}
	for _, issue := range report.Issues {
		target := stdout
		if issue.Severity == ir.SeverityError {
			target = stderr
		}
		fmt.Fprintln(target, issue.String())
		if issue.Hint != "" {
			fmt.Fprintln(target, "  hint:", issue.Hint)
		}
	}
	if !report.OK() {
		fmt.Fprintln(stderr, "validation failed")
	} else {
		fmt.Fprintln(stdout, "OK with warnings")
	}
}

// newValidateCommand wires the subcommand into cobra.
func newValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a Nimbusfab project directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runValidate(cmd.OutOrStdout(), cmd.ErrOrStderr(), args)
			if code != 0 {
				return errors.New("")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}
```

- [ ] **Step 4: Replace the placeholder `main.go` with a real Cobra root that wires the subcommand**

Replace contents of `cmd/cli/main.go`:

```go
// Command nimbusfab is the Nimbusfab platform CLI. It instantiates an
// in-process Engine and dispatches commands. Phase 1 wires only the
// `validate` subcommand; later phases add `show`, `plan`, `apply`, etc.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "nimbusfab",
		Short:         "Multi-cloud Infrastructure-as-Code framework over OpenTofu",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newValidateCommand())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run tests, verify pass**

Run:
```bash
go test ./cmd/cli/ -v
```

Expected: `TestValidate_HappyPath`, `TestValidate_RejectsBadAPIVersion`, `TestValidate_RejectsBadName` all PASS.

- [ ] **Step 6: Manual smoke test**

Run:
```bash
make cli
./bin/nimbusfab validate internal/dsl/loader/testdata/single-file
```

Expected: prints `OK`, exit 0.

Then:
```bash
echo 'apiVersion: infra.dev/v999
name: bad
stacks:
  dev: {}' > /tmp/bad.yaml
mkdir -p /tmp/bad-proj && cp /tmp/bad.yaml /tmp/bad-proj/project.yaml
./bin/nimbusfab validate /tmp/bad-proj
echo "exit: $?"
```

Expected: prints an `ErrUnknownAPIVersion` issue, exit 1.

- [ ] **Step 7: Commit**

```bash
git add cmd/cli/validate.go cmd/cli/validate_test.go cmd/cli/main.go
git commit -m "cli: wire \`nimbusfab validate\` to loader + validator phases 1-3"
```

---

## Task 13: End-to-end test against the multi-file fixture

**Files:**
- Create: `cmd/cli/integration_test.go`

- [ ] **Step 1: Write the end-to-end test**

Create `cmd/cli/integration_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

// Exercises the full Phase 1 pipeline: multi-file project layout, loader
// discovery, validator phases 1-3, CLI exit code. The fixture lives in the
// loader package's testdata to avoid duplicating files.
func TestValidate_MultiFileFixture(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runValidate(&stdout, &stderr, []string{"../../internal/dsl/loader/testdata/multi-file"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("stdout missing OK: %s", stdout.String())
	}
}
```

- [ ] **Step 2: Run the test**

Run:
```bash
go test ./cmd/cli/ -run TestValidate_MultiFileFixture -v
```

Expected: PASS. If it fails, the most likely cause is a fixture file missing a required field; fix the fixture rather than relaxing validation.

- [ ] **Step 3: Run the full test suite**

Run:
```bash
go test ./...
```

Expected: all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/cli/integration_test.go
git commit -m "cli: end-to-end validate against multi-file fixture"
```

---

## Task 14: Final polish — error rendering and exit codes

**Files:**
- Modify: `cmd/cli/validate.go`

- [ ] **Step 1: Write failing test for warning-vs-error exit codes**

Append to `cmd/cli/validate_test.go`:

```go
func TestValidate_ExitCodes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: orders
stacks:
  dev: {}
`))

	var stdout, stderr bytes.Buffer
	if exit := runValidate(&stdout, &stderr, []string{root}); exit != 0 {
		t.Errorf("clean project exit = %d, want 0", exit)
	}

	// Make project bad: missing stacks.
	mustWrite(t, filepath.Join(root, "project.yaml"), []byte(`
apiVersion: infra.dev/v1alpha1
name: orders
`))
	stdout.Reset()
	stderr.Reset()
	if exit := runValidate(&stdout, &stderr, []string{root}); exit == 0 {
		t.Error("missing stacks should fail validation")
	}
}
```

- [ ] **Step 2: Run the new test**

Run:
```bash
go test ./cmd/cli/ -run TestValidate_ExitCodes -v
```

Expected: PASS (validator already rejects missing-stacks via JSON Schema; this test verifies the exit code wiring).

- [ ] **Step 3: Commit**

```bash
git add cmd/cli/validate_test.go
git commit -m "cli: cover validate exit codes for clean and broken projects"
```

---

## Wrap-up checklist

After completing Tasks 1–14:

- [ ] All tests pass: `go test ./...` shows no failures.
- [ ] Binary builds: `make cli` produces `./bin/nimbusfab`.
- [ ] CLI smoke test: `./bin/nimbusfab validate internal/dsl/loader/testdata/single-file` prints `OK` and exits 0.
- [ ] CLI rejection smoke test: A project with `apiVersion: infra.dev/v999` exits non-zero with `ErrUnknownAPIVersion`.
- [ ] Committed work pushes cleanly: `git push origin main` succeeds (or PR if branch-based).

**What's deferred to Phase 2** (interpolation engine, composition expansion, validator phases 4–9, `nimbusfab show`, REST API endpoints):

- `${var.x}`, `${component.X.outputs.Y}`, etc. — Phase 2 plan
- Composition expansion at validation time — Phase 2 plan
- Cross-component reference resolution and cycle detection — Phase 2 plan
- Semantic checks (cloud-supported-by-type, credentialRef existence, name uniqueness beyond duplicates within a single source) — Phase 2 plan
- Parity contract resolution — Phase 2 plan (gated by Parity subsystem)
- `nimbusfab show` and `--merged` rendering — Phase 3 plan
- REST endpoints `/api/v1/schema/...`, `/api/v1/projects/{id}/validate`, `/api/v1/projects/{id}/render` — Phase 3 plan

**Phase 1 produces working, useful software:** an installable `nimbusfab` binary that catches the most common authoring errors (malformed YAML, unknown apiVersion, missing required fields, invalid names). That alone is a meaningful CI gate for a team starting to write Nimbusfab YAML before any provisioner logic exists.
