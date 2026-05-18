package aws

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestOutputBindings_Network(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__component": "web-network", "__type": "network",
			"cidr": "10.0.0.0/16", "subnetCount": 2},
	}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, err := a.OutputBindings(context.Background(), target, prim)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["vpc_id"] != "${aws_vpc.web_network.id}" {
		t.Errorf("vpc_id: got %q", got["vpc_id"])
	}
	wantSubnets := []string{"${aws_subnet.web_network_0.id}", "${aws_subnet.web_network_1.id}"}
	gotSubnets, ok := got["subnet_ids"].([]string)
	if !ok {
		t.Fatalf("subnet_ids should be []string, got %T: %v", got["subnet_ids"], got["subnet_ids"])
	}
	if len(gotSubnets) != len(wantSubnets) || gotSubnets[0] != wantSubnets[0] || gotSubnets[1] != wantSubnets[1] {
		t.Errorf("subnet_ids: got %v, want %v", gotSubnets, wantSubnets)
	}
}

func TestOutputBindings_Compute(t *testing.T) {
	a := New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__component": "web-app", "__type": "compute",
			"size": "small", "instanceCount": 1,
			"subnetId": "${var.upstream_web_network_subnet_ids[0]}"},
	}
	prim, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	got, err := a.OutputBindings(context.Background(), target, prim)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["security_group_id"] == "" {
		t.Errorf("security_group_id missing: %v", got)
	}
}
