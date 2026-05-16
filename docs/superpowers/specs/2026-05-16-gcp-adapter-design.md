# GCP Adapter Subsystem Spec

**Status:** Subsystem spec. Defines the GCP cloud-adapter implementation for the four v1 component types (`network`, `compute`, `database`, `storage`). Mirrors the structure of `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` and `docs/superpowers/specs/2026-05-16-azure-adapter-design.md` with GCP-specific primitives, T-shirt size mappings, PricingKey shapes, and Profile data.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (cloud adapter contract)
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (Adapter interface methods)
- `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` (sibling design)
- `docs/superpowers/specs/2026-05-16-azure-adapter-design.md` (sibling design — GCP mirrors the same shape)
- `docs/superpowers/specs/2026-05-15-parity-design.md` (`Profile()` returns parity-engine-shaped data)
- `docs/superpowers/specs/2026-05-16-cost-estimator-design.md` (`PricingKey()` returns cost-estimator-shaped data)

**Depended on by:**
- Parity Engine — turns the 2-way AWS/Azure parity report into a 3-way comparison; surfaces patterns invisible with two clouds (e.g., AWS and GCP both pick 2 GiB RAM for "small" while Azure picks 4 — easier to see the outlier with three).
- Cost Estimator — activates 3-way cost comparison; users see AWS / Azure / GCP side-by-side for the same component.

---

## Context

AWS Phase 3 and Azure Phase 4 implemented the full adapter contract — `Emit` dispatching on `target.Spec["__type"]`, per-type primitive emission with sane defaults, T-shirt size resolution, `PricingKey` + `Profile` populated, contract test suite passing. With two clouds live, parity reports and cost estimates show meaningful divergence. A third cloud strengthens both: parity rules that depend on weighted-average behavior across N targets only become non-trivial at N≥3; cost comparison gets a third reference price to triangulate against.

This spec lands the GCP equivalent. The shape mirrors the AWS/Azure adapters 1:1 — same four `emit*` files, same `Validate / TierOneSchema / DefaultStateBackend / ProviderBlock` surfaces — but with GCP resource types, region names, and SKU choices. After this lands:
- A user can write `targets: [{cloud: aws, region: us-east-1}, {cloud: azure, region: eastus}, {cloud: gcp, region: us-central1}]` and get real per-cloud apply.
- Parity score reflects 3-way SKU divergence (AWS picks t3.small / Azure picks B2s / GCP picks e2-small; memory differs across all three; weighted score reflects that).
- Cost estimates show three per-cloud subtotals.
- The `nimbusfab parity` and `nimbusfab cost estimate` commands show 3-way data.

**Design principles:**
1. **Mirror AWS Phase 3 / Azure Phase 4.** Same file layout, same dispatch pattern, same out-of-scope deferrals. Reviewer sees "GCP equivalent of `internal/cloud/aws/emit.go`" and the structure clicks.
2. **GCP quirks are documented, not abstracted.** GCP has projects (mandatory at the provider level, not per-resource like Azure RGs), region/zone separation (similar to AWS but explicit), and Cloud SQL's distinct provisioning model. These show up in emit code with comments; the adapter doesn't try to make GCP look like AWS or Azure.
3. **Project ID flows through provider config.** Unlike Azure RGs (per-component, deterministic), the GCP project ID is a tenant-scope identifier configured once at the provider level. The adapter reads project ID from `target.Spec["project"]` if present, falls back to environment variable `GOOGLE_PROJECT`, never invents one.
4. **`hashicorp/google` provider, not `google-beta`.** Phase 5 uses the GA provider exclusively. Beta resources deferred to v2.
5. **Stable resource naming.** All GCP resource names are derived from `component` + `region` so multiple deploys produce the same names. GCP has 1-63 char limits and lowercase-alphanumeric-hyphen rules; the adapter sanitizes.
6. **Bucket names are globally unique.** GCS buckets share a global namespace; the adapter derives `<project>-<component>-<region>-<hash>` to minimize collision risk. User can override via `spec.name`.

---

## Scope

**In scope (this spec):**
- `internal/cloud/gcp` package matching AWS-Phase-3 / Azure-Phase-4 layout: `adapter.go`, `emit.go`, `network.go`, `compute.go`, `database.go`, `storage.go`, `pricing.go`, `profile.go`, `stubs.go`, `schema/v1alpha1/tier_one.json`, `testdata/`.
- Per-type primitive emission:
  - `network` → `google_compute_network` (custom-subnetwork mode), `google_compute_subnetwork` × N (one per AZ-equivalent; GCP zones are letter suffixes per region: `us-central1-a`, `-b`, etc.), `google_compute_firewall` with default rules (allow internal, deny external ingress).
  - `compute` → `google_compute_firewall` (default egress allow + deny-all ingress), `google_compute_instance` × N replicas with boot disk.
  - `database` → `google_sql_database_instance` (for postgres/mysql) + `google_sql_database` for default DB. MariaDB unsupported on Cloud SQL — adapter returns `ErrAdapterGCPMariaDBUnsupported`.
  - `storage` → `google_storage_bucket` (single resource — GCS has no "container" concept; the bucket IS the container).
- T-shirt size tables for compute (e2-small through e2-standard-4 / n2-standard-4) and database (db-f1-micro through db-custom-4-16384).
- `PricingKey()` shapes matching GCP Cloud Billing Catalog SKU descriptor fields.
- `Profile()` returning the same `parity.ResourceProfile` shape AWS/Azure use, populated with GCP SKU values (memory, vCPU, storage class).
- `gcp.DefaultStateBackend` returns `gcs` backend pointing at a configured bucket (defaults to `nimbusfab-state` bucket, prefix derived from project/stack).
- `gcp.ProviderBlock` returns the `google` provider configuration with `project` and `region` (no `zone` — zone-scoped resources specify zones individually).
- Cost snapshot rows for the GCP SKUs Phase 5 emits.
- Region validation: `gcp.Validate` rejects AWS/Azure-style names; valid GCP regions match `^[a-z]+-[a-z]+[0-9]$` (e.g., `us-central1`, `europe-west4`).
- Updates to the full-stack fixture to include `gcp/us-central1` targets so the 3-way parity + cost demos light up.
- Contract test suite scenarios pass.
- `defaultCloudRegistry()` in `cmd/cli/clouds.go` registers GCP alongside AWS + Azure.

**Out of scope (deferred):**
- `google-beta` provider resources (Confidential VMs, GKE Autopilot features, etc.). v2.
- Service Accounts as managed resources (provider-level auth is in scope; per-resource SAs deferred).
- Cloud Load Balancing, Cloud CDN, Cloud Armor. v2.
- BigQuery, Spanner, Firestore, Bigtable. The architecture spec covers these in later sub-specs.
- GKE / Cloud Run / App Engine. These are application-tier; out of v1.
- VPC peering, Cloud Interconnect, Cloud VPN. v2.
- Committed Use Discounts / Sustained Use Discounts. v2 (consistent with AWS Reserved / Azure Hybrid Benefit deferrals).
- Cloud KMS, Secret Manager. Web app + secrets phases.
- Tier-1 `<cloud>:` escape hatch schemas for GCP-specific fields. Same deferral as AWS Phase 3 / Azure Phase 4.
- Emulator integration tests (gcloud has emulators for Datastore/Pub/Sub/Spanner; no Compute emulator). Integration tests skip when no GCP creds.

---

## Per-type primitive emissions

### `network`

**Inputs:** `spec.cidr` (default `10.0.0.0/16`), optional `spec.subnetCount` (default 3), optional `spec.enableIPv6` (default false).

**Primitives:**
1. `google_compute_network.<component>` with `auto_create_subnetworks = false` (custom-mode VPC; we manage subnets explicitly).
2. `google_compute_subnetwork.<component>_<i>` for `i` in `0..subnetCount-1`, /24 slices of `cidr`, each `region = target.Region` (subnetworks are regional in GCP — they span all zones of a region, unlike AWS subnets which are per-AZ).
3. `google_compute_firewall.<component>_internal` allowing intra-VPC traffic on all protocols.
4. `google_compute_firewall.<component>_deny_external` denying external ingress by default.

No GCP equivalent of the AWS internet gateway is emitted — GCP instances with external IPs route to the internet automatically; firewall rules gate access. No route tables either; GCP provides default routes per VPC.

**Note on subnetworks:** GCP subnetworks are *regional* (cover all zones), not zonal. The `subnetCount` parameter still creates N subnets but they share the region — the typical reason to have multiple is for separate IP ranges (e.g., one for compute, one for managed services).

### `compute`

**Inputs:** `spec.size` OR `spec.compute`, optional `spec.replicas` (default 1), optional `spec.storage.sizeGB` (default 30), optional `spec.imageRef` (default: latest Ubuntu 22.04 LTS from `ubuntu-os-cloud`), optional `spec.zone` (default: `<region>-a`).

**Primitives:**
1. `google_compute_firewall.<component>_egress` (default: allow all egress; deny-all-ingress is in network/firewall).
2. `google_compute_instance.<component>_<i>` per replica, `zone = <region>-{a,b,c}[i%3]` round-robin for availability.

T-shirt → GCP machine type (closest equivalent to AWS Phase 3 t3.* sizing):
| Size | GCP machine type | vCPU | RAM (GiB) |
|---|---|---|---|
| small | e2-small | 2 (shared) | 2 |
| medium | e2-medium | 2 (shared) | 4 |
| large | e2-standard-2 | 2 | 8 |
| xlarge | n2-standard-4 | 4 | 16 |

(Note: e2 is GCP's general-purpose cost-optimized family — closest to AWS t3 burstable and Azure B-series. n2 is general-purpose performance, similar to AWS m6i / Azure D_v5. The small/medium intentionally cross-cloud-compare with AWS t3.small/t3.medium and Azure B2s/B2ms for parity engine; the parity score reflects divergence — that's the point. Notably, AWS small (2 GiB) and GCP small (2 GiB) match, while Azure B2s (4 GiB) is the outlier; large flips — GCP standard-2 (8 GiB) matches Azure B4ms (16 GiB) at vCPU/RAM ratio level. These are the patterns we want the parity engine to surface.)

Default image: `ubuntu-os-cloud/ubuntu-2204-lts`. GCP image references are family + project; the same family works across all regions.

### `database`

**Inputs:** `spec.engine` (postgres / mysql; mariadb is unsupported), optional `spec.version`, `spec.size` OR `spec.compute`+`spec.storage`, optional `spec.multiAZ` (Cloud SQL equivalent: `availability_type = "REGIONAL"`; default ZONAL).

**Primitives:**
- `google_sql_database_instance.<component>` (database_version = `POSTGRES_16` / `MYSQL_8_0`; settings block configures tier, disk, availability).
- `google_sql_database.<component>_default` (default database within the instance).

T-shirt → Cloud SQL tier:
| Size | Cloud SQL tier | vCPU | RAM (GiB) | Storage default (GiB) |
|---|---|---|---|---|
| small | db-f1-micro | shared | 0.6 | 10 |
| medium | db-g1-small | shared | 1.7 | 20 |
| large | db-custom-2-7680 | 2 | 7.5 | 100 |
| xlarge | db-custom-4-15360 | 4 | 15 | 200 |

Engine version defaults: postgres `POSTGRES_16`, mysql `MYSQL_8_0`. MariaDB → `ErrAdapterGCPMariaDBUnsupported` (Cloud SQL does not offer MariaDB; this is a documented limitation that surfaces a parity-engine "engine-divergence" rule violation, which is the correct behavior).

`availability_type = "ZONAL"` by default; `"REGIONAL"` when `multiAZ: true` (provides high-availability replica in a different zone within the same region).

### `storage`

**Inputs:** optional `spec.name` (default: deterministic from project+component+region), optional `spec.versioning` (default true → `versioning { enabled = true }`), optional `spec.publicAccess` (default blocked → `public_access_prevention = "enforced"` + uniform bucket-level access enabled).

**Primitives:**
1. `google_storage_bucket.<component>` with `location = <region>` (lowercased), `storage_class = "STANDARD"`, `uniform_bucket_level_access = true`, `public_access_prevention = "enforced"`, `versioning { enabled = true }`.

Bucket name has GCS-specific constraints: 3-63 characters, lowercase letters / digits / hyphens / dots only, must start and end with letter or digit, globally unique. The adapter derives a safe name: `<project_short>-<component>-<region>-<sha6>` where each part is sanitized to lowercase alphanumerics + hyphens.

GCS has no "container" sub-resource — the bucket IS the container. Phase 5 emits exactly one primitive per storage component (compared to AWS's 4-primitive bucket + versioning + public-access-block + SSE, and Azure's 3-primitive RG + Account + Container). This is a real cross-cloud divergence; the parity engine reports `storage` profile equivalence per Class but the underlying primitive counts differ.

---

## Adapter Validate behavior

`gcp.Adapter.Validate(target)` checks:
- `target.Region` is non-empty and matches GCP region naming (e.g., `us-central1`, `europe-west4`). Regex: `^[a-z]+-[a-z]+[0-9]$`. Rejects AWS (`us-east-1`) and Azure (`eastus`) formats with `ErrAdapterGCPRegionInvalid`.
- For databases: `spec.engine` is one of `postgres` / `mysql`; `mariadb` → `ErrAdapterGCPMariaDBUnsupported`.
- For storage: derived bucket name (if user didn't override) is ≤63 chars.

---

## PricingKey shapes

GCP Cloud Billing Catalog API uses SKU descriptors keyed by service name + resource group + region. PricingKey shape matches the canonical "SKU description fields" the snapshot indexes by.

### VM
```json
{
  "service": "Compute Engine",
  "resourceFamily": "Compute",
  "resourceGroup": "E2",
  "usageType": "OnDemand",
  "machineType": "e2-small",
  "region": "us-central1"
}
```

(For n2 machines: `resourceGroup = "N2"`.)

### Cloud SQL Instance
```json
{
  "service": "Cloud SQL",
  "resourceFamily": "ApplicationServices",
  "resourceGroup": "SQLGen2InstancesF1Micro",
  "usageType": "OnDemand",
  "tier": "db-f1-micro",
  "region": "us-central1",
  "engine": "POSTGRES"
}
```

(For custom tiers: `resourceGroup = "SQLGen2InstancesCustom"`. For MySQL: `engine = "MYSQL"`.)

### Storage Bucket
```json
{
  "service": "Cloud Storage",
  "resourceFamily": "Storage",
  "resourceGroup": "StandardStorage",
  "usageType": "OnDemand",
  "storageClass": "STANDARD",
  "region": "us-central1"
}
```

### Free primitives

`google_compute_network`, `google_compute_subnetwork`, `google_compute_firewall`, `google_sql_database` (the per-DB child resource — the instance is billed): all return `(nil, nil)` from PricingKey.

---

## Profile shapes

Same `parity.ResourceProfile` shape AWS/Azure use, populated with GCP equivalents:

- VM (`google_compute_instance`): `Class = "compute"`, `Compute.VCPU/MemoryGB/Architecture` from the machine-type table.
- Cloud SQL instance (`google_sql_database_instance`): `Class = "database"`, `Database.{Engine, Version, Compute, Storage}` + features map (e.g., `regional_availability` if multiAZ).
- Bucket (`google_storage_bucket`): `Class = "storage"`, `Storage.Class = "object"`.
- Network / subnetwork / firewall / SQL database (child): `ErrProfileUnavailable`.

---

## DefaultStateBackend

```go
ir.StateBackend{
    Kind: "gcs",
    Config: map[string]any{
        "bucket": "nimbusfab-state",
        "prefix": fmt.Sprintf("gcp/%s", target.Region),
    },
}
```

Users override per stack via `project.yaml` `stateBackend:` block, same pattern as AWS / Azure.

---

## ProviderBlock

```go
map[string]any{
    "google": map[string]any{
        "project": "<from-target-spec-or-env>",
        "region":  target.Region,
    },
}
```

If `target.Spec["project"]` is set, use it. Otherwise the provider relies on `GOOGLE_PROJECT` env var at runtime (the adapter does not block emit on missing project — that's a runtime concern surfaced by `tofu plan`).

Authentication: credentials flow through env vars (`GOOGLE_APPLICATION_CREDENTIALS` pointing at a service-account JSON file, OR `GOOGLE_CREDENTIALS` containing the JSON itself), never embedded in the provider block. The secrets backend resolves the credentialRef and the provisioner stuffs them into the runner's environment.

---

## Region / Location mapping

GCP regions use yet another naming convention:
| AWS | Azure | GCP |
|---|---|---|
| us-east-1 | eastus | us-east1 |
| us-east-2 | eastus2 | us-east4 |
| us-west-2 | westus2 | us-west1 |
| eu-west-1 | westeurope | europe-west1 |
| ap-southeast-2 | australiaeast | australia-southeast1 |

Users specify GCP regions directly in YAML — no automatic translation. Three-cloud projects look like:

```yaml
targets:
  - cloud: aws
    region: us-east-1
  - cloud: azure
    region: eastus
  - cloud: gcp
    region: us-central1
```

The adapter rejects non-GCP region formats via Validate.

---

## Pricing snapshot additions

`pkg/cost/pricing/snapshot/gcp.json` (new file) covers the Phase-5 emissions:

| Service | Coverage |
|---|---|
| Compute Engine | e2-small, e2-medium, e2-standard-2, n2-standard-4 × {us-central1, us-east1, europe-west1} × Linux |
| Cloud SQL | db-f1-micro, db-g1-small, db-custom-2-7680, db-custom-4-15360 × same regions × ZONAL / REGIONAL |
| Cloud Storage | STANDARD class × same regions |

Snapshot row count: ~50 entries (similar to Azure snapshot). Manually curated; same refresh process documented in `pkg/cost/pricing/snapshot/README.md`.

---

## Contract test suite

`pkg/plugin/contract.RunProvisionerScenarios` already runs against any `cloud.Adapter`. Phase 5 adds a `gcp_test.go` in the plugin/contract package that runs the suite against `gcp.New()`:

```go
func TestGCPAdapter_ProvisionerContract(t *testing.T) {
    sample := ir.DeploymentTarget{
        Cloud:  "gcp",
        Region: "us-central1",
        Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web", "__type": "network"},
    }
    contract.RunProvisionerScenarios(t, gcp.New(), sample)
}
```

All seven contract scenarios pass.

---

## Full-stack fixture update

Add `gcp/us-central1` targets to the existing `cmd/cli/testdata/full-stack-project/components/*.yaml` so the fixture exercises all three clouds. After this lands:
- `nimbusfab plan --stack dev cmd/cli/testdata/full-stack-project` shows 12 targets (4 components × 3 clouds).
- `nimbusfab parity` reports 3-way scores per component (weighted average across all three target SKUs).
- `nimbusfab cost estimate` shows three per-cloud subtotals.

---

## CLI registration

`cmd/cli/clouds.go` `defaultCloudRegistry()` extended to register GCP:

```go
func defaultCloudRegistry() (cloud.Registry, error) {
    reg := cloud.NewRegistry()
    if err := reg.Register(aws.New()); err != nil { return nil, err }
    if err := reg.Register(azure.New()); err != nil { return nil, err }
    if err := reg.Register(gcp.New()); err != nil { return nil, err }
    return reg, nil
}
```

One additional edit; all 6 CLI production files automatically pick up GCP support.

---

## Cost estimator usage extension

`pkg/cost/estimator/usage.go` `UnitsFor` extended to recognize GCP Tofu types:

```go
case "aws_instance", "aws_db_instance",
    "azurerm_linux_virtual_machine",
    "azurerm_postgresql_flexible_server",
    "azurerm_mysql_flexible_server",
    "azurerm_mariadb_server",
    "google_compute_instance",
    "google_sql_database_instance":
    // 730 hrs/month default
case "aws_s3_bucket", "azurerm_storage_account", "google_storage_bucket":
    // 100 GB-Mo default
```

---

## Error model additions

| Code | Origin | Meaning |
|---|---|---|
| `ErrAdapterGCPRegionInvalid` | `gcp.Validate` | `target.Region` empty or uses AWS/Azure format |
| `ErrAdapterGCPUnsupportedType` | `gcp.Emit` | `target.Spec["__type"]` not in the four v1 types |
| `ErrAdapterGCPUnsupportedEngine` | `gcp.emitDatabase` | `spec.engine` not postgres/mysql |
| `ErrAdapterGCPMariaDBUnsupported` | `gcp.emitDatabase` | Cloud SQL doesn't offer MariaDB; explicit error |
| `ErrAdapterGCPSizeConflict` | `gcp.Validate` | Both `spec.size` and `spec.compute` set |
| `ErrAdapterGCPUnknownSize` | sizing helpers | T-shirt size not in mapping |
| `ErrAdapterGCPBucketNameInvalid` | `gcp.emitStorage` | User-provided name violates GCS constraints |

---

## Verification (design-level)

1. **Per-type walkthrough.** Hand-draft `(cidr=10.0.0.0/16, size=small)` for each of network/compute/database/storage. Walk the emit logic; confirm the primitives form a valid Tofu workspace mentally.
2. **3-way parity walkthrough.** A `compute small` component targeting AWS (`t3.small`: 2 vCPU / 2 GiB), Azure (`Standard_B2s`: 2 vCPU / 4 GiB), GCP (`e2-small`: 2 vCPU / 2 GiB). Confirm parity engine sees Azure as outlier on memory; weighted memory score reflects 2/3 agreement.
3. **3-way cost walkthrough.** Same component: AWS $15.18/mo, Azure $30.37/mo, GCP $12.23/mo (e2-small @ ~$0.01675/hr × 730). Cost estimator shows three subtotals.
4. **MariaDB rejection walkthrough.** A `database mariadb` component targeted at GCP. Validate or emit returns `ErrAdapterGCPMariaDBUnsupported` with a clear message about Cloud SQL not offering MariaDB.
5. **State backend walkthrough.** `gcp.DefaultStateBackend` returns `gcs` kind. Confirm workspace.go's existing backend serializer handles `gcs` without modification (it just JSON-serializes the backend block keyed by kind).
6. **Project ID propagation walkthrough.** A target with `spec.project = "my-project"`. Confirm `ProviderBlock` includes the project; the Tofu generated has `project = "my-project"` in the provider block.
7. **Snapshot freshness.** Same 90-day staleness warning applies to GCP snapshot as AWS/Azure.

---

## Future hooks (not Phase 5)

- **Service account management** as a separate component primitive or as part of compute/SQL profiles (currently provider-level auth only).
- **`google-beta` provider integration** for Confidential VMs, GKE Autopilot.
- **GKE / Cloud Run / App Engine** as separate component types (application tier).
- **BigQuery / Spanner / Firestore** as alternative `database` engines or new component types.
- **Multi-region buckets** (`location = "US"`) for storage HA — Phase 5 keeps regional only.
- **Committed Use Discounts / Sustained Use Discounts** as cost-model variants (v2 with AWS Reserved / Azure Hybrid Benefit).
- **Spot VMs** via `scheduling { provisioning_model = "SPOT" }` — different pricing dimension, snapshot updates.
- **Workload Identity Federation** for keyless auth from CI / external systems.
