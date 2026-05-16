# Validator Phase 5 (Cross-Component Refs) Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Working cross-component ref validation. After Phase 5, `nimbusfab validate` against a project with typo'd ref component names, refs to nonexistent outputs, self-refs, or cyclic ref graphs produces structured `ir.Issue`s at `components[i].refs[j].component` / `.output`.

**Architecture:** New `internal/dsl/validator/phase5_refs.go` mirrors `phase4_typespec.go` shape: takes `*ir.Project` + `components.Registry`, iterates components and their refs, emits Issues. Cycle detection runs after per-ref checks via DFS with three-color marking.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-validator-phase5/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- Output Kind/value-shape matching.
- Ref interpolation in spec strings (`${component.foo.output.bar}`).
- Composition graph validation (v2 Composed components).
- Inventory-state-aware ref validation.

---

## Task 1: Phase 5 implementation

**Files:**
- Create: `internal/dsl/validator/phase5_refs.go`
- Create: `internal/dsl/validator/phase5_refs_test.go`

- [ ] **Step 1: Implement phase5RefsImpl**

```go
package validator

import (
    "fmt"
    "strings"

    "github.com/klehmer/nimbusfab/pkg/components"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

// phase5RefsImpl validates the cross-component reference graph:
//   - each ref points at an existing component
//   - the referenced output is declared by the target's Type
//   - no component refs itself
//   - no cycles in the ref graph
func phase5RefsImpl(proj *ir.Project, registry components.Registry, report *ir.ValidationReport) error {
    if registry == nil {
        return fmt.Errorf("validator phase 5: nil registry")
    }
    // Build component-name → index map and collect names once.
    nameIdx := make(map[string]int, len(proj.Components))
    knownNames := make([]string, 0, len(proj.Components))
    for i, c := range proj.Components {
        nameIdx[c.Name] = i
        knownNames = append(knownNames, c.Name)
    }

    // Per-ref checks: self / unknown component / unknown output.
    for i, comp := range proj.Components {
        for j, ref := range comp.Refs {
            if ref.Component == comp.Name {
                report.Issues = append(report.Issues, ir.Issue{
                    Severity: ir.SeverityError,
                    Code:     "ErrValidatorRefSelf",
                    Message:  fmt.Sprintf("component %q refs itself", comp.Name),
                    Path:     fmt.Sprintf("components[%d].refs[%d].component", i, j),
                })
                continue
            }
            targetIdx, ok := nameIdx[ref.Component]
            if !ok {
                report.Issues = append(report.Issues, ir.Issue{
                    Severity: ir.SeverityError,
                    Code:     "ErrValidatorRefUnknownComponent",
                    Message:  fmt.Sprintf("ref points at unknown component %q (known: %s)", ref.Component, strings.Join(knownNames, ", ")),
                    Path:     fmt.Sprintf("components[%d].refs[%d].component", i, j),
                })
                continue
            }
            target := proj.Components[targetIdx]
            t, ok := registry.Type(target.Type)
            if !ok {
                // Phase 4 already flagged the target's bad type; skip
                // output check rather than emit noise.
                continue
            }
            outs := t.Outputs()
            if ref.Output == "" {
                report.Issues = append(report.Issues, ir.Issue{
                    Severity: ir.SeverityError,
                    Code:     "ErrValidatorRefUnknownOutput",
                    Message:  fmt.Sprintf("component %q (type %s) ref has empty output name (declared: %s)", target.Name, target.Type, joinOutputNames(outs)),
                    Path:     fmt.Sprintf("components[%d].refs[%d].output", i, j),
                })
                continue
            }
            if _, ok := outs[ref.Output]; !ok {
                report.Issues = append(report.Issues, ir.Issue{
                    Severity: ir.SeverityError,
                    Code:     "ErrValidatorRefUnknownOutput",
                    Message:  fmt.Sprintf("component %q (type %s) does not declare output %q (declared: %s)", target.Name, target.Type, ref.Output, joinOutputNames(outs)),
                    Path:     fmt.Sprintf("components[%d].refs[%d].output", i, j),
                })
            }
        }
    }

    // Cycle detection (skip self-loops; those are already reported).
    detectCycles(proj, nameIdx, report)
    return nil
}

func joinOutputNames(outs map[string]components.OutputType) string {
    names := make([]string, 0, len(outs))
    for k := range outs {
        names = append(names, k)
    }
    sort.Strings(names)
    return strings.Join(names, ", ")
}

// Three-color DFS cycle detection. WHITE=0 (unvisited), GRAY=1 (in
// current DFS path), BLACK=2 (fully explored).
func detectCycles(proj *ir.Project, nameIdx map[string]int, report *ir.ValidationReport) {
    const (
        WHITE = 0
        GRAY  = 1
        BLACK = 2
    )
    color := make([]int, len(proj.Components))
    parent := make([]int, len(proj.Components))
    for i := range parent {
        parent[i] = -1
    }
    var reported map[string]bool = map[string]bool{}

    var dfs func(u int)
    dfs = func(u int) {
        color[u] = GRAY
        for _, ref := range proj.Components[u].Refs {
            v, ok := nameIdx[ref.Component]
            if !ok || v == u {
                continue // unknown or self-loop: handled elsewhere
            }
            switch color[v] {
            case WHITE:
                parent[v] = u
                dfs(v)
            case GRAY:
                // back-edge: cycle from v to u
                emitCycle(proj, parent, u, v, reported, report)
            }
        }
        color[u] = BLACK
    }

    for i := range proj.Components {
        if color[i] == WHITE {
            dfs(i)
        }
    }
}

func emitCycle(proj *ir.Project, parent []int, u, v int, reported map[string]bool, report *ir.ValidationReport) {
    // Walk back from u to v collecting names; closing edge is u→v.
    path := []string{proj.Components[u].Name}
    for cur := u; cur != v && cur != -1; cur = parent[cur] {
        if parent[cur] == -1 {
            break
        }
        path = append([]string{proj.Components[parent[cur]].Name}, path...)
        if parent[cur] == v {
            break
        }
    }
    // Make sure v is the start of the path; close with v at the end too.
    if len(path) == 0 || path[0] != proj.Components[v].Name {
        path = append([]string{proj.Components[v].Name}, path...)
    }
    closed := append(append([]string(nil), path...), proj.Components[v].Name)
    key := strings.Join(closed, "→")
    if reported[key] {
        return
    }
    reported[key] = true

    firstIdx := -1
    for i, c := range proj.Components {
        if c.Name == path[0] {
            firstIdx = i
            break
        }
    }
    pathStr := fmt.Sprintf("components[%d].refs", firstIdx)
    report.Issues = append(report.Issues, ir.Issue{
        Severity: ir.SeverityError,
        Code:     "ErrValidatorRefCycle",
        Message:  fmt.Sprintf("ref cycle detected: %s", strings.Join(closed, " → ")),
        Path:     pathStr,
    })
}
```

Don't forget `import "sort"`.

- [ ] **Step 2: Tests** in `phase5_refs_test.go`:

- `TestPhase5_HappyPath`: project with 3 components where one refs the other two, no cycles → zero issues.
- `TestPhase5_UnknownComponent`: ref's component name is typo'd → ErrValidatorRefUnknownComponent at `components[i].refs[j].component`.
- `TestPhase5_UnknownOutput`: ref against a real component but output name not in Type.Outputs() → ErrValidatorRefUnknownOutput listing declared outputs.
- `TestPhase5_SelfRef`: component refs itself → ErrValidatorRefSelf; output check skipped.
- `TestPhase5_CycleLength2`: A↔B → ErrValidatorRefCycle with path `a → b → a`.
- `TestPhase5_CycleLength3`: A→B→C→A → ErrValidatorRefCycle with path `a → b → c → a`.
- `TestPhase5_TargetHasBadType`: target component's type isn't registered → no extra Phase 5 noise (Phase 4 owns that error).
- `TestPhase5_EmptyOutputField`: ref with `output: ""` → ErrValidatorRefUnknownOutput "empty output name".
- `TestPhase5_NilRegistry`: nil registry → returns error from phase 5.
- `TestPhase5_MultipleErrorsPerComponent`: one component with 3 bad refs → 3 issues, all reported.

- [ ] **Step 3: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/dsl/validator/ -run TestPhase5 -v
git add internal/dsl/validator/phase5_refs.go internal/dsl/validator/phase5_refs_test.go
git commit -m "validator: phase 5 — cross-component ref validation"
```

---

## Task 2: Wire Phase 5 into pipeline

**Files:**
- Edit: `internal/dsl/validator/validator.go`

- [ ] **Step 1: Add Phase 5 call after Phase 4**

```go
if err := phase4TypeSpec(proj, v.registry, report); err != nil {
    return nil, err
}
if err := phase5Refs(proj, v.registry, report); err != nil {
    return nil, err
}
```

```go
func phase5Refs(proj *ir.Project, reg components.Registry, report *ir.ValidationReport) error {
    return phase5RefsImpl(proj, reg, report)
}
```

- [ ] **Step 2: Run full validator test suite + CLI suite**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/dsl/validator/ ./cmd/cli/ -v
```

Expected: all green. The full-stack fixture has refs but they're all well-formed.

- [ ] **Step 3: Commit** `validator: wire phase 5 into Validate() pipeline`

---

## Task 3: CLI integration tests

**Files:**
- Edit: `cmd/cli/validate_test.go` (add 3-4 phase-5 tests)

- [ ] **Step 1: Add tests** mirroring Phase 4 style. Each writes a small TempDir project with a ref-graph fault and asserts the right error code surfaces in CLI output:

- `TestValidate_Phase5_UnknownComponent`
- `TestValidate_Phase5_UnknownOutput`
- `TestValidate_Phase5_SelfRef`
- `TestValidate_Phase5_Cycle`

Use the existing `writePhase4Project` helper as a model; introduce `writePhase5Project(t, root, ...components)` if helpful.

- [ ] **Step 2: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./cmd/cli/ -run TestValidate_Phase5 -v
git add cmd/cli/validate_test.go
git commit -m "cli: add validator-phase5 tests (unknown-component, unknown-output, self-ref, cycle)"
```

---

## Task 4: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: Update README** status line.

- [ ] **Step 2: Add CHANGELOG entry** under "Unreleased — Validator Phase 5" with the four new issue codes; mention cycle detection algorithm.

- [ ] **Step 3: Final test + gofmt**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/dsl/validator/ cmd/cli/
```

- [ ] **Step 4: Commit** `docs: Validator Phase 5 merged — cross-component ref validation`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/validator-phase5 -m "Merge feat/validator-phase5: cross-component ref validation"
git push origin main
git push origin feat/validator-phase5
```
