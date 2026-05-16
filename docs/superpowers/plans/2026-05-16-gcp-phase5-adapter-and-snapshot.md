# GCP Adapter Phase 5 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Working GCP adapter that mirrors AWS Phase 3 and Azure Phase 4 for the four v1 component types. After Phase 5, `nimbusfab plan` against a project with `gcp/us-central1` targets produces real GCP Tofu workspaces; three-cloud projects light up parity engine + cost estimator with 3-way reports.

**Architecture:** `internal/cloud/gcp/{adapter,emit,network,compute,database,storage,pricing,profile,stubs}.go` matching AWS/Azure layout 1:1. CLI auto-registers GCP adapter alongside AWS + Azure. Pricing snapshot adds `pkg/cost/pricing/snapshot/gcp.json` covering the Phase-5 SKUs.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-gcp-phase5/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- `google-beta` provider resources.
- Service Account / IAM management.
- BigQuery / Spanner / Firestore / Bigtable / GKE / Cloud Run.
- Tier-1 `<cloud>: gcp:` escape hatch schemas.
- Live integration tests (no GCP emulator for Compute / Cloud SQL).

---

## Task 1: Adapter scaffold + Emit dispatch shim + per-type stubs

**Files:**
- Create: `internal/cloud/gcp/adapter.go`
- Create: `internal/cloud/gcp/emit.go`
- Create: `internal/cloud/gcp/schema.go`
- Create: `internal/cloud/gcp/schema/v1alpha1/tier_one.json`
- Create: `internal/cloud/gcp/stubs.go`
- Create: `internal/cloud/gcp/adapter_test.go`

- [ ] **Step 1: Adapter scaffold** with `Adapter{}`, `New()`, `Name()="gcp"`, `SupportedAPIVersions()=[v1alpha1]`, `SupportedComponentTypes()=[network,compute,database,storage]`, `TierOneSchema()=tierOneSchema`. `Validate` rejects empty region and any region not matching `^[a-z]+-[a-z]+[0-9]$` with `ErrAdapterGCPRegionInvalid`. `DefaultStateBackend` returns `{Kind: "gcs", Config: {bucket: "nimbusfab-state", prefix: fmt.Sprintf("gcp/%s", target.Region)}}`. `ProviderBlock` returns `{"google": {"project": <from spec or empty>, "region": target.Region}}`. `BillingQuery`/`FetchBilling` return `cloud.ErrNotImplementedYet`.

- [ ] **Step 2: Tier-one schema** (empty pass-through with optional `tags` map, same shape as Azure).

- [ ] **Step 3: Emit dispatch** in `emit.go`:

```go
func (a *Adapter) Emit(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    compType, _ := target.Spec["__type"].(string)
    switch compType {
    case "network":  return a.emitNetwork(ctx, target, refs)
    case "compute":  return a.emitCompute(ctx, target, refs)
    case "database": return a.emitDatabase(ctx, target, refs)
    case "storage":  return a.emitStorage(ctx, target, refs)
    default: return nil, fmt.Errorf("gcp: unsupported component type %q", compType)
    }
}
```

Helpers: `tofuIdent` (lowercase + sanitize to `[a-z0-9_]`); `gcpResourceName(component string)` returning a GCP-safe name (`<component>` lowercase with hyphens preserved, validated `^[a-z]([-a-z0-9]*[a-z0-9])?$`, ≤63 chars).

- [ ] **Step 4: Per-type stubs in stubs.go** (replaced by Tasks 2-5):
Each method returns `nil, fmt.Errorf("gcp: <type> emit not yet implemented")`. Also `Profile` returns `parity.ResourceProfile{}, cloud.ErrProfileUnavailable` and `PricingKey` returns `nil, nil` (Tasks 6-7 replace these).

- [ ] **Step 5: Adapter scaffold tests** covering: `Name() == "gcp"`; `SupportedComponentTypes()` has 4 entries; `Validate` rejects empty region; `Validate` rejects `us-east-1` (AWS format); `Validate` rejects `eastus` (Azure format); `Validate` accepts `us-central1`; `Validate` accepts `europe-west1`; `DefaultStateBackend` returns `gcs` kind with `bucket` and `prefix` keys; `ProviderBlock` has `google` entry with `region`; `Emit` with unsupported `__type` errors.

- [ ] **Step 6: Build + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/gcp/ -v
git add internal/cloud/gcp/
git commit -m "gcp: adapter scaffold + Emit dispatch shim + per-type stubs"
```

---

## Task 2: Network emit (VPC + Subnetworks + Firewalls)

**Files:**
- Create: `internal/cloud/gcp/network.go` (replaces stub)
- Create: `internal/cloud/gcp/network_test.go`

- [ ] **Step 1: Implement emitNetwork**

Per spec: `google_compute_network` (auto_create_subnetworks=false) + N `google_compute_subnetwork` (regional, /24 slices) + `google_compute_firewall.<name>_internal` (allow intra-VPC) + `google_compute_firewall.<name>_deny_external` (deny external ingress).

```go
func emitNetworkImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    cidr, _ := target.Spec["cidr"].(string)
    if cidr == "" { cidr = "10.0.0.0/16" }
    component, _ := target.Spec["__component"].(string)
    if component == "" { component = "network" }
    subnetCount := intFromSpec(target.Spec, "subnetCount", 3)
    if subnetCount < 1 { subnetCount = 1 }
    name := tofuIdent(component)
    netName := gcpResourceName(component) + "-vpc"

    subnetCIDRs, err := splitCIDR(cidr, subnetCount)
    if err != nil { return nil, fmt.Errorf("gcp.emitNetwork: %w", err) }

    out := []ir.ResourcePrimitive{
        {
            ID: fmt.Sprintf("%s.gcp-%s.vpc", component, target.Region),
            Cloud: "gcp", TofuType: "google_compute_network", TofuName: name,
            Attributes: map[string]any{
                "name": netName,
                "auto_create_subnetworks": false,
            },
        },
    }
    for i := 0; i < subnetCount; i++ {
        sname := fmt.Sprintf("%s_%d", name, i)
        out = append(out, ir.ResourcePrimitive{
            ID: fmt.Sprintf("%s.gcp-%s.subnet_%d", component, target.Region, i),
            Cloud: "gcp", TofuType: "google_compute_subnetwork", TofuName: sname,
            Attributes: map[string]any{
                "name": fmt.Sprintf("%s-subnet-%d", gcpResourceName(component), i),
                "ip_cidr_range": subnetCIDRs[i],
                "region": target.Region,
                "network": "${google_compute_network." + name + ".id}",
            },
        })
    }
    out = append(out,
        ir.ResourcePrimitive{
            ID: fmt.Sprintf("%s.gcp-%s.fw_internal", component, target.Region),
            Cloud: "gcp", TofuType: "google_compute_firewall", TofuName: name + "_internal",
            Attributes: map[string]any{
                "name": gcpResourceName(component) + "-fw-internal",
                "network": "${google_compute_network." + name + ".name}",
                "direction": "INGRESS",
                "source_ranges": []any{cidr},
                "allow": []any{map[string]any{"protocol": "all"}},
            },
        },
        ir.ResourcePrimitive{
            ID: fmt.Sprintf("%s.gcp-%s.fw_deny_external", component, target.Region),
            Cloud: "gcp", TofuType: "google_compute_firewall", TofuName: name + "_deny_external",
            Attributes: map[string]any{
                "name": gcpResourceName(component) + "-fw-deny-ext",
                "network": "${google_compute_network." + name + ".name}",
                "direction": "INGRESS",
                "priority": 65000,
                "source_ranges": []any{"0.0.0.0/0"},
                "deny": []any{map[string]any{"protocol": "all"}},
            },
        },
    )
    return out, nil
}
```

Add helpers `intFromSpec`, `splitCIDR` (same shape as Azure's).

- [ ] **Step 2: Test** — full primitive shape (1 VPC + N subnets + 2 firewalls); determinism; custom subnet count; CIDR splitting (`10.0.0.0/16` → `10.0.0.0/24`, `10.0.1.0/24`, `10.0.2.0/24` for default count=3).

- [ ] **Step 3: Replace stub in stubs.go** with `return emitNetworkImpl(target, refs)`; remove the imports that become unused.

- [ ] **Step 4: Build + commit** `gcp: network emit (VPC + subnetworks + firewalls)`

---

## Task 3: Compute emit (Instance + boot disk + egress firewall)

**Files:**
- Create: `internal/cloud/gcp/compute.go`
- Create: `internal/cloud/gcp/compute_test.go`

Per spec § "compute". T-shirt → GCP machine type:

```go
var computeMachineTypes = map[string]string{
    "small":  "e2-small",
    "medium": "e2-medium",
    "large":  "e2-standard-2",
    "xlarge": "n2-standard-4",
}
```

- [ ] **Step 1: Implement emitCompute**

Primitives per replica `i`: `google_compute_instance.<name>_<i>` with `zone = <region>-{a,b,c}[i%3]`, `machine_type = computeMachineTypes[size]`, `boot_disk { initialize_params { image = imageRef } }`, `network_interface { network = "default" }` (if no network ref, use default; if `refs["networkId"]` present, use it as subnetwork), `metadata_startup_script` left unset. Plus `google_compute_firewall.<name>_egress` allowing all egress.

Default image: `"ubuntu-os-cloud/ubuntu-2204-lts"`. Default boot disk size: 30 GB.

- [ ] **Step 2: Test** — primitive count (1 firewall + N instances); machine_type per size; zone distribution across replicas; custom imageRef; custom storage.sizeGB.

- [ ] **Step 3: Replace stub; build + commit** `gcp: compute emit (instances + egress firewall)`

---

## Task 4: Database emit (Cloud SQL Instance + default database)

**Files:**
- Create: `internal/cloud/gcp/database.go`
- Create: `internal/cloud/gcp/database_test.go`

T-shirt → Cloud SQL tier:

```go
var dbTiers = map[string]string{
    "small":  "db-f1-micro",
    "medium": "db-g1-small",
    "large":  "db-custom-2-7680",
    "xlarge": "db-custom-4-15360",
}
var dbStorageDefaults = map[string]int{
    "small": 10, "medium": 20, "large": 100, "xlarge": 200,
}
```

- [ ] **Step 1: Implement emitDatabase**

```go
func emitDatabaseImpl(target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    engine, _ := target.Spec["engine"].(string)
    if engine == "" { engine = "postgres" }
    if engine == "mariadb" {
        return nil, fmt.Errorf("gcp.emitDatabase: %w", ErrAdapterGCPMariaDBUnsupported)
    }
    if engine != "postgres" && engine != "mysql" {
        return nil, fmt.Errorf("gcp.emitDatabase: %w (engine=%q)", ErrAdapterGCPUnsupportedEngine, engine)
    }
    component, _ := target.Spec["__component"].(string)
    if component == "" { component = "database" }
    size, _ := target.Spec["size"].(string)
    if size == "" { size = "small" }
    tier, ok := dbTiers[size]
    if !ok { return nil, fmt.Errorf("gcp.emitDatabase: unknown size %q", size) }
    storageGB := intFromSpec(target.Spec, "storageGB", dbStorageDefaults[size])
    name := tofuIdent(component)
    instanceName := gcpResourceName(component) + "-sql"

    version := dbVersion(engine, target.Spec)
    availability := "ZONAL"
    if b, _ := target.Spec["multiAZ"].(bool); b {
        availability = "REGIONAL"
    }

    return []ir.ResourcePrimitive{
        {
            ID: fmt.Sprintf("%s.gcp-%s.instance", component, target.Region),
            Cloud: "gcp", TofuType: "google_sql_database_instance", TofuName: name,
            Attributes: map[string]any{
                "name": instanceName,
                "region": target.Region,
                "database_version": version,
                "settings": []any{map[string]any{
                    "tier": tier,
                    "availability_type": availability,
                    "disk_size": storageGB,
                    "disk_type": "PD_SSD",
                }},
                "deletion_protection": false,
            },
        },
        {
            ID: fmt.Sprintf("%s.gcp-%s.db_default", component, target.Region),
            Cloud: "gcp", TofuType: "google_sql_database", TofuName: name + "_default",
            Attributes: map[string]any{
                "name": "default",
                "instance": "${google_sql_database_instance." + name + ".name}",
            },
        },
    }, nil
}

func dbVersion(engine string, spec map[string]any) string {
    if v, _ := spec["version"].(string); v != "" { return v }
    switch engine {
    case "postgres": return "POSTGRES_16"
    case "mysql":    return "MYSQL_8_0"
    }
    return ""
}
```

Add errors in `adapter.go` or a new `errors.go`:

```go
var (
    ErrAdapterGCPMariaDBUnsupported  = errors.New("Cloud SQL does not offer MariaDB; use postgres or mysql")
    ErrAdapterGCPUnsupportedEngine   = errors.New("unsupported database engine for GCP")
)
```

- [ ] **Step 2: Test** — postgres + mysql produce correct database_version; mariadb returns ErrAdapterGCPMariaDBUnsupported; size mapping; multiAZ → availability_type REGIONAL.

- [ ] **Step 3: Replace stub; build + commit** `gcp: database emit (Cloud SQL + default DB)`

---

## Task 5: Storage emit (GCS Bucket)

**Files:**
- Create: `internal/cloud/gcp/storage.go`
- Create: `internal/cloud/gcp/storage_test.go`

- [ ] **Step 1: Implement emitStorage** — exactly one `google_storage_bucket` per component (GCS has no container sub-resource). Helpers:

```go
func deriveBucketName(project, component, region string) string {
    parts := []string{}
    if project != "" { parts = append(parts, sanitizeBucketPart(project)) }
    parts = append(parts, sanitizeBucketPart(component))
    parts = append(parts, sanitizeBucketPart(region))
    base := strings.Join(parts, "-")
    if len(base) > 50 { base = base[:50] }
    sum := sha256.Sum256([]byte(component + ":" + region + ":" + project))
    suffix := hex.EncodeToString(sum[:])[:6]
    return base + "-" + suffix
}

func sanitizeBucketPart(s string) string {
    out := strings.Builder{}
    for _, c := range strings.ToLower(s) {
        if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
            out.WriteRune(c)
        } else if c == '-' || c == '_' {
            out.WriteRune('-')
        }
    }
    return strings.Trim(out.String(), "-")
}
```

Defaults: `versioning {enabled = true}`, `uniform_bucket_level_access = true`, `public_access_prevention = "enforced"`, `storage_class = "STANDARD"`, `location = strings.ToUpper(target.Region)`.

- [ ] **Step 2: Test** — single primitive; derived name length ≤63; user-provided name accepted; versioning toggle; publicAccess=allowed→prevention="inherited".

- [ ] **Step 3: Replace stub; build + commit** `gcp: storage emit (GCS bucket)`

---

## Task 6: PricingKey real impl

**Files:**
- Create: `internal/cloud/gcp/pricing.go`
- Create: `internal/cloud/gcp/pricing_test.go`

- [ ] **Step 1: Implement pricingKeyImpl** returning canonical descriptor maps per primitive class:

```go
func pricingKeyImpl(p ir.ResourcePrimitive) (map[string]any, error) {
    region := regionFromID(p.ID)
    switch p.TofuType {
    case "google_compute_instance":
        machineType, _ := p.Attributes["machine_type"].(string)
        return map[string]any{
            "service":        "Compute Engine",
            "resourceFamily": "Compute",
            "resourceGroup":  machineFamilyGroup(machineType),
            "usageType":      "OnDemand",
            "machineType":    machineType,
            "region":         region,
        }, nil
    case "google_sql_database_instance":
        tier, engine := sqlTierAndEngine(p)
        return map[string]any{
            "service":        "Cloud SQL",
            "resourceFamily": "ApplicationServices",
            "resourceGroup":  sqlResourceGroup(tier),
            "usageType":      "OnDemand",
            "tier":           tier,
            "region":         region,
            "engine":         engine,
        }, nil
    case "google_storage_bucket":
        return map[string]any{
            "service":        "Cloud Storage",
            "resourceFamily": "Storage",
            "resourceGroup":  "StandardStorage",
            "usageType":      "OnDemand",
            "storageClass":   "STANDARD",
            "region":         region,
        }, nil
    }
    return nil, nil
}
```

Helpers: `regionFromID` extracts the `gcp-<region>` segment from `p.ID`; `machineFamilyGroup` maps `e2-*` → `"E2"`, `n2-*` → `"N2"`; `sqlTierAndEngine` reads `settings[0].tier` and `database_version`, returns `tier` plus normalized engine (`"POSTGRES_16"` → `"POSTGRES"`); `sqlResourceGroup` maps `db-f1-micro` → `"SQLGen2InstancesF1Micro"`, `db-g1-small` → `"SQLGen2InstancesG1Small"`, `db-custom-*` → `"SQLGen2InstancesCustom"`.

- [ ] **Step 2: Test** covering all three real-key shapes plus the free-primitive set (`google_compute_network`, `google_compute_subnetwork`, `google_compute_firewall`, `google_sql_database`) → `(nil, nil)`.

- [ ] **Step 3: Rewrite stubs.go** to delegate `PricingKey` to `pricingKeyImpl`. Remove unused imports.

- [ ] **Step 4: Build + commit** `gcp: PricingKey real impls per primitive class`

---

## Task 7: Profile real impl

**Files:**
- Create: `internal/cloud/gcp/profile.go`
- Create: `internal/cloud/gcp/profile_test.go`

- [ ] **Step 1: Implement profileImpl** returning `parity.ResourceProfile`:

```go
func profileImpl(p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
    switch p.TofuType {
    case "google_compute_instance":
        mt, _ := p.Attributes["machine_type"].(string)
        prof, ok := lookupMachineProfile(mt)
        if !ok { return parity.ResourceProfile{}, cloud.ErrProfileUnavailable }
        return parity.ResourceProfile{Class: "compute", Compute: &prof}, nil
    case "google_sql_database_instance":
        // ... read tier from settings; return Class=database with Compute + Storage + features
    case "google_storage_bucket":
        return parity.ResourceProfile{
            Class: "storage",
            Storage: &parity.StorageProfile{Class: "object"},
        }, nil
    }
    return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
}

var machineProfiles = map[string]parity.ComputeProfile{
    "e2-small":      {VCPU: 2, MemoryGB: 2, Architecture: "x86_64"},
    "e2-medium":     {VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
    "e2-standard-2": {VCPU: 2, MemoryGB: 8, Architecture: "x86_64"},
    "n2-standard-4": {VCPU: 4, MemoryGB: 16, Architecture: "x86_64"},
}
```

Cloud SQL tier → compute profile:

```go
var sqlComputeProfiles = map[string]parity.ComputeProfile{
    "db-f1-micro":      {VCPU: 1, MemoryGB: 0.6, Architecture: "x86_64"},  // shared
    "db-g1-small":      {VCPU: 1, MemoryGB: 1.7, Architecture: "x86_64"},  // shared
    "db-custom-2-7680": {VCPU: 2, MemoryGB: 7.5, Architecture: "x86_64"},
    "db-custom-4-15360":{VCPU: 4, MemoryGB: 15, Architecture: "x86_64"},
}
```

- [ ] **Step 2: Test** — compute profiles per machine type; database profiles per tier; storage class = "object"; unknown tier → ErrProfileUnavailable.

- [ ] **Step 3: Rewrite stubs.go** so `Profile` delegates to `profileImpl`. Remove unused imports.

- [ ] **Step 4: Build + commit** `gcp: Profile real impls per primitive class`

---

## Task 8: GCP pricing snapshot

**Files:**
- Create: `pkg/cost/pricing/snapshot/gcp.json` (~50 rows)

- [ ] **Step 1: Author snapshot** covering:
  - Compute Engine: e2-small @ $0.01675/hr, e2-medium @ $0.0335/hr, e2-standard-2 @ $0.067/hr, n2-standard-4 @ $0.1942/hr × {us-central1, us-east1, europe-west1} × {Linux}
  - Cloud SQL: db-f1-micro @ $0.0150/hr, db-g1-small @ $0.0500/hr, db-custom-2-7680 @ $0.1018/hr, db-custom-4-15360 @ $0.2036/hr × same regions × {ZONAL, REGIONAL doubles the rate}
  - Cloud Storage: STANDARD @ $0.020/GB-Mo × same regions

Each row shape:
```json
{
  "key": {
    "service": "Compute Engine",
    "resourceFamily": "Compute",
    "resourceGroup": "E2",
    "usageType": "OnDemand",
    "machineType": "e2-small",
    "region": "us-central1"
  },
  "pricePerUnit": 0.01675,
  "unit": "Hr",
  "currency": "USD",
  "source": "Cloud Billing Catalog snapshot (2026-05-16, manual curation)"
}
```

- [ ] **Step 2: Verify cost cache loads it** via existing `pkg/cost/pricing/cache_test.go` (which auto-discovers all snapshot files); no test changes needed if cache loader is glob-based; if not, add `pkg/cost/pricing/cache_test.go::TestCache_LoadsGCPSnapshot` that calls `cache.Lookup` against a GCP key.

- [ ] **Step 3: Build + commit** `cost: GCP pricing snapshot for Phase 5 SKUs`

---

## Task 9: Plugin contract test for GCP

**Files:**
- Create: `pkg/plugin/contract/gcp_test.go`

- [ ] **Step 1: Add test** that runs `contract.RunProvisionerScenarios(t, gcp.New(), sample)` with a sample `gcp/us-central1` network target.

- [ ] **Step 2: Build + commit** `contract: run provisioner scenarios against GCP adapter`

---

## Task 10: CLI registers GCP

**Files:**
- Edit: `cmd/cli/clouds.go`

- [ ] **Step 1: Extend defaultCloudRegistry**:

```go
import "github.com/klehmer/nimbusfab/internal/cloud/gcp"

func defaultCloudRegistry() (cloud.Registry, error) {
    reg := cloud.NewRegistry()
    if err := reg.Register(aws.New()); err != nil { return nil, err }
    if err := reg.Register(azure.New()); err != nil { return nil, err }
    if err := reg.Register(gcp.New()); err != nil { return nil, err }
    return reg, nil
}
```

- [ ] **Step 2: Run `go build ./cmd/cli/`** — all 6 production CLI files automatically pick up GCP.

- [ ] **Step 3: Build + commit** `cli: register GCP adapter in defaultCloudRegistry`

---

## Task 11: Full-stack fixture goes 3-way + cost UnitsFor extension

**Files:**
- Edit: `cmd/cli/testdata/full-stack-project/components/web-network.yaml`
- Edit: `cmd/cli/testdata/full-stack-project/components/orders-db.yaml`
- Edit: `cmd/cli/testdata/full-stack-project/components/web-app.yaml`
- Edit: `cmd/cli/testdata/full-stack-project/components/uploads.yaml`
- Edit: `pkg/cost/estimator/usage.go`
- Edit: `cmd/cli/full_stack_test.go`
- Edit: `cmd/cli/parity_test.go`
- Edit: `cmd/cli/cost_test.go`

- [ ] **Step 1: Add gcp/us-central1 target to each component** alongside the existing aws + azure targets. The DB component uses `engine: postgres` (Cloud SQL supports it; mariadb fixture would fail GCP, so keep postgres). Result: 4 components × 3 clouds = 12 targets.

- [ ] **Step 2: Extend UnitsFor** in `pkg/cost/estimator/usage.go`:

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

- [ ] **Step 3: Update assertions** in `full_stack_test.go`, `parity_test.go`, `cost_test.go` to expect 12 targets / 3-cloud breakdown where they previously expected 8/2.

- [ ] **Step 4: Run full CLI test suite** `go test ./cmd/cli/ -v` — all green.

- [ ] **Step 5: Build + commit** `cli: full-stack fixture multi-cloud → 3 clouds + GCP usage assumptions`

---

## Task 12: Docs

**Files:**
- Edit: `README.md`
- Edit: `CHANGELOG.md`

- [ ] **Step 1: Update README** status line to reflect GCP Phase 5 merged.

- [ ] **Step 2: Add CHANGELOG entry** under a new "Phase 5: GCP Adapter" section listing: gcp package, per-type emit, machine-type mappings, pricing snapshot, contract scenarios, CLI registration, fixture goes 3-way.

- [ ] **Step 3: Run final full test suite**:

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
```

All packages green.

- [ ] **Step 4: gofmt check**:

```bash
PATH=$HOME/.local/go/bin:$PATH gofmt -l internal/cloud/gcp/ pkg/cost/ cmd/cli/
```

(should print nothing)

- [ ] **Step 5: Commit** `docs: GCP Phase 5 merged — 3-cloud parity + cost live`

---

## Merge

```bash
cd /home/kurt/git/nimbusfab
git checkout main
git merge --no-ff feat/gcp-phase5 -m "Merge feat/gcp-phase5: GCP adapter + 3-cloud full-stack demo"
git push origin main
git push origin feat/gcp-phase5
```
