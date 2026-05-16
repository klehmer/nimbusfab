package aws_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_EmitNetworkVPC_HasCorrectCIDR(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web-network", "__type": "network"},
	}
	primitives, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(primitives) < 1 {
		t.Fatalf("Emit returned 0 primitives")
	}
	var vpc *ir.ResourcePrimitive
	for i := range primitives {
		if primitives[i].TofuType == "aws_vpc" {
			vpc = &primitives[i]
			break
		}
	}
	if vpc == nil {
		t.Fatalf("no aws_vpc in primitives: %v", primitives)
	}
	if got := vpc.Attributes["cidr_block"]; got != "10.0.0.0/16" {
		t.Errorf("cidr_block = %v, want 10.0.0.0/16", got)
	}
}

func TestAdapter_EmitIsPure(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"cidr": "10.0.0.0/16", "__type": "network"},
	}
	a1, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	a2, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if len(a1) != len(a2) || a1[0].TofuName != a2[0].TofuName {
		t.Fatal("Emit not idempotent")
	}
	j1, _ := json.Marshal(a1)
	j2, _ := json.Marshal(a2)
	if string(j1) != string(j2) {
		t.Errorf("Emit nondeterministic:\n a1: %s\n a2: %s", j1, j2)
	}
}

func TestAdapter_EmitTofuNameIsSafe(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"__component": "web-network-1", "__type": "network"},
	}
	primitives, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	// First primitive should be the VPC; its TofuName should be sanitized.
	if primitives[0].TofuName != "web_network_1" {
		t.Errorf("TofuName = %q, want \"web_network_1\"", primitives[0].TofuName)
	}
}
