# Azure Adapter Phase 4 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans`. Steps use `- [ ]` checkboxes.

**Goal:** Working Azure adapter that mirrors AWS Phase 3 for the four v1 component types. After Phase 4, `nimbusfab plan` against a project with `azure/eastus` targets produces real Azure Tofu workspaces; cross-cloud projects (AWS + Azure) light up the parity engine and cost estimator with non-trivial multi-cloud reports.

**Architecture:** `internal/cloud/azure/{adapter,emit,network,compute,database,storage,pricing,profile,schema}.go` matching AWS Phase 3's layout 1:1. CLI auto-registers Azure adapter alongside AWS. Pricing snapshot adds `pkg/cost/pricing/snapshot/azure.json` covering the Phase-4 SKUs.

**Conventions:**
- All paths relative to `/home/kurt/git/nimbusfab-azure-phase4/`.
- `PATH=$HOME/.local/go/bin:$PATH` for go commands.
- The Bash `cd` persists between calls — stay in the worktree.
- One commit per task.

**Out of scope:**
- LocalStack-equivalent live integration (Azurite covers storage but not VMs / RDS).
- Managed identities / RBAC / Application Gateway / Spot VMs / Scale Sets / Azure SQL.
- Tier-1 `<cloud>: azure:` escape hatch schemas.
- GCP adapter (separate phase).

---

## Task 1: Adapter scaffold + Emit dispatch shim + per-type stubs

**Files:**
- Create: `internal/cloud/azure/adapter.go`
- Create: `internal/cloud/azure/emit.go`
- Create: `internal/cloud/azure/schema.go`
- Create: `internal/cloud/azure/schema/v1alpha1/tier_one.json`
- Create: `internal/cloud/azure/stubs.go` (per-type stubs replaced by Tasks 2-5)
- Create: `internal/cloud/azure/adapter_test.go`

- [ ] **Step 1: Adapter scaffold**

Create `internal/cloud/azure/adapter.go`:

```go
// Package azure implements pkg/cloud.Adapter for Microsoft Azure. Phase 4
// supports the four v1 component types via the hashicorp/azurerm provider.
package azure

import (
    "context"
    "fmt"
    "time"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

// Adapter is the Azure implementation of cloud.Adapter.
type Adapter struct{}

// New returns a configured Azure Adapter.
func New() *Adapter { return &Adapter{} }

var _ cloud.Adapter = (*Adapter)(nil)

func (*Adapter) Name() string                      { return "azure" }
func (*Adapter) SupportedAPIVersions() []string    { return []string{ir.APIVersionV1Alpha1} }
func (*Adapter) SupportedComponentTypes() []string {
    return []string{"network", "compute", "database", "storage"}
}
func (*Adapter) TierOneSchema() []byte { return tierOneSchema }

func (*Adapter) Validate(ctx context.Context, target ir.DeploymentTarget) []ir.Issue {
    if target.Region == "" {
        return []ir.Issue{{
            Severity: ir.SeverityError, Code: "ErrAdapterAzureRegionInvalid",
            Message: "Azure targets must declare a region (use Azure location names like 'eastus', not AWS-style 'us-east-1')",
            Path:    "target.region",
        }}
    }
    // Reject AWS-style names (contain hyphens like us-east-1).
    if hasHyphen(target.Region) {
        return []ir.Issue{{
            Severity: ir.SeverityError, Code: "ErrAdapterAzureRegionInvalid",
            Message:  fmt.Sprintf("Azure region %q looks like an AWS region name; use Azure format (e.g. 'eastus' not 'us-east-1')", target.Region),
            Path:     "target.region",
        }}
    }
    return nil
}

func hasHyphen(s string) bool {
    for i := 0; i < len(s); i++ {
        if s[i] == '-' {
            return true
        }
    }
    return false
}

func (*Adapter) DefaultStateBackend(ctx context.Context, target ir.DeploymentTarget) (ir.StateBackend, error) {
    return ir.StateBackend{
        Kind: "azurerm",
        Config: map[string]any{
            "resource_group_name":  "nimbusfab-state",
            "storage_account_name": "nimbusfabstate",
            "container_name":       "terraform-state",
            "key":                  fmt.Sprintf("azure/%s/terraform.tfstate", target.Region),
        },
    }, nil
}

func (*Adapter) ProviderBlock(ctx context.Context, target ir.DeploymentTarget, _ cloud.Credentials) (map[string]any, error) {
    return map[string]any{
        "azurerm": map[string]any{
            "features": map[string]any{},
        },
    }, nil
}

// BillingQuery / FetchBilling stubs until Cost Collector phase.

func (*Adapter) BillingQuery(ctx context.Context, _ cloud.Credentials, _, _ time.Time) (cloud.BillingQueryParams, error) {
    return nil, cloud.ErrNotImplementedYet
}

func (*Adapter) FetchBilling(ctx context.Context, _ cloud.Credentials, _ cloud.BillingQueryParams) ([]cloud.NormalizedCostRow, error) {
    return nil, cloud.ErrNotImplementedYet
}
```

- [ ] **Step 2: Tier-one schema (empty pass-through)**

Create `internal/cloud/azure/schema.go`:

```go
package azure

import _ "embed"

//go:embed schema/v1alpha1/tier_one.json
var tierOneSchema []byte
```

Create `internal/cloud/azure/schema/v1alpha1/tier_one.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://nimbusfab.dev/schema/cloud/azure/v1alpha1/tier_one.json",
  "title": "AzureTargetSpec",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "tags": {
      "type": "object",
      "additionalProperties": { "type": "string" },
      "description": "Per-target additional resource tags."
    }
  }
}
```

- [ ] **Step 3: Emit dispatch + per-type stubs**

Create `internal/cloud/azure/emit.go`:

```go
package azure

import (
    "context"
    "fmt"
    "regexp"
    "strings"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

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
    default:
        return nil, fmt.Errorf("azure: unsupported component type %q", compType)
    }
}

// tofuIdent sanitizes a component name to a Tofu-safe local identifier.
// Same shape as AWS adapter's helper but kept local to avoid cross-cloud import.
var tofuIdentRe = regexp.MustCompile(`[^a-z0-9_]`)

func tofuIdent(s string) string {
    s = strings.ToLower(s)
    s = strings.ReplaceAll(s, "-", "_")
    s = tofuIdentRe.ReplaceAllString(s, "_")
    if s == "" || (s[0] >= '0' && s[0] <= '9') {
        s = "_" + s
    }
    return s
}

// resourceGroupName derives a stable RG name from (component, region).
func resourceGroupName(component, region string) string {
    return "nimbusfab-" + component + "-" + region
}
```

Create `internal/cloud/azure/stubs.go` (replaced by Tasks 2-5):

```go
package azure

import (
    "context"
    "fmt"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) emitNetwork(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    return nil, fmt.Errorf("azure: network emit not yet implemented")
}
func (*Adapter) emitCompute(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    return nil, fmt.Errorf("azure: compute emit not yet implemented")
}
func (*Adapter) emitDatabase(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    return nil, fmt.Errorf("azure: database emit not yet implemented")
}
func (*Adapter) emitStorage(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    return nil, fmt.Errorf("azure: storage emit not yet implemented")
}
```

- [ ] **Step 4: Profile + PricingKey stubs (real impls in later tasks)**

Add to `adapter.go`:

```go
func (*Adapter) Profile(ctx context.Context, p ir.ResourcePrimitive) (parity.ResourceProfile, error) {
    return parity.ResourceProfile{}, cloud.ErrProfileUnavailable
}

func (*Adapter) PricingKey(ctx context.Context, p ir.ResourcePrimitive) (map[string]any, error) {
    return nil, nil
}
```

Tasks 6-7 replace these with real impls.

- [ ] **Step 5: Adapter scaffold tests**

Create `internal/cloud/azure/adapter_test.go`:

```go
package azure_test

import (
    "context"
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/azure"
    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_NameAndSupport(t *testing.T) {
    a := azure.New()
    if a.Name() != "azure" {
        t.Errorf("Name = %q", a.Name())
    }
    types := a.SupportedComponentTypes()
    if len(types) != 4 {
        t.Errorf("Supported types = %v, want 4", types)
    }
}

func TestAdapter_ValidateRejectsAWSStyleRegion(t *testing.T) {
    a := azure.New()
    issues := a.Validate(context.Background(), ir.DeploymentTarget{Region: "us-east-1"})
    if len(issues) == 0 {
        t.Error("expected validation issue for AWS-style region name")
    }
}

func TestAdapter_ValidateAcceptsAzureRegion(t *testing.T) {
    a := azure.New()
    issues := a.Validate(context.Background(), ir.DeploymentTarget{Region: "eastus"})
    if len(issues) != 0 {
        t.Errorf("unexpected issues for valid region: %v", issues)
    }
}

func TestAdapter_DefaultStateBackend(t *testing.T) {
    a := azure.New()
    sb, err := a.DefaultStateBackend(context.Background(), ir.DeploymentTarget{Region: "eastus"})
    if err != nil {
        t.Fatalf("DefaultStateBackend: %v", err)
    }
    if sb.Kind != "azurerm" {
        t.Errorf("Kind = %q, want azurerm", sb.Kind)
    }
}

func TestAdapter_ProviderBlock(t *testing.T) {
    a := azure.New()
    pb, _ := a.ProviderBlock(context.Background(), ir.DeploymentTarget{Region: "eastus"}, cloud.Credentials{})
    azBlock, ok := pb["azurerm"].(map[string]any)
    if !ok {
        t.Fatalf("missing azurerm block: %v", pb)
    }
    if _, ok := azBlock["features"]; !ok {
        t.Errorf("missing features block: %v", azBlock)
    }
}

func TestAdapter_Emit_UnsupportedType(t *testing.T) {
    a := azure.New()
    _, err := a.Emit(context.Background(), ir.DeploymentTarget{
        Cloud: "azure", Region: "eastus",
        Spec: map[string]any{"__type": "exotic"},
    }, cloud.ResolvedRefs{})
    if err == nil {
        t.Error("expected error for unsupported type")
    }
}
```

- [ ] **Step 6: Build + commit**

```bash
PATH=$HOME/.local/go/bin:$PATH go test ./internal/cloud/azure/ -v
git add internal/cloud/azure/
git commit -m "azure: adapter scaffold + Emit dispatch shim + per-type stubs"
```

---

## Task 2: Network emit (RG + VNet + Subnets + NSG)

**Files:**
- Create: `internal/cloud/azure/network.go` (replaces stub)
- Create: `internal/cloud/azure/network_test.go`

- [ ] **Step 1: Implement**

Replace the stub in `internal/cloud/azure/stubs.go`'s `emitNetwork` (move it to `network.go`):

```go
package azure

import (
    "context"
    "fmt"
    "net"

    "github.com/klehmer/nimbusfab/pkg/cloud"
    "github.com/klehmer/nimbusfab/pkg/ir"
)

func (*Adapter) emitNetwork(ctx context.Context, target ir.DeploymentTarget, refs cloud.ResolvedRefs) ([]ir.ResourcePrimitive, error) {
    cidr, _ := target.Spec["cidr"].(string)
    if cidr == "" {
        cidr = "10.0.0.0/16"
    }
    component, _ := target.Spec["__component"].(string)
    if component == "" {
        component = "network"
    }
    subnetCount := intFromSpec(target.Spec, "subnetCount", 3)
    if subnetCount < 1 {
        subnetCount = 1
    }
    name := tofuIdent(component)
    rgName := resourceGroupName(component, target.Region)

    subnetCIDRs, err := splitCIDR(cidr, subnetCount)
    if err != nil {
        return nil, fmt.Errorf("azure.emitNetwork: %w", err)
    }

    out := []ir.ResourcePrimitive{
        {
            ID:       fmt.Sprintf("%s.azure-%s.rg", component, target.Region),
            Cloud:    "azure",
            TofuType: "azurerm_resource_group",
            TofuName: name,
            Attributes: map[string]any{
                "name":     rgName,
                "location": target.Region,
            },
        },
        {
            ID:       fmt.Sprintf("%s.azure-%s.vnet", component, target.Region),
            Cloud:    "azure",
            TofuType: "azurerm_virtual_network",
            TofuName: name,
            Attributes: map[string]any{
                "name":                name + "-vnet",
                "address_space":       []any{cidr},
                "location":            target.Region,
                "resource_group_name": "${azurerm_resource_group." + name + ".name}",
            },
        },
        {
            ID:       fmt.Sprintf("%s.azure-%s.nsg", component, target.Region),
            Cloud:    "azure",
            TofuType: "azurerm_network_security_group",
            TofuName: name,
            Attributes: map[string]any{
                "name":                name + "-nsg",
                "location":            target.Region,
                "resource_group_name": "${azurerm_resource_group." + name + ".name}",
            },
        },
    }
    for i := 0; i < subnetCount; i++ {
        subnetName := fmt.Sprintf("%s_%d", name, i)
        out = append(out, ir.ResourcePrimitive{
            ID:       fmt.Sprintf("%s.azure-%s.subnet_%d", component, target.Region, i),
            Cloud:    "azure",
            TofuType: "azurerm_subnet",
            TofuName: subnetName,
            Attributes: map[string]any{
                "name":                 subnetName,
                "resource_group_name":  "${azurerm_resource_group." + name + ".name}",
                "virtual_network_name": "${azurerm_virtual_network." + name + ".name}",
                "address_prefixes":     []any{subnetCIDRs[i]},
            },
        })
    }
    return out, nil
}

func intFromSpec(spec map[string]any, key string, def int) int {
    if v, ok := spec[key]; ok {
        switch t := v.(type) {
        case int:
            return t
        case int64:
            return int(t)
        case float64:
            return int(t)
        }
    }
    return def
}

func splitCIDR(parent string, n int) ([]string, error) {
    _, ipNet, err := net.ParseCIDR(parent)
    if err != nil {
        return nil, fmt.Errorf("invalid CIDR %q: %w", parent, err)
    }
    ones, bits := ipNet.Mask.Size()
    if bits != 32 {
        return nil, fmt.Errorf("only IPv4 supported in Phase 4 (got %q)", parent)
    }
    newPrefix := ones + 8
    if newPrefix > 30 {
        newPrefix = 30
    }
    out := make([]string, n)
    base := ipNet.IP.To4()
    if base == nil {
        return nil, fmt.Errorf("not IPv4: %s", parent)
    }
    for i := 0; i < n; i++ {
        ip := []byte{base[0], base[1], byte(i), 0}
        mask := net.CIDRMask(newPrefix, 32)
        subnet := &net.IPNet{IP: ip, Mask: mask}
        out[i] = subnet.String()
    }
    return out, nil
}
```

Remove the `emitNetwork` stub from `stubs.go`.

- [ ] **Step 2: Test**

Create `internal/cloud/azure/network_test.go` covering: full primitive shape (RG + VNet + NSG + N subnets); determinism; custom subnet count.

- [ ] **Step 3: Build + commit**

---

## Task 3: Compute emit (VM + NIC + Public IP)

**Files:**
- Create: `internal/cloud/azure/compute.go`
- Create: `internal/cloud/azure/compute_test.go`

Per spec § "compute" emission. T-shirt → Azure VM SKU table:

```go
var computeSizes = map[string]string{
    "small":  "Standard_B2s",
    "medium": "Standard_B2ms",
    "large":  "Standard_B4ms",
    "xlarge": "Standard_D4s_v5",
}
```

Per-replica: NSG (shared), public IP, NIC, VM. Default image: Ubuntu 22.04 LTS (Canonical / 0001-com-ubuntu-server-jammy / 22_04-lts / latest).

Implementation pattern mirrors AWS Phase 3 compute.

- [ ] **Step 1: Implement**
- [ ] **Step 2: Test**
- [ ] **Step 3: Commit**

---

## Task 4: Database emit (PostgreSQL/MySQL Flexible Server + MariaDB classic)

**Files:**
- Create: `internal/cloud/azure/database.go`
- Create: `internal/cloud/azure/database_test.go`

T-shirt → Flexible Server SKU table per spec. Engine version defaults: postgres 16, mysql 8.0, mariadb 10.3.

Special-case mariadb to emit `azurerm_mariadb_server` (classic, not Flexible). The plan logs a `WarnMariaDBClassicDeprecated` issue at Validate time.

- [ ] **Step 1: Implement**
- [ ] **Step 2: Test**
- [ ] **Step 3: Commit**

---

## Task 5: Storage emit (Storage Account + Container)

**Files:**
- Create: `internal/cloud/azure/storage.go`
- Create: `internal/cloud/azure/storage_test.go`

Per spec § "storage" emission. Storage account name derivation: `lower(remove-dashes(component))[0:18] + sha8(component+region)[0:6]`, max 24 chars.

- [ ] **Step 1: Implement**
- [ ] **Step 2: Test**
- [ ] **Step 3: Commit**

---

## Task 6: PricingKey real impl

**Files:**
- Create: `internal/cloud/azure/pricing.go`
- Create: `internal/cloud/azure/pricing_test.go`

Per spec § "PricingKey shapes". Map per-primitive `TofuType` → Azure Retail Prices API key. Free primitives (RG, VNet, Subnet, NIC, NSG) return `(nil, nil)`.

`regionFromID` extracts region from primitive ID `"<component>.azure-<region>.<localname>"`.

- [ ] **Step 1: Implement**
- [ ] **Step 2: Replace adapter.go's PricingKey stub** (delete from adapter.go)
- [ ] **Step 3: Test + commit**

---

## Task 7: Profile real impl

**Files:**
- Create: `internal/cloud/azure/profile.go`
- Create: `internal/cloud/azure/profile_test.go`

Per spec § "Profile shapes". `lookupComputeProfile` for Azure VM SKUs. `stripFlexibleServerPrefix` for DB SKUs (Standard_B2s, etc.).

```go
var knownVMSKUs = map[string]parity.ComputeProfile{
    "Standard_B2s":    {VCPU: 2, MemoryGB: 4, Architecture: "x86_64", NetworkGbps: 12.5},
    "Standard_B2ms":   {VCPU: 2, MemoryGB: 8, Architecture: "x86_64", NetworkGbps: 12.5},
    "Standard_B4ms":   {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 12.5},
    "Standard_D4s_v5": {VCPU: 4, MemoryGB: 16, Architecture: "x86_64", NetworkGbps: 12.5},
}
```

Replace adapter.go's Profile stub.

- [ ] **Step 1: Implement**
- [ ] **Step 2: Test + commit**

---

## Task 8: Azure pricing snapshot

**Files:**
- Create: `pkg/cost/pricing/snapshot/azure.json`

Per spec § "Pricing snapshot additions". Manually curated from Azure Retail Prices API or Azure pricing pages.

Sample VM prices (Linux pay-as-you-go, eastus, May 2026):
- Standard_B2s: $0.0416/hr
- Standard_B2ms: $0.0832/hr
- Standard_B4ms: $0.1664/hr
- Standard_D4s_v5: $0.1920/hr

Sample DB prices (PostgreSQL Flexible Server, eastus, Single Zone):
- Standard_B1ms: $0.034/hr
- Standard_B2s: $0.068/hr
- Standard_D2s_v3: $0.198/hr
- Standard_D4s_v3: $0.396/hr

Storage Account Standard_LRS eastus: $0.0184/GB-Mo.

Cover {eastus, eastus2, westeurope}.

- [ ] **Step 1: Write snapshot**
- [ ] **Step 2: Update `pkg/cost/pricing/snapshot/README.md` to note Azure additions**
- [ ] **Step 3: Test snapshot loads + Azure cache lookup succeeds**
- [ ] **Step 4: Commit**

---

## Task 9: Plugin contract test scenarios

**Files:**
- Create: `pkg/plugin/contract/azure_test.go`

```go
package contract_test

import (
    "testing"

    "github.com/klehmer/nimbusfab/internal/cloud/azure"
    "github.com/klehmer/nimbusfab/pkg/ir"
    "github.com/klehmer/nimbusfab/pkg/plugin/contract"
)

func TestAzureAdapter_ProvisionerContract(t *testing.T) {
    sample := ir.DeploymentTarget{
        Cloud: "azure", Region: "eastus",
        Spec: map[string]any{"cidr": "10.0.0.0/16", "__component": "web", "__type": "network"},
    }
    contract.RunProvisionerScenarios(t, azure.New(), sample)
}
```

- [ ] Run + commit.

---

## Task 10: CLI registers Azure adapter

**Files:**
- Modify: `cmd/cli/plan.go`, `apply.go`, `destroy.go`, `drift.go`, `parity.go`, `cost.go` — all create their own cloud.Registry and register `aws.New()`. Add `azure.New()` registration alongside in each.

A small helper at `cmd/cli/clouds.go` consolidates:

```go
package main

import (
    "github.com/klehmer/nimbusfab/internal/cloud/aws"
    "github.com/klehmer/nimbusfab/internal/cloud/azure"
    "github.com/klehmer/nimbusfab/pkg/cloud"
)

// defaultCloudRegistry returns a Registry populated with every in-tree adapter.
func defaultCloudRegistry() (cloud.Registry, error) {
    reg := cloud.NewRegistry()
    if err := reg.Register(aws.New()); err != nil {
        return nil, err
    }
    if err := reg.Register(azure.New()); err != nil {
        return nil, err
    }
    return reg, nil
}
```

Each CLI command replaces its inline `reg.Register(aws.New())` with `reg, err := defaultCloudRegistry()`.

- [ ] Implement helper + refactor 6 CLI files + commit.

---

## Task 11: Full-stack fixture update

**Files:**
- Modify: `cmd/cli/testdata/full-stack-project/components/web-network.yaml` (add azure/eastus target)
- Modify: `cmd/cli/testdata/full-stack-project/components/orders-db.yaml`
- Modify: `cmd/cli/testdata/full-stack-project/components/web-app.yaml`
- Modify: `cmd/cli/testdata/full-stack-project/components/uploads.yaml`

Each gets a second target:

```yaml
targets:
  - cloud: aws
    region: us-east-1
    credentialRef: aws-dev
  - cloud: azure
    region: eastus
    credentialRef: azure-dev
```

After this lands: 4 components × 2 clouds = 8 targets in the full-stack fixture. `nimbusfab plan` shows real per-cloud divergence; `nimbusfab parity` and `nimbusfab cost estimate` produce meaningful multi-cloud reports.

- [ ] Update + verify with `nimbusfab plan` + commit.

---

## Task 12: README + CHANGELOG

Update Status; add Azure section under Components / Commands. CHANGELOG entry for Azure Phase 4.

- [ ] **Step 1: Final verification**
```bash
PATH=$HOME/.local/go/bin:$PATH go test ./...
PATH=$HOME/.local/go/bin:$PATH go vet ./...
PATH=$HOME/.local/go/bin:$PATH gofmt -l .
```
- [ ] **Step 2: Commit + merge**

---

## Final state

The Azure adapter sits alongside AWS as a peer. Multi-cloud projects work: `targets: [{cloud: aws}, {cloud: azure}]` produces per-cloud Tofu workspaces. The parity engine reports real cross-cloud divergence (Azure B2s has more RAM than AWS t3.small, etc.). The cost estimator compares per-cloud subtotals. Foundation for GCP Phase 5.
