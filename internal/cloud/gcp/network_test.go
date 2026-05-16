package gcp_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func networkTarget(spec map[string]any) ir.DeploymentTarget {
	full := map[string]any{"__type": "network", "__component": "web", "cidr": "10.0.0.0/16"}
	for k, v := range spec {
		full[k] = v
	}
	return ir.DeploymentTarget{Cloud: "gcp", Region: "us-central1", Spec: full}
}

func TestEmitNetwork_DefaultShape(t *testing.T) {
	a := gcp.New()
	prims, err := a.Emit(context.Background(), networkTarget(nil), cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	// 1 VPC + 3 subnets + 2 firewalls = 6
	if got := len(prims); got != 6 {
		t.Fatalf("len(prims) = %d, want 6", got)
	}
	counts := map[string]int{}
	for _, p := range prims {
		counts[p.TofuType]++
	}
	if counts["google_compute_network"] != 1 {
		t.Errorf("network count = %d", counts["google_compute_network"])
	}
	if counts["google_compute_subnetwork"] != 3 {
		t.Errorf("subnet count = %d", counts["google_compute_subnetwork"])
	}
	if counts["google_compute_firewall"] != 2 {
		t.Errorf("firewall count = %d", counts["google_compute_firewall"])
	}
}

func TestEmitNetwork_CustomSubnetCount(t *testing.T) {
	a := gcp.New()
	prims, err := a.Emit(context.Background(), networkTarget(map[string]any{"subnetCount": 2}), cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	// 1 VPC + 2 subnets + 2 firewalls = 5
	if got := len(prims); got != 5 {
		t.Fatalf("len = %d, want 5", got)
	}
}

func TestEmitNetwork_CIDRSplitting(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), networkTarget(nil), cloud.ResolvedRefs{})
	want := []string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"}
	got := []string{}
	for _, p := range prims {
		if p.TofuType == "google_compute_subnetwork" {
			got = append(got, p.Attributes["ip_cidr_range"].(string))
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d subnets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("subnet[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEmitNetwork_Determinism(t *testing.T) {
	a := gcp.New()
	first, _ := a.Emit(context.Background(), networkTarget(nil), cloud.ResolvedRefs{})
	second, _ := a.Emit(context.Background(), networkTarget(nil), cloud.ResolvedRefs{})
	if len(first) != len(second) {
		t.Fatalf("non-deterministic length")
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("primitive[%d] ID mismatch: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
}

func TestEmitNetwork_RegionInSubnetwork(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), networkTarget(nil), cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_compute_subnetwork" {
			continue
		}
		if p.Attributes["region"] != "us-central1" {
			t.Errorf("subnet region = %v", p.Attributes["region"])
		}
	}
}
