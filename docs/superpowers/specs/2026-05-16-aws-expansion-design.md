# AWS Adapter Expansion + v1 Component Types Spec

**Status:** Subsystem spec. Defines the four v1 component types (`network`, `compute`, `database`, `storage`) and the concrete AWS primitives the AWS adapter emits per type. Locks in `PricingKey()` and `Profile()` shapes so the cost estimator and parity engine specs slot in without further changes to the adapter.

**Date:** 2026-05-16
**Depends on:**
- `docs/superpowers/specs/2026-05-14-architecture-design.md` (component types are a §2 module boundary)
- `docs/superpowers/specs/2026-05-15-dsl-ir-design.md` (component spec schemas are validator Phase 4 inputs)
- `docs/superpowers/specs/2026-05-15-provisioner-design.md` (`Adapter.Emit` / `PricingKey` / `Profile` contracts)
- `docs/superpowers/specs/2026-05-15-parity-design.md` (`ResourceProfile` shape this spec commits to)

**Depended on by:**
- Cost estimator spec (consumes the `PricingKey` shapes locked here)
- Parity engine spec (consumes the `Profile` data per resource class)
- Azure / GCP adapter specs (mirror this structure with per-cloud primitive choices)
- Web app spec (renders per-type spec schemas in the UI)

---

## Context

Through Provisioner Phases 1–2 + Inventory Phase 1, the engine plans / applies / destroys / drifts AWS deployments end-to-end — but only for `network` components, and only emitting a single `aws_vpc`. The architecture spec promised four v1 component types (network, compute, database, storage) and four AWS primitives per network alone (VPC, subnets, internet gateway, route tables). This spec closes that gap: defines the four types, defines the AWS primitives per type, and pins `PricingKey()` and `Profile()` so downstream specs (cost, parity) need zero adapter changes when they wire up.

**Design principles:**
1. **Sane defaults > exhaustive knobs.** Users specify intent (T-shirt size, "I want a postgres database") and the adapter picks reasonable AWS resources. Knobs are added when a real user need surfaces, not preemptively.
2. **Outputs are the contract.** Each component type publishes a fixed list of outputs (`vpc_id`, `subnet_ids`, `endpoint`, etc.). Downstream components reference them via `${component.X.outputs.Y}`. This list is stable across cloud adapters — AWS's `vpc_id` and the eventual GCP adapter's `vpc_id` are the same key, even though the underlying resources differ.
3. **One AWS adapter per component type stays in the same file.** `internal/cloud/aws/network.go`, `database.go`, `compute.go`, `storage.go`. Adapter root in `emit.go` dispatches on `target.Spec["__type"]`. Keeps per-type tests close to per-type code.
4. **PricingKey is opaque to the engine.** The cost estimator passes whatever the adapter returns to the pricing cache. The shape is the adapter's choice; this spec documents AWS's choices so the estimator can rely on them.
5. **Profile data is real.** Even though the parity engine isn't wired in this phase, the adapter returns concrete `ResourceProfile` values (Compute / Storage / Database / Network sub-profiles). The parity engine adopts them as-is.

---

## Scope

**In scope (this spec):**
- Concrete `components.Type` implementations for `network`, `compute`, `database`, `storage` — each with `Name()`, `SpecSchema()`, `SupportedClouds()`, `Outputs()`.
- A `DefaultRegistry()` helper that registers all four.
- A `Type.Outputs() map[string]OutputType` extension to the `components.Type` interface (the current scaffold doesn't expose Outputs; this spec adds it).
- AWS adapter dispatch in `Emit()` based on a new `target.Spec["__type"]` field the provisioner stuffs alongside `__component`.
- AWS primitives per type:
  - `network` → `aws_vpc`, three `aws_subnet`s (one per AZ in the region's default AZ trio), `aws_internet_gateway`, `aws_route_table`, three `aws_route_table_association`s.
  - `database` → `aws_db_subnet_group`, `aws_db_instance` (instance class + storage resolved from T-shirt size).
  - `compute` → `aws_security_group`, `aws_instance` (count = `spec.replicas`, default 1; instance type from T-shirt size).
  - `storage` → `aws_s3_bucket`, `aws_s3_bucket_versioning`, `aws_s3_bucket_public_access_block`, `aws_s3_bucket_server_side_encryption_configuration`.
- `PricingKey()` shapes per primitive type — what the cost estimator hands to the AWS Price List API.
- `Profile()` shapes per primitive type — what the parity engine compares across clouds.
- T-shirt size → SKU resolution tables for `database` (RDS instance classes + storage GiB) and `compute` (EC2 instance types + EBS GiB).
- Provisioner-side change to pass component type to the adapter (one-line addition to `target.Spec["__type"]`).
- Updates to the AWS adapter's `SupportedComponentTypes()` return value.
- Golden file tests for every type's emit output.

**Out of scope (deferred):**
- Validator Phase 4 (component-spec validation against per-type `SpecSchema`). The types ship their schemas; wiring them into the validator pipeline is a DSL/IR Phase 2 (separate phase). Phase 3 emits component specs the validator doesn't yet enforce against the per-type schema; users can therefore put bogus fields and the validator won't catch them until Phase 4 lands.
- Cost estimator / parity engine wiring. This spec produces the data; consuming it is its own phase. `PricingKey` returns a non-empty map (engine doesn't use it yet); `Profile` returns a populated `ResourceProfile` (parity engine doesn't use it yet); `BillingQuery` and `FetchBilling` still return `ErrNotImplementedYet`.
- Azure / GCP adapters. Each is its own phase; both follow this spec's shape per type.
- Tier-1 (`<cloud>:` block) escape hatch schemas for AWS. The architecture allows it; users in Phase 3 can put cloud-specific overrides under `target.spec.aws` but the adapter ignores them. Tier-1 wiring is a follow-up.
- `raw:` tier-2 passthrough merging into emitted Tofu JSON. The provisioner doesn't merge it; the adapter doesn't read it. The `WarnRawEscape` diagnostic emission lands here as a TODO.
- Cross-component refs beyond same-stack VPC ID + subnet IDs (the only refs Phase 3's components need). `${component.X.outputs.Y}` works for the four output names this spec defines; arbitrary chains land with the validator's full Phase 4-6 work.
- Compositions consuming v1 types — already supported by the composition expander; Phase 3 doesn't add anything.
- Real LocalStack / AWS integration tests. Phase 3 expands the unit-test surface; full E2E is a credentials-gated CI phase.

---

## The four v1 component types

### `network`

A virtual private cloud (or equivalent in other clouds) with public subnets and an internet gateway. Phase 3 is single-tier (one subnet per AZ, all public-facing). Multi-tier networks (public + private subnets, NAT gateways) are a v2 surface.

**Spec schema:**
```yaml
type: object
required: [cidr]
additionalProperties: false
properties:
  cidr:
    type: string
    description: "IPv4 CIDR block for the VPC. /16 recommended for ≥3 /24 subnets."
    pattern: "^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+/[0-9]+$"
  enableIPv6:
    type: boolean
    default: false
  subnetCount:
    type: integer
    minimum: 1
    maximum: 16
    default: 3
    description: "Number of /24 subnets to carve from cidr."
```

**Outputs:**

| Name | Type | Source |
|---|---|---|
| `vpc_id` | string | The VPC primitive's `id` attribute |
| `subnet_ids` | list<string> | List of subnet `id`s in declaration order |
| `route_table_ids` | list<string> | List of route table `id`s |

### `compute`

A pool of N identical compute instances behind a single security group. Phase 3 single-AZ; the auto-scaling group / multi-AZ surface is v2.

**Spec schema:**
```yaml
type: object
additionalProperties: false
oneOf:
  - required: [size]
  - required: [compute]
properties:
  size:
    type: string
    enum: [small, medium, large, xlarge]
  compute:
    type: object
    required: [vCPU, memoryGB]
    additionalProperties: false
    properties:
      vCPU: { type: integer, minimum: 1 }
      memoryGB: { type: number, minimum: 0.5 }
      architecture: { type: string, enum: [x86_64, arm64], default: x86_64 }
  replicas: { type: integer, minimum: 1, default: 1 }
  storage:
    type: object
    additionalProperties: false
    properties:
      sizeGB: { type: integer, minimum: 8, default: 30 }
      class:  { type: string, enum: [ssd, hdd], default: ssd }
  imageRef:
    type: string
    description: "Cloud-native image identifier (AMI ID / image name). Default = adapter's recommended Linux."
  refs:
    type: object
    properties:
      vpcId: { type: string }
      subnetId: { type: string }
```

**Outputs:**

| Name | Type | Source |
|---|---|---|
| `instance_ids` | list<string> | EC2 instance IDs |
| `private_ips` | list<string> | First private IP per instance |
| `security_group_id` | string | Backing SG ID |

### `database`

A managed relational database. Phase 3 supports postgres + mysql via RDS. Other engines and aurora are v2.

**Spec schema:**
```yaml
type: object
required: [engine]
additionalProperties: false
oneOf:
  - required: [size]
  - required: [compute, storage]
properties:
  engine: { type: string, enum: [postgres, mysql, mariadb] }
  version: { type: string }  # adapter picks a sane default if omitted
  size: { type: string, enum: [small, medium, large, xlarge] }
  compute:
    type: object
    required: [vCPU, memoryGB]
    additionalProperties: false
    properties:
      vCPU: { type: integer, minimum: 1 }
      memoryGB: { type: number, minimum: 0.5 }
  storage:
    type: object
    additionalProperties: false
    properties:
      sizeGB: { type: integer, minimum: 20, default: 100 }
      iops: { type: integer, minimum: 1000 }
      class: { type: string, enum: [ssd], default: ssd }
  multiAZ: { type: boolean, default: false }
  pointInTimeRestore: { type: boolean, default: true }
  refs:
    type: object
    properties:
      subnetIds: { type: array, items: { type: string } }
      vpcId: { type: string }
```

**Outputs:**

| Name | Type | Source |
|---|---|---|
| `endpoint` | string | DB instance endpoint hostname |
| `port` | integer | DB port |
| `db_name` | string | Default DB name (engine default) |

### `storage`

An object storage bucket with secure defaults. Phase 3 is S3-only; lifecycle policies + cross-region replication are v2.

**Spec schema:**
```yaml
type: object
additionalProperties: false
properties:
  name: { type: string, description: "Bucket name; defaults to <project>-<component>-<random>." }
  versioning: { type: boolean, default: true }
  encryption:
    type: object
    additionalProperties: false
    properties:
      algorithm: { type: string, enum: [AES256, aws:kms], default: AES256 }
      kmsKeyArn: { type: string }
  publicAccess:
    type: string
    enum: [blocked, allowed]
    default: blocked
```

**Outputs:**

| Name | Type | Source |
|---|---|---|
| `bucket_name` | string | S3 bucket name |
| `bucket_arn` | string | S3 bucket ARN |
| `bucket_url` | string | `https://<bucket>.s3.<region>.amazonaws.com` |

---

## `components.Type` interface extension

Phase 3 adds one method to `components.Type`:

```go
type Type interface {
    Name() string
    SpecSchema() []byte
    SupportedClouds() []string
    Outputs() map[string]OutputType   // NEW
    Emit(ctx context.Context, target ir.DeploymentTarget, adapter cloud.Adapter, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error)
}

type OutputType struct {
    Kind        string  // "string" | "integer" | "boolean" | "list<string>" | ...
    Description string
}
```

`Outputs()` is consulted by the validator (when ref resolution lands in DSL/IR Phase 2) and surfaced in the web app's type browser. Phase 3 implementations return the tables above.

The existing `Emit` method on the Type stays in place but Phase 3 doesn't use it — the provisioner still calls `adapter.Emit()` directly, with the adapter dispatching internally on `target.Spec["__type"]`. The Type-level Emit becomes the documented way for an out-of-tree component-type plugin to interpose; in-tree types delegate to the adapter and Phase 3 just sets it up to return `adapter.Emit(ctx, target, refs)`.

### `DefaultRegistry()`

```go
// DefaultRegistry returns an inventory.Registry populated with the four v1
// component types: network, compute, database, storage. Engines that want
// the standard surface use this; tests can use NewInMemoryRegistry() with
// a custom subset.
func DefaultRegistry() Registry { ... }
```

The engine's `Config.ComponentTypes` defaults to `DefaultRegistry()` when nil (mirroring the `nullRepo` pattern for `InventoryRepo`).

---

## Provisioner change: pass component type to adapter

In `pkg/provisioner/plan.go`, alongside `target.Spec["__component"] = comp.Name`, add:

```go
target.Spec["__type"] = comp.Type
```

That's the entire provisioner change. The AWS adapter reads it in `Emit()` to dispatch.

---

## AWS Emit dispatch

In `internal/cloud/aws/emit.go`:

```go
func (a *Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    compType, _ := target.Spec["__type"].(string)
    switch compType {
    case "network":
        return a.emitNetwork(ctx, target, refs)
    case "compute":
        return a.emitCompute(ctx, target, refs)
    case "database":
        return a.emitDatabase(ctx, target, refs)
    case "storage":
        return a.emitStorage(ctx, target, refs)
    case "":
        // Backward compat: Phase 1 tests don't set __type. Default to network.
        return a.emitNetwork(ctx, target, refs)
    default:
        return nil, fmt.Errorf("aws: unsupported component type %q", compType)
    }
}
```

Each `emit<Type>` lives in its own file (`network.go`, `compute.go`, `database.go`, `storage.go`) with its golden file in `testdata/`.

### `emitNetwork`

Inputs: `spec.cidr`, optional `spec.subnetCount` (default 3), optional `spec.enableIPv6` (default false).

Produces (ordered by deterministic ID):
1. `aws_vpc.<component>`
2. `aws_internet_gateway.<component>` referencing the VPC
3. `aws_route_table.<component>` with a 0.0.0.0/0 route to the IGW
4. `aws_subnet.<component>_<i>` for `i` in `0..subnetCount-1`, each in a different AZ, /24 sliced from the VPC CIDR
5. `aws_route_table_association.<component>_<i>` associating each subnet with the route table

AZs chosen: `<region>a`, `<region>b`, `<region>c` (the universally-available trio in supported regions; covers us-east-1, us-west-2, eu-west-1, etc.). For regions that don't follow the convention, Phase 3 emits a `WarnRegionAZAssumption` (warning in `[]ir.Issue` returned via `Validate()`, but doesn't block).

### `emitCompute`

Inputs: `spec.size` OR `spec.compute`, optional `spec.replicas` (default 1), optional `spec.storage` (default 30 GiB SSD), optional `spec.imageRef` (default: latest Amazon Linux 2023 AMI ID from a hardcoded map per region).

Produces:
1. `aws_security_group.<component>` with egress 0.0.0.0/0 (no ingress by default; users open ports via tier-1 escape hatch when Phase-3 wires it, otherwise via manual security_group_rule resources in a later phase)
2. `aws_instance.<component>_<i>` for `i` in `0..replicas-1`, each with the resolved AMI + instance type + EBS root volume

T-shirt → instance type:
| Size | Instance type | vCPU | RAM (GiB) |
|---|---|---|---|
| small | t3.small | 2 | 2 |
| medium | t3.medium | 2 | 4 |
| large | t3.large | 2 | 8 |
| xlarge | t3.xlarge | 4 | 16 |

When `spec.compute` is given explicitly, the adapter picks the cheapest `t3.*` (or `m6i.*` for >16 GiB or >4 vCPU) that satisfies both vCPU and memory floors.

### `emitDatabase`

Inputs: `spec.engine`, optional `spec.version` (default per engine: postgres=16, mysql=8.0, mariadb=10.11), `spec.size` OR `spec.compute`+`spec.storage`, optional `spec.multiAZ` (default false), optional `spec.pointInTimeRestore` (default true).

Produces:
1. `aws_db_subnet_group.<component>` with `subnet_ids` from `refs.subnetIds` (or, if missing, the literal interpolation `${data.terraform_remote_state.<comp>.outputs.subnet_ids}`)
2. `aws_db_instance.<component>` with resolved instance class + allocated storage

T-shirt → RDS instance class + storage:
| Size | Instance class | vCPU | RAM (GiB) | Storage default (GiB) |
|---|---|---|---|---|
| small | db.t3.small | 2 | 2 | 100 |
| medium | db.t3.medium | 2 | 4 | 250 |
| large | db.m6i.large | 2 | 8 | 500 |
| xlarge | db.m6i.xlarge | 4 | 16 | 1000 |

Engine version map and Phase-3 chosen defaults documented in code; sourced from `internal/cloud/aws/database_versions.go`.

### `emitStorage`

Inputs: optional `spec.name` (default: `<project>-<component>-<6char-random>`), optional `spec.versioning` (default true), optional `spec.encryption.algorithm` (default AES256), optional `spec.publicAccess` (default blocked).

Produces:
1. `aws_s3_bucket.<component>`
2. `aws_s3_bucket_versioning.<component>` set to Enabled iff versioning=true
3. `aws_s3_bucket_public_access_block.<component>` with all four blocks true iff publicAccess=blocked
4. `aws_s3_bucket_server_side_encryption_configuration.<component>` per algorithm

Bucket name uniqueness: when `spec.name` is empty, the adapter generates `<project-name>-<component>-<sha8(component+region)>` to keep deterministic across runs while staying unique per project. (Truly random suffixes would break determinism.)

---

## `PricingKey()` shapes

The cost estimator uses these as cache keys + as input to AWS Price List API calls. Shape is opaque to the engine but documented here so the estimator can rely on it.

### `aws_vpc`, `aws_internet_gateway`, `aws_subnet`, `aws_route_table`, `aws_route_table_association`

```go
nil, nil  // No cost for these primitives in AWS (data transfer aside).
```

Returning `(nil, nil)` is the convention for free primitives. The estimator skips them.

### `aws_instance`

```go
map[string]any{
    "service":      "AmazonEC2",
    "instanceType": "t3.medium",
    "region":       "us-east-1",
    "tenancy":      "Shared",
    "operatingSystem": "Linux",
    "preInstalledSw": "NA",
    "capacitystatus": "Used",
}, nil
```

### `aws_db_instance`

```go
map[string]any{
    "service":          "AmazonRDS",
    "instanceType":     "db.t3.medium",
    "region":           "us-east-1",
    "engineCode":       "postgres",
    "deploymentOption": "Single-AZ",  // or "Multi-AZ"
    "licenseModel":     "No license required",
}, nil
```

### `aws_s3_bucket`

```go
map[string]any{
    "service":       "AmazonS3",
    "region":        "us-east-1",
    "storageClass":  "Standard",
    // Note: S3 cost is usage-based, not provisioning-based. Estimator uses
    // `spec.usage` (when present) to drive cost estimates; otherwise it
    // shows "usage-priced" with $0 baseline.
}, nil
```

The estimator's pricing-cache normalizes these keys (sorts map keys, drops adapter-private fields) before hashing.

---

## `Profile()` shapes

For each emitted primitive, the adapter populates the relevant sub-profile of `parity.ResourceProfile`. Other clouds populate the same shape with their own SKU values, enabling cross-cloud comparison.

### `aws_vpc`

```go
parity.ResourceProfile{
    Class: "network",
    Network: &parity.NetworkProfile{
        CIDR:          "10.0.0.0/16",
        BandwidthGbps: 0,  // VPC itself has no fixed bandwidth; instances drive it
        IPv6:          false,
        NAT:           false,  // Phase 3 doesn't create NAT gateways
    },
    SKU: "aws_vpc",
}
```

### `aws_subnet`, `aws_internet_gateway`, `aws_route_table*`

```go
return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
```

Routing-layer primitives don't have a meaningful cross-cloud profile.

### `aws_instance`

```go
parity.ResourceProfile{
    Class: "compute",
    Compute: &parity.ComputeProfile{
        VCPU:         2,
        MemoryGB:     4,
        Architecture: "x86_64",
        NetworkGbps:  5,   // t3.medium "up to 5 Gbps"
    },
    Storage: &parity.StorageProfile{
        SizeGB:    30,
        Class:     "ssd",  // gp3 default
        Encrypted: true,
    },
    SKU: "t3.medium",
}
```

### `aws_db_instance`

```go
parity.ResourceProfile{
    Class: "database",
    Database: &parity.DatabaseProfile{
        Engine:   "postgres",
        Version:  "16",
        Compute:  parity.ComputeProfile{VCPU: 2, MemoryGB: 4},
        Storage:  parity.StorageProfile{SizeGB: 250, IOPS: 3000, Class: "ssd", Encrypted: true},
        Replicas: 0,
        HA:       false,
    },
    Features: map[string]bool{
        "pointInTimeRestore": true,
        "multiAZ":            false,
    },
    SKU: "db.t3.medium",
}
```

### `aws_s3_bucket`

```go
parity.ResourceProfile{
    Class: "storage",
    Storage: &parity.StorageProfile{
        SizeGB:    0,  // S3 is usage-priced; size unknown at plan time
        Class:     "tiered",  // S3 standard with intelligent tiering hint
        Encrypted: true,
    },
    Features: map[string]bool{
        "versioning":   true,
        "publicAccess": false,
    },
    SKU: "aws_s3_standard",
}
```

---

## T-shirt size resolution

Each emit function has a sizing helper that turns `spec.size` (or explicit `spec.compute`/`spec.storage`) into concrete AWS values. The mapping lives in per-type Go maps (`network.go` has no sizing; `compute.go`, `database.go`, `storage.go` have one each).

Validation: if both `spec.size` and `spec.compute` are set, the adapter's `Validate()` returns `ErrSizeConflict`. If `spec.size` is unknown, `ErrUnknownSize`. If `spec.compute` is missing required fields, `ErrMissingDimension`. (These error codes are defined in the DSL/IR spec; the adapter just emits them.)

When the validator's Phase 4 (per-type SpecSchema validation) lands, most of these checks become redundant — but adapter-level validation stays as defense-in-depth.

---

## Cross-component refs

Phase 3 fixtures exercise the common pattern: a `database` component references a `network` component's `subnet_ids` + `vpc_id`. The provisioner's existing `data.terraform_remote_state` injection (Phase 2) handles the workspace plumbing; the database emit uses the Tofu interpolation strings:

```go
attrs["db_subnet_group_name"] = "${aws_db_subnet_group." + name + ".name}"
attrs["vpc_security_group_ids"] = []string{"${aws_security_group." + name + ".id}"}
```

For refs that cross component boundaries:

```go
// In database emit, when refs is empty (no explicit `refs:` in YAML):
subnetRef := "${data.terraform_remote_state." + tofuIdentForComponent(refsComp) + ".outputs.subnet_ids}"
```

Where `refsComp` comes from the component's `refs[].component` declaration. If no refs are declared, the database emit returns an error: `ErrAdapterAWSDatabaseRequiresNetwork`.

---

## Error model additions

| Code | Origin | Meaning |
|---|---|---|
| `ErrAdapterAWSUnsupportedType` | `aws.Emit` | `target.Spec["__type"]` not in `{network, compute, database, storage}` |
| `ErrAdapterAWSDatabaseRequiresNetwork` | `aws.emitDatabase` | No `refs:` declared pointing to a network component |
| `ErrAdapterAWSComputeRequiresNetwork` | `aws.emitCompute` | Same for compute |
| `ErrAdapterAWSSizeConflict` | `aws.Validate` | Both `spec.size` and `spec.compute` set |
| `ErrAdapterAWSUnknownSize` | `aws.Validate` / sizing helpers | `spec.size` not in `{small, medium, large, xlarge}` |
| `ErrAdapterAWSMissingDimension` | sizing helpers | `spec.compute` missing required field |
| `ErrAdapterAWSUnknownEngine` | `aws.emitDatabase` | `spec.engine` not in `{postgres, mysql, mariadb}` |
| `WarnRegionAZAssumption` | `aws.emitNetwork` | Region doesn't follow `<region>{a,b,c}` AZ convention; adapter guessed |
| `WarnRawEscape` | `aws.Emit` (TODO Phase 3) | Reserved; emitted when tier-2 `raw:` block is present (wiring deferred) |

All errors implement `UserFacing() (code, message, remediation)`.

---

## Verification (design-level)

This is a design spec. Verify by:

1. **Per-type emit walkthrough.** For each of `network`, `compute`, `database`, `storage`, hand-draft a YAML component spec and walk the emit logic. Confirm the resulting primitives form a complete, applicable Tofu workspace (mentally run `tofu validate`).
2. **Cross-component ref walkthrough.** A 2-component project: `web-network` (network) + `orders-db` (database referencing web-network). Trace from YAML → validator → plan → orders-db's workspace has `data.terraform_remote_state.web_network` block → its `aws_db_subnet_group` references that block's `outputs.subnet_ids`.
3. **PricingKey shape walkthrough.** For each pricing-bearing primitive, confirm the returned map's keys exactly match the AWS Price List API's filter dimensions for that service. (Verify against AWS docs; mismatched keys = no pricing data found at estimator time.)
4. **Profile shape walkthrough.** Construct three deployments of the "same" `database` component to AWS / GCP / Azure (mentally — only AWS is implemented). Confirm the three `Profile()` outputs would feed the parity engine's score function with comparable values: same Class, same approximate Compute/Storage shape, Features map with same keys.
5. **T-shirt mapping coverage.** For each of `small / medium / large / xlarge` × each of `compute / database`, confirm there's a concrete instance type chosen and that the chosen type actually satisfies the parity contract's floors (see parity spec §"Contract floors").
6. **Determinism check.** Re-emit the same component twice. Confirm primitive IDs, attribute maps, and tags are byte-identical. The S3 bucket-name derivation uses a deterministic hash, not random — verify mentally.

---

## Future hooks (not Phase 3)

- **Tier-1 `<cloud>:` escape hatch.** Reserve a per-type AWS schema (`internal/cloud/aws/schema/v1alpha1/aws_network.json` etc.) that the adapter merges into the emitted attributes. Phase 3 ships the framework but doesn't wire user-facing schemas yet.
- **NAT gateways for private subnets.** Reserve a `spec.privateSubnetCount` field; v2 adds them.
- **Auto-scaling groups for compute.** Reserve a `spec.scaling: { min, max, metric }` field; v2 surfaces it.
- **Cross-region replication for storage.** Reserve `spec.replicateTo: <region>`; v2.
- **RDS read replicas.** Reserve `spec.readReplicas: <N>`; v2.
- **More image types for compute** (Ubuntu, Debian, custom AMIs). v1 = Amazon Linux 2023.
- **S3 lifecycle rules.** v2.
- **VPC peering and Transit Gateway.** v2.
