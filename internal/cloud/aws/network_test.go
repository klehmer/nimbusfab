package aws_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitNetwork_FullShape(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"__type": "network", "__component": "web-network", "cidr": "10.0.0.0/16"},
	}
	prims, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	byType := map[string]int{}
	keysByType := map[string]map[string]bool{}
	for _, p := range prims {
		byType[p.TofuType]++
		if keysByType[p.TofuType] == nil {
			keysByType[p.TofuType] = map[string]bool{}
		}
		for k := range p.Attributes {
			keysByType[p.TofuType][k] = true
		}
	}

	gold, err := os.ReadFile(filepath.Join("testdata", "network_full.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var want struct {
		PrimitivesByType map[string]struct {
			Count         int      `json:"count"`
			AttributeKeys []string `json:"attribute_keys"`
		} `json:"primitives_by_type"`
	}
	_ = json.Unmarshal(gold, &want)

	for typeName, expected := range want.PrimitivesByType {
		if byType[typeName] != expected.Count {
			t.Errorf("%s count = %d, want %d", typeName, byType[typeName], expected.Count)
		}
		for _, key := range expected.AttributeKeys {
			if !keysByType[typeName][key] {
				got := []string{}
				for k := range keysByType[typeName] {
					got = append(got, k)
				}
				sort.Strings(got)
				t.Errorf("%s missing attribute %q (have: %v)", typeName, key, got)
			}
		}
	}
}

func TestEmitNetwork_Deterministic(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16"},
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
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud:  "aws",
		Region: "us-east-1",
		Spec:   map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16", "subnetCount": 2},
	}
	prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	var subnets int
	for _, p := range prims {
		if p.TofuType == "aws_subnet" {
			subnets++
		}
	}
	if subnets != 2 {
		t.Errorf("subnet count = %d, want 2", subnets)
	}
}
