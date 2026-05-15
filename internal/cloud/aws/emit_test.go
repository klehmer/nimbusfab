package aws_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestAdapter_EmitNetworkVPC_Golden(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"cidr": "10.0.0.0/16", "__component": "web-network"},
	}
	primitives, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(primitives) != 1 {
		t.Fatalf("Emit returned %d primitives, want 1", len(primitives))
	}
	p := primitives[0]
	if p.TofuType != "aws_vpc" {
		t.Errorf("TofuType = %q, want \"aws_vpc\"", p.TofuType)
	}
	if got := p.Attributes["cidr_block"]; got != "10.0.0.0/16" {
		t.Errorf("cidr_block = %v, want 10.0.0.0/16", got)
	}
	gold, err := os.ReadFile(filepath.Join("testdata", "network_vpc.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	actual, _ := json.Marshal(p.Attributes)
	var goldData, actualData any
	_ = json.Unmarshal(gold, &goldData)
	_ = json.Unmarshal(actual, &actualData)
	goldBytes, _ := json.Marshal(goldData)
	actualBytes, _ := json.Marshal(actualData)
	if string(goldBytes) != string(actualBytes) {
		t.Errorf("emit attributes diverge from golden:\n got:  %s\n want: %s", actualBytes, goldBytes)
	}
}

func TestAdapter_EmitIsPure(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{Cloud: "aws", Region: "us-east-1", Spec: map[string]any{"cidr": "10.0.0.0/16"}}
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
		Spec:   map[string]any{"__component": "web-network-1"},
	}
	primitives, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if primitives[0].TofuName != "web_network_1" {
		t.Errorf("TofuName = %q, want \"web_network_1\"", primitives[0].TofuName)
	}
}
