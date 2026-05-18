package azure

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestOutputBindings_AzureNetwork(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__component": "web-network", "__type": "network",
			"cidr": "10.0.0.0/16", "subnetCount": 2}}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, _ := a.OutputBindings(context.Background(), target, prim)
	if !strings.HasPrefix(got["vpc_id"], "${azurerm_virtual_network.") {
		t.Errorf("vpc_id: got %q", got["vpc_id"])
	}
	if !strings.HasPrefix(got["subnet_ids"], "[${azurerm_subnet.") {
		t.Errorf("subnet_ids: got %q", got["subnet_ids"])
	}
}
