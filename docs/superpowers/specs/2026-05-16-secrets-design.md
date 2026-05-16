# Secrets Phase 1 Subsystem Spec

**Status:** Subsystem spec. Adds concrete secrets backends (env-var, file) and wires `credentialRef` resolution through the engine so the resolved payload becomes the Tofu runner's environment. Until this lands, `credentialRef` strings flow through unresolved and `tofu` commands inherit whatever the user's process environment happens to contain.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (`SecretsBackend` interface + `Credentials` flow)
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (provisioner orchestrator + Workspace.Environment)
- `pkg/secrets/backend.go` (Backend interface + Chain composition — already exist)

**Depended on by:**
- All `nimbusfab apply`/`destroy`/`drift` invocations against real clouds. Without this, those commands silently rely on whatever credentials happen to be in the user's process env — works for some users (those with `aws configure` set up) but unpredictable as a product.
- Web app — server-side credential resolution per request, isolating user identities.
- Cost Collector — billing API access requires resolved credentials.
- v2 OIDC / WIF / Vault backends — extend the same Chain.

---

## Context

`pkg/secrets/Backend` is defined (Resolve(ctx, ref) → map[string]any), and `Chain` composes backends in order. But:
- No concrete backends exist (no env, no file, no Vault).
- `engine.Config.SecretsBackend` is a field but defaults to `nil`; CLI commands don't set it.
- The provisioner constructs `cloud.Credentials{Ref: target.CredentialRef}` with `Payload: nil` — the resolution call is never made.
- `tofu.Workspace.Environment` is plumbed through to `exec.Cmd.Env` in `exec_runner.go`, but every callsite constructs `tofu.Workspace{Dir: ...}` with no Environment.

End result: a project with `credentialRef: aws-prod` and an Apply against AWS only works if the user's shell already has the right `AWS_*` env vars set. Otherwise `tofu apply` fails with "no credentials" from the AWS provider.

Phase 1 closes this gap with the simplest credible design:
- **Payload-is-envvars.** The secrets backend's resolved map is taken to be a map of env var names → string values. The engine asserts string values, builds a `map[string]string`, and merges into `Workspace.Environment`. The user/admin who manages the secret material is responsible for using the env var names each cloud provider expects (AWS_ACCESS_KEY_ID, ARM_CLIENT_SECRET, GOOGLE_APPLICATION_CREDENTIALS, etc.).
- **Two backends:** env-var (cheap, dev-mode default) and file (reads JSON files from a configurable directory; useful for local development and CI).
- **Default chain:** env → file. First non-empty wins.
- **No-backend mode preserved:** if `engine.Config.SecretsBackend` is nil, the engine passes empty env (current behavior — relies on process env).

**Design principles:**
1. **Cloud-agnostic engine code.** The engine doesn't know AWS_ACCESS_KEY_ID from ARM_CLIENT_SECRET. The secret payload is a flat string map; the engine merges it in.
2. **Resolution at operation time, not plan time.** Apply / destroy / drift each resolve credentials immediately before invoking the runner. Plan doesn't need them (adapters' `ProviderBlock` never reads `Credentials.Payload` — they all instruct users to set env vars at runtime).
3. **Audit-friendly.** Every resolution logs `(ref, backend.Kind(), success/failure)` so it's clear which backend served which ref.
4. **Fail-fast on a missing ref.** If `credentialRef: aws-prod` is set on a target but no backend can resolve it, the operation aborts with a clear error before running any tofu commands. (Without this, the user gets a tofu error mid-apply — much harder to diagnose.)
5. **No process-env mutation.** The runner sets credentials via per-command `cmd.Env`, never `os.Setenv`. Concurrent target operations stay isolated.

---

## Scope

**In scope (this spec):**
- `pkg/secrets/env.go` — `EnvBackend` looks up `NIMBUSFAB_SECRET_<UPPER_REF>` env vars; value is JSON object decoded to `map[string]any`.
- `pkg/secrets/file.go` — `FileBackend` reads `<dir>/<ref>.json` (configurable dir; default `~/.nimbusfab/secrets/`). JSON shape: `{"AWS_ACCESS_KEY_ID": "...", ...}`.
- `pkg/secrets/default.go` — `DefaultBackend()` returns `Chain(Env, File)`. CLI uses this.
- Engine integration: provisioner's apply / destroy / drift call `cfg.SecretsBackend.Resolve(ref)` before constructing `tofu.Workspace` and pass the result through as `Workspace.Environment`. Nil backend → empty env (no-op).
- CLI wiring: `cmd/cli/secrets.go` helper `defaultSecretsBackend()` returns the chain; all 4 op commands (apply, destroy, drift, plan-still-no-op-but-pass-it-anyway-for-symmetry) thread it through `engine.Config.SecretsBackend`.
- Issue: `ErrSecretsRefUnresolved` returned from the engine when the backend can't resolve a non-empty `credentialRef`.
- Tests for both backends + the engine integration.

**Out of scope (deferred):**
- Vault backend — separate phase. The `Backend` interface accommodates it; just no concrete impl yet.
- Cloud KMS backends (AWS Secrets Manager, Azure Key Vault, GCP Secret Manager) — same. v2.
- OIDC / Workload Identity Federation — v2. Requires per-cloud trust setup outside the engine's scope.
- Per-target credential override (a single component with multiple targets pointing at different credential refs) — v2 enhancement; current model is one `credentialRef` per target which already supports this case.
- Credential rotation / expiry detection — v2.
- Secret payload validation per cloud (e.g., "aws ref must have AWS_ACCESS_KEY_ID") — adapter-side concern; v2 if useful at all.
- Audit-log persistence — covered by Inventory Phase 2's AuditLog repo; secrets backend just logs to the engine's logger for now.
- Encryption at rest for the file backend — out of scope; file backend is documented as dev-only.
- Process-wide caching of resolved credentials — out of scope; per-target resolution is fine for v1 (call volume is low).

---

## Concrete backends

### EnvBackend

Lookup convention: `NIMBUSFAB_SECRET_<UPPER_REF>` where the ref string is uppercased and `-` becomes `_`. So `aws-dev` looks up `NIMBUSFAB_SECRET_AWS_DEV`.

The env var value is a JSON object. Example shell setup:

```bash
export NIMBUSFAB_SECRET_AWS_DEV='{"AWS_ACCESS_KEY_ID":"AKIA...","AWS_SECRET_ACCESS_KEY":"..."}'
```

Resolve returns the parsed object as `map[string]any`. If the env var is unset, returns `(nil, nil)` (lets a Chain fall through to the next backend). If set but invalid JSON, returns an error.

```go
type EnvBackend struct{}

func (*EnvBackend) Kind() string { return "env" }
func (*EnvBackend) Resolve(ctx context.Context, ref string) (map[string]any, error) {
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

### FileBackend

Reads `<dir>/<ref>.json`. Directory configurable; default `~/.nimbusfab/secrets/`. Files SHOULD be `0600`; backend logs a warning if it's group/world-readable (does not refuse — user might intentionally share in a dev env).

```go
type FileBackend struct {
    Dir string // default: ~/.nimbusfab/secrets
}

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
    path := filepath.Join(b.Dir, ref+".json")
    raw, err := os.ReadFile(path)
    if os.IsNotExist(err) {
        return nil, nil
    }
    if err != nil { return nil, fmt.Errorf("secrets/file: %w", err) }
    var out map[string]any
    if err := json.Unmarshal(raw, &out); err != nil {
        return nil, fmt.Errorf("secrets/file: %s invalid JSON: %w", path, err)
    }
    return out, nil
}
```

### DefaultBackend

```go
func DefaultBackend() Backend {
    return NewChain(&EnvBackend{}, NewFileBackend(""))
}
```

The Chain already exists; the first backend to return non-nil wins.

---

## Engine integration

Phase 1 resolves credentials at operation time, not plan time. The wiring lives in three orchestrator entry points (`apply.go`, `destroy.go`, `drift.go`).

Pattern:

```go
// Inside orchestrator.go before invoking the runner per target:
env, err := resolveEnvFor(ctx, cfg.SecretsBackend, target.CredentialRef, target.Cloud)
if err != nil {
    return TargetResult{Status: StatusFailed, Err: err}
}

ws := tofu.Workspace{Dir: tp.WorkspaceDir, Environment: env}
```

Helper:

```go
// resolveEnvFor resolves the credentialRef via the backend and returns
// a string-valued env-var map. Nil backend or empty ref → empty map.
func resolveEnvFor(ctx context.Context, backend secrets.Backend, ref, cloud string) (map[string]string, error) {
    if backend == nil || ref == "" {
        return map[string]string{}, nil
    }
    payload, err := backend.Resolve(ctx, ref)
    if err != nil {
        return nil, fmt.Errorf("ErrSecretsRefUnresolved: %s: %w", ref, err)
    }
    if payload == nil {
        return nil, fmt.Errorf("ErrSecretsRefUnresolved: no backend resolved %q (cloud=%s)", ref, cloud)
    }
    env := make(map[string]string, len(payload))
    for k, v := range payload {
        s, ok := v.(string)
        if !ok {
            return nil, fmt.Errorf("secrets payload for %s: key %q has non-string value %T", ref, k, v)
        }
        env[k] = s
    }
    return env, nil
}
```

---

## CLI wiring

`cmd/cli/secrets.go`:

```go
package main

import "github.com/klehmer/nimbusfab/pkg/secrets"

// defaultSecretsBackend returns the Phase-1 default backend chain (env → file).
// Apps that want to inject a different backend (Vault, KMS, etc.) construct
// engine.Config.SecretsBackend explicitly instead of calling this.
func defaultSecretsBackend() secrets.Backend {
    return secrets.DefaultBackend()
}
```

All four operation CLIs (apply, destroy, drift, plan) build `engine.Config` with `SecretsBackend: defaultSecretsBackend()`. (Plan doesn't currently run any tofu commands that need creds, but threading it through anyway keeps the wiring consistent for when Plan grows refresh-checking.)

---

## Logging

Every resolution emits one log entry via the engine's logger:

```go
cfg.Logger.Info("secrets resolved", "ref", ref, "backend", backendKind, "keys", keyNames)
```

The `keys` list contains the env var names (e.g., `["AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"]`) — NOT the values. Audit trail: who resolved which ref via which backend, what env vars it contained. Failed resolutions log at WARN level with the error.

---

## Error model additions

| Code | Origin | Meaning |
|---|---|---|
| `ErrSecretsRefUnresolved` | provisioner | No backend in the chain could resolve a non-empty credentialRef |
| `secrets/env: ... invalid JSON` | EnvBackend | `NIMBUSFAB_SECRET_*` was set but couldn't be parsed |
| `secrets/file: ... invalid JSON` | FileBackend | File exists but contents aren't valid JSON |

No new issue codes in `ir.Issue` — these surface as runtime errors from the engine, not validation errors. (A future validator phase could check that every `credentialRef` resolves at plan time; that's a v2 enhancement.)

---

## Worked examples

### Happy path: env-var backend

```bash
export NIMBUSFAB_SECRET_AWS_DEV='{"AWS_ACCESS_KEY_ID":"AKIAEXAMPLE","AWS_SECRET_ACCESS_KEY":"secret","AWS_REGION":"us-east-1"}'
nimbusfab apply --stack dev myproject/
```

Engine resolves `aws-dev` → `{"AWS_ACCESS_KEY_ID":..., "AWS_SECRET_ACCESS_KEY":..., "AWS_REGION":"us-east-1"}` → `Workspace.Environment` → tofu sees these env vars → AWS provider authenticates.

### Happy path: file backend fallback

```bash
mkdir -p ~/.nimbusfab/secrets
cat > ~/.nimbusfab/secrets/azure-dev.json <<EOF
{"ARM_CLIENT_ID":"...","ARM_CLIENT_SECRET":"...","ARM_TENANT_ID":"...","ARM_SUBSCRIPTION_ID":"..."}
EOF
chmod 0600 ~/.nimbusfab/secrets/azure-dev.json
nimbusfab apply --stack dev myproject/
```

Env backend looks up `NIMBUSFAB_SECRET_AZURE_DEV`, finds nothing (returns nil). Chain falls through to file backend, which reads the file, returns the payload. Engine merges into Workspace.Environment.

### Failure: ref not configured

```bash
nimbusfab apply --stack dev myproject/   # credentialRef: azure-prod, no env var, no file
```

Engine logs WARN; returns `TargetResult{Status: Failed, Err: ErrSecretsRefUnresolved}`; the failing target is skipped per the orchestrator's policy (leave / rollback / retry-failed — same as any other target failure).

### No-backend mode preserved

If the user runs against a project where they've configured `aws configure` to set `~/.aws/credentials` and `AWS_PROFILE` is in their shell, *and* no `engine.Config.SecretsBackend` is wired, the tofu runner inherits the shell env, AWS provider reads the credentials file → works. This is the current behavior and Phase 1 doesn't break it (nil backend short-circuits).

---

## Verification (design-level)

1. **Env backend lookup.** `aws-dev` → `NIMBUSFAB_SECRET_AWS_DEV`. Unset → `(nil, nil)`. Set with valid JSON → parsed map. Set with bad JSON → error.
2. **File backend lookup.** `aws-dev` → `<dir>/aws-dev.json`. Missing → `(nil, nil)`. Present → parsed map. Present but unparseable → error.
3. **Chain fallthrough.** Both backends configured; env unset; file present → file's payload returned. Both unset → `ErrNotFound` (already implemented).
4. **Engine integration.** Apply with valid env-backed ref → `Workspace.Environment` contains the right keys; FakeRunner records them. Drift / destroy same shape.
5. **Failure path.** Apply with a ref no backend knows → operation fails fast before any tofu invocation; engine emits a clear error message naming the ref.
6. **Mixed targets.** Project with 3 targets, two have valid refs, one has a missing ref → the two valid ones succeed, the third fails. Orchestrator's existing partial-failure policy handles the rest.
7. **Nil backend.** Engine constructed with `SecretsBackend: nil` → empty env, behavior unchanged from today.
8. **Concurrent safety.** Two target operations in parallel resolve their refs independently; resolution uses per-call state (no shared cache mutation).

---

## Future hooks (not Phase 1)

- **Vault backend** — `secrets/vault.go` reading via Vault's KV v2 API.
- **Cloud KMS backends** — `secrets/awssm.go` (Secrets Manager), `secrets/azkv.go` (Key Vault), `secrets/gcpsm.go` (Secret Manager).
- **OIDC / WIF** — exchange a CI token for cloud credentials; useful for GitHub Actions / GitLab CI without long-lived secrets.
- **Adapter-side env-var translation** — `Adapter.EnvFromPayload(payload) map[string]string` so the secret material can be cloud-neutral (e.g., `{"access_key":"..."}` instead of `{"AWS_ACCESS_KEY_ID":"..."}`). Future spec.
- **Secret payload schema per cloud** — a Schema each adapter declares describing what keys it expects. Lets Phase-2 validation catch missing keys before runtime.
- **Credential pre-validation phase** — a validator that, given the project's `credentialRef`s, confirms each one resolves through the configured backend. Currently deferred so validation doesn't depend on secrets being installed.
- **Credential rotation** — backends that return short-lived tokens with expiry hints; engine refreshes mid-apply for long-running operations.
- **Per-deployment credential override** — CLI `--credential-ref aws-prod-2` flag overrides the YAML for one apply.
