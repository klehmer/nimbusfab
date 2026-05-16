package azure_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPricingKey_VM(t *testing.T) {
	a := azure.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "small"},
	}, cloud.ResolvedRefs{"subnetId": "s"})
	for _, p := range prims {
		if p.TofuType == "azurerm_linux_virtual_machine" {
			key, err := a.PricingKey(context.Background(), p)
			if err != nil {
				t.Fatalf("PricingKey: %v", err)
			}
			if key["service"] != "VirtualMachines" {
				t.Errorf("service = %v", key["service"])
			}
			if key["armSkuName"] != "Standard_B2s" {
				t.Errorf("armSkuName = %v", key["armSkuName"])
			}
			if key["armRegionName"] != "eastus" {
				t.Errorf("armRegionName = %v", key["armRegionName"])
			}
		}
	}
}

func TestPricingKey_PostgresFlexible(t *testing.T) {
	a := azure.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": "medium"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType == "azurerm_postgresql_flexible_server" {
			key, _ := a.PricingKey(context.Background(), p)
			if key["service"] != "AzureDatabaseforPostgreSQL" {
				t.Errorf("service = %v", key["service"])
			}
			if key["tier"] != "Burstable" {
				t.Errorf("tier = %v, want Burstable for B2s", key["tier"])
			}
		}
	}
}

func TestPricingKey_FreePrimitives(t *testing.T) {
	a := azure.New()
	for _, tt := range []string{"azurerm_resource_group", "azurerm_virtual_network", "azurerm_subnet", "azurerm_network_security_group", "azurerm_network_interface", "azurerm_storage_container"} {
		key, err := a.PricingKey(context.Background(), ir.ResourcePrimitive{
			TofuType: tt, ID: "x.azure-eastus.y",
		})
		if err != nil || key != nil {
			t.Errorf("%s: expected nil/nil, got %v / %v", tt, key, err)
		}
	}
}
