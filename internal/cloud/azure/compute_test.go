package azure_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitCompute_BasicShape(t *testing.T) {
	a := azure.New()
	prims, err := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "medium", "replicas": 2},
	}, cloud.ResolvedRefs{"subnetId": "/subscriptions/x/.../subnets/foo"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	count := map[string]int{}
	for _, p := range prims {
		count[p.TofuType]++
	}
	for k, want := range map[string]int{
		"azurerm_resource_group":         1,
		"azurerm_network_security_group": 1,
		"azurerm_public_ip":              2,
		"azurerm_network_interface":      2,
		"azurerm_linux_virtual_machine":  2,
	} {
		if count[k] != want {
			t.Errorf("%s count = %d, want %d", k, count[k], want)
		}
	}
}

func TestEmitCompute_SizeMapping(t *testing.T) {
	cases := map[string]string{
		"small":  "Standard_B2s",
		"medium": "Standard_B2ms",
		"large":  "Standard_B4ms",
		"xlarge": "Standard_D4s_v5",
	}
	a := azure.New()
	for size, want := range cases {
		prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
			Cloud: "azure", Region: "eastus",
			Spec: map[string]any{"__type": "compute", "__component": "x", "size": size},
		}, cloud.ResolvedRefs{"subnetId": "s"})
		for _, p := range prims {
			if p.TofuType == "azurerm_linux_virtual_machine" {
				if p.Attributes["size"] != want {
					t.Errorf("%s: size = %v, want %v", size, p.Attributes["size"], want)
				}
			}
		}
	}
}

func TestEmitCompute_Azure_MissingSubnetIDRefErrors(t *testing.T) {
	a := azure.New()
	target := ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__component": "web-app", "__type": "compute",
			"__deployment_id": "dep-test",
			"size": "small", "replicas": 1},
	}
	_, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err == nil {
		t.Fatal("expected Emit to fail when subnetId ref is missing")
	}
	if !strings.Contains(err.Error(), "subnet") && !strings.Contains(err.Error(), "ref") {
		t.Errorf("error should mention missing ref; got: %v", err)
	}
}
