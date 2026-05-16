package azure_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitStorage_BasicShape(t *testing.T) {
	a := azure.New()
	prims, err := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "storage", "__component": "uploads"},
	}, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	count := map[string]int{}
	for _, p := range prims {
		count[p.TofuType]++
	}
	for k, want := range map[string]int{
		"azurerm_resource_group":    1,
		"azurerm_storage_account":   1,
		"azurerm_storage_container": 1,
	} {
		if count[k] != want {
			t.Errorf("%s count = %d, want %d", k, count[k], want)
		}
	}
}

func TestEmitStorage_AccountNameDeterministic(t *testing.T) {
	a := azure.New()
	target := ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "storage", "__component": "uploads"},
	}
	p1, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	p2, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	var n1, n2 string
	for _, p := range p1 {
		if p.TofuType == "azurerm_storage_account" {
			n1 = p.Attributes["name"].(string)
		}
	}
	for _, p := range p2 {
		if p.TofuType == "azurerm_storage_account" {
			n2 = p.Attributes["name"].(string)
		}
	}
	if n1 == "" || n1 != n2 {
		t.Errorf("name non-deterministic: %q vs %q", n1, n2)
	}
	if len(n1) < 3 || len(n1) > 24 {
		t.Errorf("name length out of bounds: %q (%d)", n1, len(n1))
	}
}

func TestEmitStorage_NameRespectsAzureCharRules(t *testing.T) {
	a := azure.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "storage", "__component": "user-uploads-2026"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType == "azurerm_storage_account" {
			name := p.Attributes["name"].(string)
			for _, c := range name {
				if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
					t.Errorf("storage account name %q contains invalid char %c", name, c)
				}
			}
		}
	}
}
