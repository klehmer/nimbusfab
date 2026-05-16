package azure_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestProfile_VM(t *testing.T) {
	a := azure.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "medium"},
	}, cloud.ResolvedRefs{"subnetId": "s"})
	for _, p := range prims {
		if p.TofuType == "azurerm_linux_virtual_machine" {
			prof, err := a.Profile(context.Background(), p)
			if err != nil {
				t.Fatalf("Profile: %v", err)
			}
			if prof.Class != "compute" || prof.Compute == nil {
				t.Errorf("compute profile: %+v", prof)
			}
			if prof.Compute.VCPU != 2 || prof.Compute.MemoryGB != 8 {
				t.Errorf("Standard_B2ms expected VCPU=2 MemoryGB=8; got %+v", prof.Compute)
			}
		}
	}
}

func TestProfile_PostgresFlexible(t *testing.T) {
	a := azure.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": "small"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType == "azurerm_postgresql_flexible_server" {
			prof, _ := a.Profile(context.Background(), p)
			if prof.Class != "database" || prof.Database == nil {
				t.Errorf("database profile: %+v", prof)
			}
			if prof.Database.Engine != "postgres" {
				t.Errorf("engine = %v", prof.Database.Engine)
			}
			if !prof.Features["pointInTimeRestore"] {
				t.Errorf("Features missing PITR: %v", prof.Features)
			}
		}
	}
}

func TestProfile_StorageAccount(t *testing.T) {
	a := azure.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "storage", "__component": "x"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType == "azurerm_storage_account" {
			prof, _ := a.Profile(context.Background(), p)
			if prof.Class != "storage" {
				t.Errorf("class = %v", prof.Class)
			}
		}
	}
}

func TestProfile_FreePrimitives(t *testing.T) {
	a := azure.New()
	_, err := a.Profile(context.Background(), ir.ResourcePrimitive{TofuType: "azurerm_subnet"})
	if err == nil {
		t.Error("expected ErrProfileUnavailable for subnet")
	}
}
