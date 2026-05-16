# Azure Adapter Subsystem Spec

**Status:** Subsystem spec. Defines the Azure cloud-adapter implementation for the four v1 component types (`network`, `compute`, `database`, `storage`). Mirrors the structure of `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` with Azure-specific primitives, T-shirt size mappings, PricingKey shapes, and Profile data.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (cloud adapter contract)
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (Adapter interface methods)
- `docs/superpowers/specs/2026-05-16-aws-expansion-design.md` (sibling design — Azure mirrors its shape)
- `docs/superpowers/specs/2026-05-15-parity-design.md` (`Profile()` returns parity-engine-shaped data)
- `docs/superpowers/specs/2026-05-16-cost-estimator-design.md` (`PricingKey()` returns cost-estimator-shaped data)

**Depended on by:**
- Parity Engine — activates real multi-cloud parity reports (AWS Phase 3 alone produces score=1.0 trivially because every component has one target).
- Cost Estimator — activates cross-cloud cost comparison; users can see AWS vs. Azure cost for the same component.
- GCP adapter spec (Phase 5) — mirrors the same structure for the third cloud.

---

## Context

AWS Phase 3 implemented the full adapter contract — `Emit` dispatching on `target.Spec["__type"]`, per-type primitive emission with sane defaults, T-shirt size resolution, `PricingKey` + `Profile` populated, contract test suite passing. The infrastructure downstream (parity engine, cost estimator) is multi-cloud-ready but currently single-cloud-degenerate: with only AWS, every component has one target, parity reports score=1.0 trivially, and cost estimates show only AWS prices.

This spec lands the Azure equivalent. The shape mirrors AWS Phase 3 1:1 — same four `emit*` files, same `Validate / TierOneSchema / DefaultStateBackend / ProviderBlock` surfaces — but with Azure resource types, region names, and SKU choices. After this lands:
- A user can write `targets: [{cloud: aws, region: us-east-1}, {cloud: azure, region: eastus}]` and get real per-cloud apply.
- Parity score reflects actual SKU divergence (Azure picks B2ms; AWS picks t3.medium; memory differs; score reflects that).
- Cost estimates show per-cloud subtotals (Azure VM @ $X/mo + AWS EC2 @ $Y/mo).
- The `nimbusfab parity` and `nimbusfab cost estimate` commands surface meaningful cross-cloud data for the first time.

**Design principles:**
1. **Mirror AWS Phase 3.** Same file layout, same dispatch pattern, same out-of-scope deferrals. Reviewer sees "Azure equivalent of `internal/cloud/aws/emit.go`" and the structure clicks.
2. **Azure quirks are documented, not abstracted.** Azure has resource groups (no AWS equivalent), location names that differ from regions, and a richer security primitive surface (NSG separate from subnet). These show up in emit code with comments explaining why; the adapter doesn't try to make Azure look like AWS.
3. **One Resource Group per (project, target).** The adapter creates a single `azurerm_resource_group` per `(component, region)` and puts everything in it. RG name is derived deterministically: `<project>-<component>-<region>`.
4. **AzureRM provider, not AzAPI.** Phase 1 uses the mature `hashicorp/azurerm` provider exclusively. Resource types use its naming (`azurerm_virtual_network`, etc.).
5. **Stable resource naming.** All Azure resource names are derived from `component` + `region` so multiple deploys produce the same names. Azure has stricter character rules than AWS (no underscores in many resource types); the adapter sanitizes.

---

## Scope

**In scope (this spec):**
- `internal/cloud/azure` package matching AWS-Phase-3 layout: `adapter.go`, `emit.go`, `schema.go`, `network.go`, `compute.go`, `database.go`, `storage.go`, `pricing.go`, `profile.go`, `testdata/`.
- Per-type primitive emission:
  - `network` → `azurerm_resource_group`, `azurerm_virtual_network`, `azurerm_subnet` (one per "AZ" — Azure availability zones are integers 1/2/3 per region), `azurerm_network_security_group` with default rules.
  - `compute` → `azurerm_resource_group` (shared with network if same target), `azurerm_network_interface` per instance, `azurerm_network_security_group`, `azurerm_linux_virtual_machine` × N replicas.
  - `database` → `azurerm_resource_group`, `azurerm_postgresql_flexible_server` (for `engine: postgres`) OR `azurerm_mysql_flexible_server` (for `engine: mysql`). MariaDB → `azurerm_mariadb_server` (note: classic MariaDB, not Flexible — Azure deprecated MariaDB Flexible Server; this is a documented limitation).
  - `storage` → `azurerm_resource_group`, `azurerm_storage_account` (kind = StorageV2, replication = LRS by default), `azurerm_storage_container`.
- T-shirt size tables for compute (Standard_B2s through Standard_D4s_v5) and database (B_Standard_B1ms through GeneralPurpose_D4s_v3).
- `PricingKey()` shapes matching Azure Retail Prices API:
  - VM: `{"service": "VirtualMachines", "armSkuName": "Standard_B2s", "armRegionName": "eastus", "priceType": "Consumption", "productName": "Virtual Machines BS Series"}`.
  - PostgreSQL Flexible Server: `{"service": "AzureDatabaseforPostgreSQL", "skuName": "Standard_B1ms", "armRegionName": "eastus", "tier": "Burstable"}`.
  - Storage Account: `{"service": "Storage", "skuName": "Standard LRS", "armRegionName": "eastus", "tier": "Standard", "meterName": "LRS Data Stored"}`.
- `Profile()` returning the same `parity.ResourceProfile` shape AWS uses, populated with Azure SKU values (memory, vCPU, storage class).
- `azure.DefaultStateBackend` returns `azurerm` backend pointing at a configured storage account (defaults to `nimbusfab-state` storage account, `terraform-state` container, key derived from project/stack).
- `azure.ProviderBlock` returns the `azurerm` provider configuration with `features {}` block.
- Cost snapshot rows for the Azure SKUs Phase 4 emits.
- Region → location mapping (memory: `us-east-1` is AWS; the user writes `eastus` for Azure). Azure adapter `Validate` rejects unknown locations.
- Updates to the full-stack fixture to include `azure/eastus` targets so the parity + cost demos light up.
- Contract test suite scenarios pass.

**Out of scope (deferred):**
- AzAPI provider (advanced resource types not yet wrapped by AzureRM). v2.
- Managed identities, RBAC role assignments. v2 (compute uses system-assigned identity if any; default is no identity).
- Application Gateway, Front Door, Traffic Manager. v2.
- Azure SQL Database / Cosmos DB / Synapse. The architecture spec covers these in later sub-specs.
- Azure Storage lifecycle management (tiers, immutability). v2.
- Network peering, ExpressRoute, VPN Gateway. v2.
- Reserved instances / Savings Plans / Hybrid Benefit. v2 (consistent with AWS Phase 3).
- Azure DevOps integration, Key Vault. Web app + secrets phases.
- Tier-1 `<cloud>:` escape hatch schemas for Azure-specific fields. Same deferral as AWS Phase 3.
- LocalStack-equivalent (Azurite for storage, no equivalent for compute/DB). Integration tests skip when no Azure creds.

---

## Per-type primitive emissions

### `network`

**Inputs:** `spec.cidr` (default `10.0.0.0/16`), optional `spec.subnetCount` (default 3), optional `spec.enableIPv6` (default false; Azure IPv6 has caveats and is reserved).

**Primitives:**
1. `azurerm_resource_group.<component>` (one per (component, region) tuple)
2. `azurerm_virtual_network.<component>` with `address_space = [cidr]`
3. `azurerm_subnet.<component>_<i>` for `i` in `0..subnetCount-1`, /24 slices of `cidr`
4. `azurerm_network_security_group.<component>` with default rules (allow egress; no ingress)

No Azure equivalent of the AWS internet gateway is emitted by default — Azure VMs route to the internet automatically if their NSG allows it, but inbound connectivity requires a public IP per VM (handled in `compute`). Route tables likewise aren't created here; Azure provides default routes.

### `compute`

**Inputs:** `spec.size` OR `spec.compute`, optional `spec.replicas` (default 1), optional `spec.storage.sizeGB` (default 30), optional `spec.imageRef` (default: latest Ubuntu 22.04 LTS).

**Primitives:**
1. `azurerm_resource_group.<component>` (shared with network if same RG name)
2. `azurerm_network_security_group.<component>` (default: egress only, like AWS)
3. `azurerm_public_ip.<component>_<i>` per replica (allocation = Static, sku = Standard)
4. `azurerm_network_interface.<component>_<i>` per replica
5. `azurerm_linux_virtual_machine.<component>_<i>` per replica with managed disk

T-shirt → Azure VM SKU (closest equivalent to AWS Phase 3 t3.* sizing):
| Size | Azure SKU | vCPU | RAM (GiB) |
|---|---|---|---|
| small | Standard_B2s | 2 | 4 |
| medium | Standard_B2ms | 2 | 8 |
| large | Standard_B4ms | 4 | 16 |
| xlarge | Standard_D4s_v5 | 4 | 16 |

(Note: Azure's B-series is burstable, similar to AWS t3; D_v5 is general-purpose, similar to AWS m6i. small/medium intentionally cross-cloud-compare to t3.small/t3.medium for parity engine even though Azure B2s = 4 GiB vs AWS t3.small = 2 GiB. The parity score will reflect this divergence — that's the point.)

Default image: Ubuntu 22.04 LTS (Canonical publisher, `ubuntu-22_04-lts`, latest version). Per-region differences in image ID don't apply for Azure — the same image reference works across regions.

### `database`

**Inputs:** `spec.engine` (postgres / mysql / mariadb), optional `spec.version`, `spec.size` OR `spec.compute`+`spec.storage`, optional `spec.multiAZ` (Azure equivalent: `high_availability { mode = ZoneRedundant }`).

**Primitives:**
- `azurerm_resource_group.<component>`
- If `engine == postgres`: `azurerm_postgresql_flexible_server.<component>` + `azurerm_postgresql_flexible_server_database.<component>_default`
- If `engine == mysql`: `azurerm_mysql_flexible_server.<component>` + `azurerm_mysql_flexible_server_database.<component>_default`
- If `engine == mariadb`: `azurerm_mariadb_server.<component>` + `azurerm_mariadb_database.<component>_default` (classic, not Flexible — Azure deprecation noted)

T-shirt → Azure Flexible Server SKU:
| Size | Postgres SKU | Tier | vCPU | RAM (GiB) | Storage default (GiB) |
|---|---|---|---|---|---|
| small | Standard_B1ms | Burstable | 1 | 2 | 100 |
| medium | Standard_B2s | Burstable | 2 | 4 | 250 |
| large | Standard_D2s_v3 | GeneralPurpose | 2 | 8 | 500 |
| xlarge | Standard_D4s_v3 | GeneralPurpose | 4 | 16 | 1000 |

Engine version defaults: postgres `16`, mysql `8.0`, mariadb `10.3` (latest classic MariaDB; Flexible would be `10.11` but unavailable).

`zone = "1"` set explicitly; `high_availability { mode = "ZoneRedundant" }` when `multiAZ: true`.

### `storage`

**Inputs:** optional `spec.name` (default: deterministic from component+region), optional `spec.versioning` (default true → `blob_properties.versioning_enabled = true`), optional `spec.publicAccess` (default blocked → `public_network_access_enabled = false` + `allow_nested_items_to_be_public = false`).

**Primitives:**
1. `azurerm_resource_group.<component>`
2. `azurerm_storage_account.<component>` (kind = StorageV2, account_tier = Standard, account_replication_type = LRS)
3. `azurerm_storage_container.<component>` (`name = "default"`, container_access_type = "private")

Storage account name has Azure-specific constraints: 3-24 characters, lowercase letters and numbers only. The adapter derives a safe name: `lower(remove-dashes(component))[0:18] + sha8(component+region)[0:6]`.

---

## Adapter Validate behavior

`azure.Adapter.Validate(target)` checks:
- `target.Region` is non-empty and matches Azure location naming (no hyphens; `eastus` not `us-east-1`). Rejects with `ErrAdapterAzureRegionInvalid`.
- For databases: `spec.engine` is one of the supported engines.
- For storage: derived storage account name (if user didn't override) is ≤24 chars.

---

## PricingKey shapes

### VM
```json
{
  "service": "VirtualMachines",
  "armSkuName": "Standard_B2s",
  "armRegionName": "eastus",
  "priceType": "Consumption",
  "productName": "Virtual Machines BS Series"
}
```

### Postgres Flexible Server
```json
{
  "service": "AzureDatabaseforPostgreSQL",
  "skuName": "Standard_B1ms",
  "armRegionName": "eastus",
  "tier": "Burstable",
  "priceType": "Consumption"
}
```

(Same shape with `service = "AzureDatabaseforMySQL"` for mysql; classic MariaDB has a different service name `MariaDB`.)

### Storage Account
```json
{
  "service": "Storage",
  "skuName": "Standard LRS",
  "armRegionName": "eastus",
  "tier": "Standard",
  "meterName": "LRS Data Stored"
}
```

### Free primitives

`azurerm_resource_group`, `azurerm_virtual_network`, `azurerm_subnet`, `azurerm_network_interface`, `azurerm_network_security_group`, `azurerm_public_ip` (per Azure: SKU "Basic" is free; Standard SKU has a small per-hour cost — Phase 4 returns `nil` for free, Phase 5 may revisit), `azurerm_storage_container`: all return `(nil, nil)` from PricingKey.

---

## Profile shapes

Same `parity.ResourceProfile` shape AWS uses, populated with Azure equivalents:

- VM: `Class = "compute"`, `Compute.VCPU/MemoryGB/Architecture` from the SKU table.
- PostgreSQL Flexible Server: `Class = "database"`, `Database.{Engine, Version, Compute, Storage}` + features map.
- Storage Account: `Class = "storage"`, `Storage.Class = "tiered"` (StorageV2 is tiered by default).
- Resource group / NIC / NSG / Subnet / VNet / Public IP: `ErrProfileUnavailable`.

---

## DefaultStateBackend

```go
ir.StateBackend{
    Kind: "azurerm",
    Config: map[string]any{
        "resource_group_name":  "nimbusfab-state",
        "storage_account_name": "nimbusfabstate",
        "container_name":       "terraform-state",
        "key":                  fmt.Sprintf("azure/%s/terraform.tfstate", target.Region),
    },
}
```

Users override per stack via `project.yaml` `stateBackend:` block, same pattern as AWS.

---

## ProviderBlock

```go
map[string]any{
    "azurerm": map[string]any{
        "features": map[string]any{}, // mandatory empty block
    },
}
```

Authentication: same pattern as AWS — credentials flow through env vars (`ARM_CLIENT_ID`, `ARM_CLIENT_SECRET`, `ARM_SUBSCRIPTION_ID`, `ARM_TENANT_ID`), never embedded in the provider block. The secrets backend resolves the credentialRef and the provisioner stuffs them into the runner's environment.

---

## Region / Location mapping

Azure locations use a different naming convention than AWS regions:
| AWS | Azure |
|---|---|
| us-east-1 | eastus |
| us-east-2 | eastus2 |
| us-west-2 | westus2 |
| eu-west-1 | westeurope |
| ap-southeast-2 | australiaeast |

Users specify Azure locations directly in YAML — no automatic translation. Multi-cloud projects look like:

```yaml
targets:
  - cloud: aws
    region: us-east-1
  - cloud: azure
    region: eastus
```

The adapter rejects AWS-style names via Validate. (When GCP lands, it'll have its own region naming convention, e.g., `us-central1`.)

---

## Pricing snapshot additions

`pkg/cost/pricing/snapshot/azure.json` (new file) covers the Phase-4 emissions:

| Service | Coverage |
|---|---|
| VirtualMachines | Standard_B2s, B2ms, B4ms, D4s_v5 × {eastus, eastus2, westeurope} × Linux |
| AzureDatabaseforPostgreSQL | Standard_B1ms, B2s, D2s_v3, D4s_v3 × same regions × Single / ZoneRedundant |
| AzureDatabaseforMySQL | same pattern (subset) |
| Storage | Standard LRS × same regions |

Snapshot row count: ~50 entries (smaller than AWS snapshot because fewer SKUs covered). Manually curated; same refresh process documented in `pkg/cost/pricing/snapshot/README.md`.

---

## Contract test suite

`pkg/plugin/contract.RunProvisionerScenarios` already runs against any `cloud.Adapter`. Phase 4 adds an `azure_test.go` in the plugin/contract package that runs the suite against `azure.New()`:

```go
func TestAzureAdapter_ProvisionerContract(t *testing.T) {
    sample := ir.DeploymentTarget{
        Cloud:  "azure",
        Region: "eastus",
        Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web", "__type": "network"},
    }
    contract.RunProvisionerScenarios(t, azure.New(), sample)
}
```

All seven contract scenarios pass (name stable, supported API versions / types, tier-one schema is JSON, provider block has no plaintext secrets, default state backend kind set, emit is pure).

---

## Full-stack fixture update

Add `azure/eastus` targets to the existing `cmd/cli/testdata/full-stack-project/components/*.yaml` so the fixture exercises both clouds. After this lands:
- `nimbusfab plan --stack dev cmd/cli/testdata/full-stack-project` shows 8 targets (4 components × 2 clouds).
- `nimbusfab parity` reports non-trivial scores per component (AWS vs Azure SKU divergence).
- `nimbusfab cost estimate` shows per-cloud subtotals.

---

## Error model additions

| Code | Origin | Meaning |
|---|---|---|
| `ErrAdapterAzureRegionInvalid` | `azure.Validate` | `target.Region` empty or uses AWS-style format |
| `ErrAdapterAzureUnsupportedType` | `azure.Emit` | `target.Spec["__type"]` not in the four v1 types |
| `ErrAdapterAzureUnsupportedEngine` | `azure.emitDatabase` | `spec.engine` not postgres/mysql/mariadb |
| `ErrAdapterAzureSizeConflict` | `azure.Validate` | Both `spec.size` and `spec.compute` set |
| `ErrAdapterAzureUnknownSize` | sizing helpers | T-shirt size not in mapping |
| `ErrAdapterAzureStorageNameInvalid` | `azure.emitStorage` | User-provided name violates Azure constraints |
| `WarnMariaDBClassicDeprecated` | `azure.emitDatabase` | Emit MariaDB via classic provider (Azure deprecation surface) |

---

## Verification (design-level)

1. **Per-type walkthrough.** Hand-draft `(cidr=10.0.0.0/16, size=small)` for each of network/compute/database/storage. Walk the emit logic; confirm the primitives form a valid Tofu workspace mentally.
2. **Cross-cloud parity walkthrough.** A `compute small` component targeting both AWS (`t3.small`: 2 vCPU / 2 GiB) and Azure (`Standard_B2s`: 2 vCPU / 4 GiB). Confirm parity engine reports memory divergence; `compute.memoryGB` score < 1.0.
3. **Cost-comparison walkthrough.** Same component; AWS cost ≈ $0.0208 × 730 ≈ $15.18; Azure cost ≈ $0.0416 × 730 ≈ $30.37 (B2s is roughly 2x t3.small per hour). Cost estimator shows both totals.
4. **Resource group walkthrough.** A 2-component (network + database) deployment to Azure. Confirm each component gets its own RG (one for network, one for database) — Phase 4 doesn't share RGs across components; sharing is a v2 optimization.
5. **State backend walkthrough.** `azure.DefaultStateBackend` returns `azurerm` kind. Confirm workspace.go's existing backend serializer handles `azurerm` without modification (it just JSON-serializes the backend block keyed by kind).
6. **Snapshot freshness.** Same 90-day staleness warning applies to Azure snapshot as AWS.

---

## Future hooks (not Phase 4)

- **Resource group sharing** across components in the same target (current default: one RG per component, mirroring AWS's "no RG" semantics with explicit per-component grouping).
- **Managed identities** for VM-to-storage / VM-to-DB authentication (Azure best practice, but requires per-resource IAM logic).
- **Azure SQL Database** as a separate component type or as an alternative engine — needs its own emit logic since the AzureRM provider distinguishes Flexible Server from Azure SQL.
- **VM Scale Sets** for the `compute` type's auto-scaling case (v2 with AWS ASG).
- **Application Gateway** as a separate component type for HTTP routing (separate spec).
- **Cross-region replication** for storage (Azure GRS / RA-GRS replication types).
- **Spot VMs** via `priority = "Spot"` — different pricing dimension, requires snapshot updates.
