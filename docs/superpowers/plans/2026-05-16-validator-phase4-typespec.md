# Validator Phase 4 (Per-Type Spec Schema) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Working per-type spec validation. After Phase 4, a `nimbusfab validate` against a project with a malformed component spec (missing required field, wrong-type value, unknown type) produces structured `ir.Issue`s with paths like `components[2].spec.cidr` — same UX as the existing top-level schema errors from Phase 3.

**Architecture:** New `internal/dsl/validator/phase4_typespec.go` mirrors `phase3_schema.go` but iterates components and validates each `Spec` against its Type's `SpecSchema()`. Validator constructor gains a `components.Registry` argument; all 8 CLI callsites pass `components.DefaultRegistry()`.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-validator-phase4/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- Cross-component ref validation (Phase 5).
- Plugin-loaded types (Phase 6+).
- Per-cloud Type.SupportedClouds() check (latent; tracked in spec).

---

## Task 1: Constructor refactor — Validator takes a Registry

**Files:**
- Edit: `internal/dsl/validator/validator.go`
- Edit: `internal/dsl/validator/validator_test.go`
- Edit: 8 CLI files that call `validator.New()` (apply.go, cost.go, destroy.go, drift.go, parity.go, plan.go, validate.go — the last has two callsites)

- [ ] **Step 1: Update Validator signature**

```go
// Before:
func New() Validator { return &fsValidator{} }
type fsValidator struct{}

// After:
import "github.com/klehmer/nimbusfab/pkg/components"

func New(registry components.Registry) Validator {
    return &fsValidator{registry: registry}
}

type fsValidator struct {
    registry components.Registry
}
```

The registry is stored but not yet consumed by any phase (Task 3 wires it in).

- [ ] **Step 2: Update existing test callers**

`internal/dsl/validator/*_test.go` and `cmd/cli/*_test.go`: any `validator.New()` becomes `validator.New(components.DefaultRegistry())`. Production callers update in step 3.

- [ ] **Step 3: Update production CLI callers**

8 callsites in `cmd/cli/`:
- `apply.go:120`, `cost.go:89`, `destroy.go:108`, `drift.go:99`, `parity.go:83`, `plan.go:86`, `validate.go:28` (ValidateLoaderError path), `validate.go:33` (Validate path).

Each gains `components.DefaultRegistry()` as the argument. Add `import "github.com/klehmer/nimbusfab/pkg/components"` where needed.

- [ ] **Step 4: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go build ./...
PATH=$HOME/.local/go/bin:$PATH go test ./internal/dsl/validator/ ./cmd/cli/
git add internal/dsl/validator/ cmd/cli/
git commit -m "validator: New() takes components.Registry; production callers pass DefaultRegistry"
```

---

## Task 2: Phase 4 implementation

**Files:**
- Create: `internal/dsl/validator/phase4_typespec.go`
- Create: `internal/dsl/validator/phase4_typespec_test.go`

- [ ] **Step 1: Implement phase4TypeSpecImpl**

```go
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
        // Spec is naturally absent if user wrote no spec block; treat that
        // like an empty object so "required" can fire.
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
    resourceURL := name + ".json"
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
```

- [ ] **Step 2: Tests** in `phase4_typespec_test.go`:

- `TestPhase4_HappyPath`: project with one well-formed network component → zero issues.
- `TestPhase4_MissingRequiredField`: network spec without `cidr` → one `ErrValidatorTypeSpec` issue at `components[0].spec`.
- `TestPhase4_WrongTypeValue`: network spec with `subnetCount: "two"` → one `ErrValidatorTypeSpec` at `components[0].spec.subnetCount`.
- `TestPhase4_UnknownType`: component with `type: storrage` → one `ErrValidatorUnknownType` at `components[0].type`; no schema validation attempted.
- `TestPhase4_BadCIDRPattern`: spec with `cidr: not-a-cidr` → `ErrValidatorTypeSpec` mentioning pattern.
- `TestPhase4_SchemaCachingWithinInvocation`: 3 components of the same type → no panic, schema compiled once (functional verification: all 3 pass; cache hit logging not needed for test).
- `TestPhase4_NilRegistry`: passing nil registry returns an error from phase 4 (constructor caller shouldn't allow this; fence belt-and-braces).
- `TestPhase4_EmptySpec`: component with no spec block → required fields still fire.
- `TestPhase4_MultipleComponentsMultipleErrors`: 2 invalid components → 2 issues; phase 4 doesn't abort after first error.

- [ ] **Step 3: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/dsl/validator/ -run TestPhase4 -v
git add internal/dsl/validator/phase4_typespec.go internal/dsl/validator/phase4_typespec_test.go
git commit -m "validator: phase 4 — per-type spec schema validation"
```

---

## Task 3: Wire Phase 4 into the validator pipeline

**Files:**
- Edit: `internal/dsl/validator/validator.go`

- [ ] **Step 1: Add Phase 4 call**

```go
func (v *fsValidator) Validate(ctx context.Context, proj *ir.Project) (*ir.ValidationReport, error) {
    report := &ir.ValidationReport{}
    if proj == nil { /* ... */ }
    if err := phase2APIVersion(proj, report); err != nil { return nil, err }
    if err := phase3Schema(proj, report); err != nil { return nil, err }
    if err := phase4TypeSpec(proj, v.registry, report); err != nil { return nil, err }
    _ = ctx
    return report, nil
}

func phase4TypeSpec(proj *ir.Project, reg components.Registry, report *ir.ValidationReport) error {
    return phase4TypeSpecImpl(proj, reg, report)
}
```

- [ ] **Step 2: Run full validator test suite + CLI suite**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/dsl/validator/ ./cmd/cli/ -v
```

Expected: all green, including existing fixtures (because they use well-formed specs).

- [ ] **Step 3: Build + commit**

```bash
git add internal/dsl/validator/validator.go
git commit -m "validator: wire phase 4 into Validate() pipeline"
```

---

## Task 4: CLI fixtures + a malformed-spec demo

**Files:**
- Create: `cmd/cli/testdata/validator-phase4/missing-cidr.yaml` (project with a network component lacking cidr)
- Create: `cmd/cli/testdata/validator-phase4/unknown-type.yaml`
- Create: `cmd/cli/testdata/validator-phase4/wrong-type-value.yaml`
- Create: `cmd/cli/testdata/validator-phase4/project.yaml` (shared project shell)
- Edit: `cmd/cli/validate_test.go` to assert these fixtures produce the right error codes

- [ ] **Step 1: Author 3 small fixtures**

Each fixture is a minimal multi-file project (project.yaml + one components/*.yaml file) exercising one failure mode.

- [ ] **Step 2: Add validate_test.go assertions**

For each fixture, run `runValidate` with `--strict`-equivalent and assert: exit non-zero, output contains the expected `Code:` value (`ErrValidatorTypeSpec` or `ErrValidatorUnknownType`).

- [ ] **Step 3: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./cmd/cli/ -run TestValidate -v
git add cmd/cli/testdata/validator-phase4/ cmd/cli/validate_test.go
git commit -m "cli: add validator-phase4 fixtures (missing-cidr, unknown-type, wrong-type-value)"
```

---

## Task 5: Docs

**Files:**
- Edit: `README.md` (status line)
- Edit: `CHANGELOG.md` (new section)

- [ ] **Step 1: Update README** status to include Validator Phase 4.

- [ ] **Step 2: Add CHANGELOG entry** under a new "Validator Phase 4" section listing: per-type schema enforcement; ErrValidatorUnknownType / ErrValidatorTypeSpec codes; Validator constructor signature change; 3 fixtures added.

- [ ] **Step 3: Final full suite + gofmt check**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/dsl/validator/ cmd/cli/
```

- [ ] **Step 4: Commit** `docs: Validator Phase 4 merged — per-type spec validation`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/validator-phase4 -m "Merge feat/validator-phase4: per-type spec schema validation"
git push origin main
git push origin feat/validator-phase4
```
