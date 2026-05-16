# Secrets Phase 1 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Working credential resolution. After Phase 1, `nimbusfab apply` against a project with `credentialRef: aws-dev` resolves that ref through a configured backend chain (env-var first, then file) and the resolved key/value pairs become the Tofu runner's per-command env so the cloud provider authenticates.

**Architecture:** New `pkg/secrets/{env,file,default}.go` for concrete backends. New helper `pkg/provisioner/secrets.go` that resolves a credentialRef into a `map[string]string` env. Orchestrator's apply/destroy/drift each call the helper before constructing `tofu.Workspace`. CLI commands wire `secrets.DefaultBackend()` into `engine.Config.SecretsBackend`.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-secrets-phase1/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- Vault / KMS backends.
- OIDC / WIF.
- Adapter-side env-var translation.
- Secret payload schema validation.
- Audit log persistence.

---

## Task 1: EnvBackend

**Files:**
- Create: `pkg/secrets/env.go`
- Create: `pkg/secrets/env_test.go`

- [ ] **Step 1: Implement**

```go
package secrets

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "strings"
)

// EnvBackend resolves refs from process environment variables. Convention:
// ref "aws-dev" maps to env var "NIMBUSFAB_SECRET_AWS_DEV". The env var's
// value MUST be a JSON object; the parsed map is returned.
type EnvBackend struct{}

func (*EnvBackend) Kind() string { return "env" }

func (*EnvBackend) Resolve(ctx context.Context, ref string) (map[string]any, error) {
    if ref == "" {
        return nil, nil
    }
    key := "NIMBUSFAB_SECRET_" + envify(ref)
    raw := os.Getenv(key)
    if raw == "" {
        return nil, nil
    }
    var out map[string]any
    if err := json.Unmarshal([]byte(raw), &out); err != nil {
        return nil, fmt.Errorf("secrets/env: %s contains invalid JSON: %w", key, err)
    }
    return out, nil
}

func envify(ref string) string {
    return strings.ToUpper(strings.ReplaceAll(ref, "-", "_"))
}
```

- [ ] **Step 2: Tests** in `env_test.go`:

- `TestEnv_KindIsEnv`
- `TestEnv_UnsetReturnsNilNil`
- `TestEnv_HappyPath`: set env var, resolve, assert payload shape
- `TestEnv_BadJSONReturnsError`
- `TestEnv_EmptyRefReturnsNilNil`
- `TestEnv_RefMapping`: confirm `aws-dev` → `NIMBUSFAB_SECRET_AWS_DEV`

Use `t.Setenv` for hermetic env var setup.

- [ ] **Step 3: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/secrets/ -v
git add pkg/secrets/env.go pkg/secrets/env_test.go
git commit -m "secrets: EnvBackend (NIMBUSFAB_SECRET_<UPPER_REF> JSON)"
```

---

## Task 2: FileBackend

**Files:**
- Create: `pkg/secrets/file.go`
- Create: `pkg/secrets/file_test.go`

- [ ] **Step 1: Implement**

```go
package secrets

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
)

// FileBackend reads JSON files from a directory. Convention: ref "aws-dev"
// resolves to "<Dir>/aws-dev.json". Missing files return (nil, nil) so the
// backend can be chained.
type FileBackend struct {
    Dir string
}

// NewFileBackend returns a FileBackend. If dir is empty, defaults to
// ~/.nimbusfab/secrets when the user's home dir is resolvable.
func NewFileBackend(dir string) *FileBackend {
    if dir == "" {
        if home, err := os.UserHomeDir(); err == nil {
            dir = filepath.Join(home, ".nimbusfab", "secrets")
        }
    }
    return &FileBackend{Dir: dir}
}

func (*FileBackend) Kind() string { return "file" }

func (b *FileBackend) Resolve(ctx context.Context, ref string) (map[string]any, error) {
    if ref == "" || b.Dir == "" {
        return nil, nil
    }
    path := filepath.Join(b.Dir, ref+".json")
    raw, err := os.ReadFile(path)
    if errors.Is(err, fs.ErrNotExist) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("secrets/file: read %s: %w", path, err)
    }
    var out map[string]any
    if err := json.Unmarshal(raw, &out); err != nil {
        return nil, fmt.Errorf("secrets/file: %s invalid JSON: %w", path, err)
    }
    return out, nil
}
```

- [ ] **Step 2: Tests** in `file_test.go`:

- `TestFile_KindIsFile`
- `TestFile_MissingReturnsNilNil` (uses `t.TempDir()`)
- `TestFile_HappyPath`: write a JSON file, resolve, assert payload
- `TestFile_BadJSONReturnsError`
- `TestFile_EmptyRefReturnsNilNil`
- `TestFile_EmptyDirReturnsNilNil`

- [ ] **Step 3: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/secrets/ -v
git add pkg/secrets/file.go pkg/secrets/file_test.go
git commit -m "secrets: FileBackend (~/.nimbusfab/secrets/<ref>.json)"
```

---

## Task 3: DefaultBackend chain

**Files:**
- Create: `pkg/secrets/default.go`
- Create: `pkg/secrets/default_test.go`

- [ ] **Step 1: Implement**

```go
package secrets

// DefaultBackend returns the Phase-1 default backend chain. Tries the
// EnvBackend first (cheapest), then FileBackend rooted at
// ~/.nimbusfab/secrets/. First non-empty resolution wins.
func DefaultBackend() Backend {
    return NewChain(&EnvBackend{}, NewFileBackend(""))
}
```

- [ ] **Step 2: Tests**:

- `TestDefaultBackend_KindIsChain`
- `TestDefaultBackend_EnvFirstWins`: set both env var AND file with different payloads; assert env wins
- `TestDefaultBackend_FileFallback`: env unset, file present; assert file payload returned
- `TestDefaultBackend_NeitherReturnsNotFound`: both empty; assert `ErrNotFound`

Use `t.Setenv` and `t.TempDir()`, plus a `*FileBackend` constructed directly to control the dir.

- [ ] **Step 3: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/secrets/ -v
git add pkg/secrets/default.go pkg/secrets/default_test.go
git commit -m "secrets: DefaultBackend chains EnvBackend → FileBackend"
```

---

## Task 4: Provisioner integration

**Files:**
- Create: `pkg/provisioner/secrets.go`
- Create: `pkg/provisioner/secrets_test.go`
- Edit: `pkg/provisioner/apply.go`
- Edit: `pkg/provisioner/destroy.go`
- Edit: `pkg/provisioner/drift.go`

- [ ] **Step 1: Helper in `secrets.go`**

```go
package provisioner

import (
    "context"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/secrets"
)

// resolveEnvFor turns a credentialRef into a string-keyed env var map by
// asking the configured secrets backend. Nil backend or empty ref → empty
// map (caller proceeds with whatever the process env contains). Missing
// ref with a non-nil backend → ErrSecretsRefUnresolved.
func resolveEnvFor(ctx context.Context, backend secrets.Backend, ref string) (map[string]string, error) {
    if backend == nil || ref == "" {
        return map[string]string{}, nil
    }
    payload, err := backend.Resolve(ctx, ref)
    if err != nil {
        return nil, fmt.Errorf("ErrSecretsRefUnresolved: resolving %q: %w", ref, err)
    }
    if payload == nil {
        return nil, fmt.Errorf("ErrSecretsRefUnresolved: no backend resolved credentialRef %q", ref)
    }
    env := make(map[string]string, len(payload))
    for k, v := range payload {
        s, ok := v.(string)
        if !ok {
            return nil, fmt.Errorf("secrets payload for %s: key %q has non-string value (%T)", ref, k, v)
        }
        env[k] = s
    }
    return env, nil
}
```

- [ ] **Step 2: Plumb a SecretsBackend into the provisioner orchestrator**

The orchestrator currently takes a `*ProvisionerConfig` or similar — examine `apply.go`/`destroy.go`/`drift.go` and add `SecretsBackend secrets.Backend` to whichever struct flows through. Engine constructs it from `Config.SecretsBackend`.

Then at each tofu.Workspace construction site, swap:

```go
ws := tofu.Workspace{Dir: tp.WorkspaceDir}
```

for:

```go
env, err := resolveEnvFor(ctx, secretsBackend, target.CredentialRef)
if err != nil { /* fail the target */ }
ws := tofu.Workspace{Dir: tp.WorkspaceDir, Environment: env}
```

Three callsites: `apply.go:94`, `apply.go:126`, `destroy.go:39`, `drift.go:28`. Search for `tofu.Workspace{Dir:` to confirm.

- [ ] **Step 3: Tests** in `secrets_test.go`:

- `TestResolveEnvFor_NilBackend`: nil backend → empty map, no error
- `TestResolveEnvFor_EmptyRef`: nil ref → empty map, no error
- `TestResolveEnvFor_Resolves`: stub backend returning a payload → map with string values
- `TestResolveEnvFor_PayloadHasNonStringValue`: returns error
- `TestResolveEnvFor_BackendError`: backend returns error → wrapped error
- `TestResolveEnvFor_NoBackendResolved`: backend returns (nil, nil) → ErrSecretsRefUnresolved

End-to-end: extend `apply_test.go` (or create a focused new test) to assert the Workspace.Environment field is populated when SecretsBackend is configured and a target has a non-empty credentialRef. Use the existing FakeRunner to inspect what env was passed in.

- [ ] **Step 4: Build + test + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./pkg/provisioner/ ./pkg/secrets/ -v
git add pkg/provisioner/secrets.go pkg/provisioner/secrets_test.go pkg/provisioner/apply.go pkg/provisioner/destroy.go pkg/provisioner/drift.go
git commit -m "provisioner: resolve credentialRef → Workspace.Environment via SecretsBackend"
```

---

## Task 5: Engine + CLI wiring

**Files:**
- Edit: `pkg/engine/*.go` (whichever file constructs the provisioner) to pass `cfg.SecretsBackend` through
- Create: `cmd/cli/secrets.go` (defaultSecretsBackend helper)
- Edit: `cmd/cli/{apply,destroy,drift,plan}.go` to wire `SecretsBackend: defaultSecretsBackend()` into engine.Config

- [ ] **Step 1: Engine plumbing**

Find where the engine constructs the provisioner / orchestrator and pass `cfg.SecretsBackend` through. May need to grep `runtimeEngine` / `provisioner.New` to locate the construction.

- [ ] **Step 2: CLI helper**

```go
// cmd/cli/secrets.go
package main

import "github.com/klehmer/nimbusfab/pkg/secrets"

func defaultSecretsBackend() secrets.Backend {
    return secrets.DefaultBackend()
}
```

- [ ] **Step 3: CLI command wiring**

In each command file where `engine.Config{...}` is constructed, add:

```go
cfg := engine.Config{
    // ... existing fields
    SecretsBackend: defaultSecretsBackend(),
}
```

- [ ] **Step 4: Run full suite**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
```

Expected: existing tests still green. (Tests don't set `NIMBUSFAB_SECRET_*` env vars and the full-stack fixture's credentialRef strings `aws-dev`/`azure-dev`/`gcp-dev` won't resolve — but the existing FakeRunner-based tests don't actually invoke Apply against `aws-dev`-style refs in a way that depends on env injection. Verify by running and iterating if failures surface.)

If a fixture-based apply test fails because it now expects ErrSecretsRefUnresolved, set the env var inside the test with `t.Setenv` before invoking the CLI.

- [ ] **Step 5: Commit**

```bash
git add pkg/engine/ cmd/cli/secrets.go cmd/cli/apply.go cmd/cli/destroy.go cmd/cli/drift.go cmd/cli/plan.go
git commit -m "engine, cli: wire DefaultBackend through Config.SecretsBackend"
```

---

## Task 6: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: Update README** status line to include Secrets Phase 1. Brief note: "credentialRef now resolves via env-var or ~/.nimbusfab/secrets/ JSON files; cloud providers see correct env vars."

- [ ] **Step 2: CHANGELOG entry** under "Unreleased — Secrets Phase 1" with the two backends, the chain, the payload-is-envvars design choice, the engine integration, and the out-of-scope deferrals (Vault / KMS / OIDC).

- [ ] **Step 3: Final test + gofmt**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l pkg/secrets/ pkg/provisioner/ cmd/cli/ pkg/engine/
```

- [ ] **Step 4: Commit** `docs: Secrets Phase 1 merged — env + file backends; resolution into tofu env`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/secrets-phase1 -m "Merge feat/secrets-phase1: env + file backends + credentialRef resolution"
git push origin main
git push origin feat/secrets-phase1
```
