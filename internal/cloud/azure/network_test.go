package azure_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitNetwork_FullShape(t *testing.T) {
	a := azure.New()
	target := ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16"},
	}
	prims, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	byType := map[string]int{}
	for _, p := range prims {
		byType[p.TofuType]++
	}
	for typeName, want := range map[string]int{
		"azurerm_resource_group":         1,
		"azurerm_virtual_network":        1,
		"azurerm_network_security_group": 1,
		"azurerm_subnet":                 3,
	} {
		if byType[typeName] != want {
			t.Errorf("%s count = %d, want %d", typeName, byType[typeName], want)
		}
	}
}

func TestEmitNetwork_Deterministic(t *testing.T) {
	a := azure.New()
	target := ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16"},
	}
	a1, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	a2, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	j1, _ := json.Marshal(a1)
	j2, _ := json.Marshal(a2)
	if string(j1) != string(j2) {
		t.Errorf("non-deterministic:\n%s\nvs\n%s", j1, j2)
	}
}

func TestEmitNetwork_CustomSubnetCount(t *testing.T) {
	a := azure.New()
	target := ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16", "subnetCount": 2},
	}
	prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	var subnets int
	for _, p := range prims {
		if p.TofuType == "azurerm_subnet" {
			subnets++
		}
	}
	if subnets != 2 {
		t.Errorf("subnet count = %d, want 2", subnets)
	}
}
