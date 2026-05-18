# Azure + GCP Adapter Audit — v1.1 Design

**Status:** Subsystem spec. Closes the v1.1 gap from user-test session #1 where Azure (orders-db, uploads) and GCP (orders-db, uploads, web-app) emit tofu that doesn't pass `tofu validate` against current provider versions. Introduces a typed-attribute model for framework tags vs. labels, per-cloud cloud-API name helpers, and removes a stale `data.terraform_remote_state` fallback in AWS that survived Cross-Component Planning Phase 1.

**Date:** 2026-05-18

**Depends on:**
- `docs/superpowers/specs/2026-05-17-cross-component-planning-design.md` — Phase 1's `upstream.VarName` is the canonical interpolation syntax that AWS housekeeping migrates to.
- `docs/superpowers/specs/2026-05-16-azure-adapter-design.md` — original azurerm v4 pinning + per-type emit.
- `docs/superpowers/specs/2026-05-16-gcp-adapter-design.md` — original google v7 pinning + per-type emit; the "GCP labels strategy" deferred there is the work this spec lands.

**Depended on by:**
- All future multi-cloud features (real Azure / GCP apply paths) require this baseline to be green.

---

## Context

User-test session #1 (2026-05-16) ran real `tofu validate` against the full-stack-project fixture for the first time. Result:
- **AWS:** 4/4 targets pass.
- **Azure:** 2/4 targets pass (`web-network`, `web-app`). `orders-db`, `uploads` fail.
- **GCP:** 1/4 targets pass (`web-network`). `web-app`, `orders-db`, `uploads` fail.

Session #2 narrowed the Azure failures and unblocked `web-app`. The remaining 5 failures are real schema drift between what the adapters emit and what the current azurerm v4 / google v7 provider schemas accept.

Two adjacent issues are bundled because addressing the audit without them leaves the codebase in a half-done state:

1. **Stale `data.terraform_remote_state` fallbacks in AWS** — `internal/cloud/aws/database.go:154` and `compute.go:59,71` still contain fallback strings using the pre-v1.1 `data.terraform_remote_state.<comp>.outputs.*` syntax. Cross-Component Planning Phase 1 removed those blocks from the workspace, so any code path that hits the fallback would emit invalid tofu. The validator+preflight make this unreachable in practice, but defensive cleanup belongs with the rest of the schema-correctness work.

2. **GCP labels vs. tags** — GCP resources accept `labels`, not `tags`. The current `ir.ResourcePrimitive.NoTags bool` is binary (tag-or-skip), so GCP resources are flagged `NoTags=true` everywhere — losing framework attribution on the cloud side. A three-state field unlocks `labels` and the GCP audit can land properly tagged resources.

## Design principles

1. **One PR, full audit.** Five schema fixes + naming helpers + tag-attribute migration + AWS housekeeping. Single integration-test green bar.
2. **Test-led.** `TestFullStack_TofuValidate` is the success metric. The spec sets up the infrastructure (TagAttribute, naming helpers); the per-resource schema fixes follow the real-tofu diagnostics, not speculation.
3. **No new product surface.** Tag attribute is internal-only (an IR struct field). Naming helpers are per-cloud package functions. No CLI flags, no API changes, no schema migrations.
4. **MariaDB is removed, not workarounded.** azurerm v4 dropped `azurerm_mariadb_server`. The Azure database emit drops it; users requesting `engine: mariadb` on Azure get a clear "unsupported" error rather than emit-and-pray.
5. **AWS stale fallbacks fail-loud, not silent.** When `buildResolvedRefs` doesn't carry a ref (which the validator + preflight should prevent), AWS database/compute emit returns an error instead of emitting placeholder `data.terraform_remote_state.*` strings.

## Architecture

### `ir.ResourcePrimitive.TagAttribute`

Replace `NoTags bool` with `TagAttribute string`. Three valid values:

| Value | Meaning | Used by |
|-------|---------|---------|
| `""` | Use the per-cloud default | AWS / Azure resources that take tags |
| `"tags"` | Emit `tags = {...}` | Same as default for AWS / Azure |
| `"labels"` | Emit `labels = {...}` with GCP-sanitized values | GCP resources that accept labels |
| `""` + leave `Tags` map nil | Skip — equivalent to old `NoTags=true` | Resources that reject tags entirely |

The `TagAttribute == ""` case behaves as:
- If the primitive's cloud is AWS or Azure → default to `"tags"`.
- If the primitive's cloud is GCP → default to `""` (skip). GCP requires explicit `TagAttribute: "labels"` to opt in. This preserves the current "no tags on GCP resources" behavior for any resource the audit doesn't explicitly flip.

The skip behavior — `TagAttribute == ""` AND no value injected from `Tags` — applies to resources that genuinely reject any tag/label attribute (AWS S3 sub-resources, AWS routes, route table associations, Azure subnets in some versions, GCP networks/subnetworks/firewalls).

Existing `NoTags: true` sites map mechanically to `TagAttribute: ""`. The `Tags` field on the primitive stays (it stores the per-resource override map before injection); the renderer reads `TagAttribute`, computes the final attribute map (framework + per-resource), and writes it to `Attributes[<attr>]`.

### `injectFrameworkTags` rewrite

Lives in `pkg/provisioner/plan.go` next to its existing call site. Pseudocode:

```go
func injectFrameworkTags(p ir.ResourcePrimitive, ctx tagContext) ir.ResourcePrimitive {
    attr := resolveTagAttribute(p)  // see "TagAttribute resolution"
    if attr == "" {
        return p  // skip
    }
    fw := frameworkTags(ctx.Component, ctx.DeploymentID, ctx.OrgID)
    merged := merge(fw, p.Tags)  // per-resource Tags override framework
    if attr == "labels" {
        merged = sanitizeForLabels(merged)
    }
    if p.Attributes == nil {
        p.Attributes = map[string]any{}
    }
    p.Attributes[attr] = merged
    p.Tags = nil
    return p
}

func resolveTagAttribute(p ir.ResourcePrimitive) string {
    if p.TagAttribute != "" {
        return p.TagAttribute
    }
    if p.Cloud == "gcp" {
        return ""  // GCP requires explicit opt-in
    }
    return "tags"
}

func sanitizeForLabels(m map[string]string) map[string]string {
    // GCP label rules: lowercase keys + values, [a-z0-9_-] only, keys <=63 chars.
    out := map[string]string{}
    re := regexp.MustCompile(`[^a-z0-9_-]`)
    for k, v := range m {
        kk := re.ReplaceAllString(strings.ToLower(k), "_")
        if len(kk) > 63 { kk = kk[:63] }
        vv := re.ReplaceAllString(strings.ToLower(v), "_")
        if len(vv) > 63 { vv = vv[:63] }
        out[kk] = vv
    }
    return out
}
```

GCP framework label keys land as `infra_component`, `infra_deployment_id`, `infra_org_id` after sanitization (`:` → `_`).

### Per-cloud naming helpers

**`internal/cloud/azure/naming.go` (new):**

```go
// azureStorageAccountName: lowercase alphanum only, 3-24 chars. The Azure
// SA namespace is globally unique; component names alone aren't suitable.
// We append the first 12 chars of the deployment ID to disambiguate.
func azureStorageAccountName(component, deploymentID string) string

// azureCloudResourceName: lowercase alphanum + hyphens, 1-80 chars. Used
// for Cloud SQL flexible server names and similar.
func azureCloudResourceName(component string) string
```

**`internal/cloud/gcp/naming.go` (new):**

```go
// gcpResourceName: DNS-compliant — lowercase alphanum + hyphens, 3-63
// chars, must start with lowercase letter, no trailing hyphen. Used for
// GCS buckets and Cloud SQL instance names. Bucket names ARE global so
// we append the deployment-id prefix to disambiguate.
func gcpResourceName(component, deploymentID string) string
```

Adapters use these for cloud-API name attributes; `tofuIdentifier` (existing) continues to drive tofu local names. Same pattern as AWS where `awsResourceName` already exists.

### Schema reconciliation (test-led)

The 5 failing cells:

**Azure database (`azurerm_postgresql_flexible_server`, `azurerm_mysql_flexible_server`):**
- Replace `administrator_login` / `administrator_password` with the v4 `administrator_login` / `administrator_password` pair under the correct `administrator` field name (the schema changed locations between v3 and v4).
- Replace `zone` with `zone = "1"` for single-AZ deployments (no high_availability block in v1).
- Storage block: `storage_mb` (PG) / `storage` (MySQL has different shape).
- `sku_name` mapping: refresh against v4 valid SKUs (`B_Standard_B1ms`, `B_Standard_B2s`, etc.).
- Drop `azurerm_mariadb_server` emit entirely. When `engine: mariadb` is requested on Azure, the adapter returns an error during Emit (`cloud.UnsupportedConfigError` or similar typed error). The validator catches this earlier as a Phase 5 reasonability check if we wire it; otherwise it surfaces at plan.

**Azure storage (`azurerm_storage_account`):**
- Name via `azureStorageAccountName(component, deploymentID)`.
- `account_replication_type = "LRS"` (or per-spec override).
- `allow_nested_items_to_be_public = false` (replaces v3 `allow_blob_public_access`).
- `network_rules` block removed from default emit (defaults are sensible).

**GCP database (`google_sql_database_instance`):**
- `settings.tier` mapping refresh against current docs.
- `settings.deletion_protection` bool field (was `deletion_protection_enabled` in older versions).
- `settings.database_version` enum names: `POSTGRES_15`, `MYSQL_8_0` (not the older underscore patterns).
- Name via `gcpResourceName(component, deploymentID)`.
- `TagAttribute: "labels"`.

**GCP storage (`google_storage_bucket`):**
- Name via `gcpResourceName(component, deploymentID)`.
- `force_destroy = false` explicit.
- `public_access_prevention = "enforced"`.
- `uniform_bucket_level_access = true`.
- `TagAttribute: "labels"`.

**GCP compute (`google_compute_instance`):**
- `network_interface` block requires `access_config = {}` for public IP, OR omit it entirely for internal-only. Default: omit (internal only; users add `publicIP: true` to opt in — out of scope for this PR).
- `network` references `google_compute_network.<name>.self_link` (not `.id`).
- `subnetwork` references `google_compute_subnetwork.<name>.self_link`.
- `boot_disk.initialize_params.image` references match GCP image naming.
- `TagAttribute: "labels"`.

### AWS housekeeping

In `internal/cloud/aws/database.go` and `internal/cloud/aws/compute.go`:

- Find the `stringFromRefs(refs, "<alias>", "<fallback>")` calls where the fallback is a literal `data.terraform_remote_state.*.outputs.*` string. Change behavior: when the ref isn't present, return a typed error from `Emit` (e.g., `fmt.Errorf("aws.%s: required ref %q missing", componentType, alias)`).
- The validator+preflight already make this unreachable in normal flow. Failing-loud is a regression net for future refactors that might break the ref-resolution chain.

The `subnetIDsFromRefs` helper in `internal/cloud/aws/database.go:135-156` similarly currently falls back to `data.terraform_remote_state...` — same treatment.

## Components

### Files created

- `internal/cloud/azure/naming.go` + `naming_test.go`
- `internal/cloud/gcp/naming.go` + `naming_test.go`

### Files modified

- `pkg/ir/types.go` — `ResourcePrimitive.NoTags` → `TagAttribute`. Adjust JSON tag.
- `pkg/provisioner/plan.go` — `injectFrameworkTags` rewrite + helpers (`resolveTagAttribute`, `sanitizeForLabels`).
- `internal/cloud/aws/*.go` — mechanical `NoTags: true` → `TagAttribute: ""` migration; AWS housekeeping in `database.go` + `compute.go`.
- `internal/cloud/azure/*.go` — mechanical NoTags migration; new schemas in `database.go` + `storage.go`; integrate `azureStorageAccountName` + `azureCloudResourceName`.
- `internal/cloud/gcp/*.go` — mechanical NoTags migration; tag attribute flips to "labels" on `compute`, `database`, `storage` resources; new schemas in `compute.go` + `database.go` + `storage.go`; integrate `gcpResourceName`.
- `pkg/provisioner/workspace.go` — no change (it reads `p.Attributes` after injection — already cloud-attribute-agnostic).

### Tests touched

- `pkg/ir/types_test.go` — if a test exercises NoTags JSON shape, update to TagAttribute.
- All adapter `*_test.go` files in `internal/cloud/{aws,azure,gcp}/` — fix golden/expected attribute names and tag-vs-label assertions.
- `internal/cloud/azure/naming_test.go` (new).
- `internal/cloud/gcp/naming_test.go` (new).
- `cmd/cli/integration_validate_test.go` — extend `TestFullStack_TofuValidate` to require all 12 targets (currently AWS-only). Or rather: drop the AWS-only filtering and assert the full count.

## Data flow

No new data flow. The IR shape evolves (`NoTags` → `TagAttribute`), but persistence (`ir_json` in inventory) accepts both during JSON round-trip because the field is internal-only and only populated by adapters (not by user YAML). Existing inventory rows persisted with old `noTags: true` JSON survive decode (Go's JSON unmarshal silently drops unknown fields; the `noTags` value becomes irrelevant once adapters re-emit on the next plan).

## Error handling

| Condition | Behavior |
|-----------|----------|
| Azure database emit with `engine: mariadb` | Adapter returns typed error from `Emit`; plan fails with a clear message naming the unsupported engine. |
| AWS database/compute Emit when a required ref isn't in `refs` map | Adapter returns typed error from `Emit`; plan fails with the missing alias name. |
| Tag attribute resolution for an unknown cloud value | Defaults to `"tags"` (AWS/Azure convention). Doesn't error — out-of-tree adapters can still skip via explicit `TagAttribute: ""`. |
| Label sanitization produces an empty key (all chars rejected) | The empty key is dropped silently from the map. Logged at debug; framework keys (`infra_component` etc.) are always valid post-sanitization. |

## Testing strategy

1. **Unit tests** for `azureResourceName`, `gcpResourceName` covering: too-short names, too-long names, special characters, leading digit, deployment-id prefix length.
2. **Unit tests** for `sanitizeForLabels`: colon replacement, uppercase folding, 63-char truncation.
3. **Adapter tests** (per cloud × per component type) get updated assertions matching the new schema.
4. **Integration test** `TestFullStack_TofuValidate` becomes the green-bar gate:
   - Drops the AWS-only filter / skip.
   - Asserts all 12 targets pass `tofu init` + `tofu validate`.
   - Runs in the existing `-tags=integration` flow; requires `tofu` on PATH.
5. **Adapter migration** is mechanical (every `NoTags: true` → `TagAttribute: ""`); the existing per-adapter unit tests catch regressions automatically.

## Non-goals (deferred)

- **Real-credential Azure / GCP apply.** This audit makes `tofu validate` + `tofu plan` succeed; full `apply` against real clouds is a separate user-test exercise.
- **MariaDB on Azure.** Removed in azurerm v4; users wanting MariaDB-class workloads use Azure MySQL flexible server or move to AWS.
- **Public IP on GCP compute.** Default emit is internal-only; opt-in spec field is deferred.
- **Cross-cloud / cross-region refs.** Already deferred to v1.2 per cross-component-planning spec.
- **Schema generation tooling.** This audit is hand-reconciled per-resource; a future tool could generate adapter shapes from provider docs but is its own project.

## Implementation phasing

Single phase: **Adapter Audit Phase 1.**

Estimated implementation tasks (~14):

1. `ir.ResourcePrimitive.TagAttribute` field + JSON tag; `NoTags` deleted from struct.
2. `pkg/provisioner/plan.go` — `injectFrameworkTags` rewrite, `resolveTagAttribute`, `sanitizeForLabels`; tests.
3. Mechanical `NoTags: true` → `TagAttribute: ""` across all AWS / Azure / GCP emit files. No test changes yet (all still pass).
4. `internal/cloud/azure/naming.go` + tests (azureStorageAccountName, azureCloudResourceName).
5. `internal/cloud/gcp/naming.go` + tests (gcpResourceName).
6. AWS housekeeping: replace fallback strings in `database.go` + `compute.go` with typed errors; update tests.
7. Azure storage rewrite: `azurerm_storage_account` schema + naming helper; tests.
8. Azure database rewrite: `azurerm_postgresql_flexible_server` schema; tests.
9. Azure database: `azurerm_mysql_flexible_server` schema; tests.
10. Azure database: drop `azurerm_mariadb_server`; emit returns typed error on `engine: mariadb`; tests.
11. GCP storage rewrite: `google_storage_bucket` schema + labels + naming helper; tests.
12. GCP database rewrite: `google_sql_database_instance` schema + labels; tests.
13. GCP compute rewrite: `google_compute_instance` schema + labels; tests.
14. Integration test: extend `TestFullStack_TofuValidate` to require all 12 targets; verify green.

## Open questions

None blocking. Possible follow-ups surfaced:
- Should `engine: mariadb` on Azure produce a validator Phase 5 error rather than an Emit error? Cleaner UX; out of scope here.
- GCP `google_compute_instance` public-IP opt-in — should this be the next small enhancement after the audit lands?
