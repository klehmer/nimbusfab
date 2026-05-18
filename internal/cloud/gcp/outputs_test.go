package gcp

import (
	"context"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestOutputBindings_GCPNetwork(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__component": "web-network", "__type": "network",
			"cidr": "10.0.0.0/16", "subnetCount": 2}}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, _ := a.OutputBindings(context.Background(), target, prim)
	vpcID, _ := got["vpc_id"].(string)
	if !strings.HasPrefix(vpcID, "${google_compute_network.") {
		t.Errorf("vpc_id: got %q", got["vpc_id"])
	}
	subnetIDs, ok := got["subnet_ids"].([]string)
	if !ok || len(subnetIDs) == 0 {
		t.Fatalf("subnet_ids should be non-empty []string, got %T: %v", got["subnet_ids"], got["subnet_ids"])
	}
	if !strings.HasPrefix(subnetIDs[0], "${google_compute_subnetwork.") {
		t.Errorf("subnet_ids[0]: got %q", subnetIDs[0])
	}
}
