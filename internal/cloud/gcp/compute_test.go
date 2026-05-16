package gcp_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func computeTarget(spec map[string]any) ir.DeploymentTarget {
	full := map[string]any{"__type": "compute", "__component": "web", "size": "small"}
	for k, v := range spec {
		full[k] = v
	}
	return ir.DeploymentTarget{Cloud: "gcp", Region: "us-central1", Spec: full}
}

func TestEmitCompute_DefaultShape(t *testing.T) {
	a := gcp.New()
	prims, err := a.Emit(context.Background(), computeTarget(nil), cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	// 1 firewall + 1 instance (replicas=1)
	if got := len(prims); got != 2 {
		t.Fatalf("len = %d, want 2", got)
	}
	if prims[0].TofuType != "google_compute_firewall" {
		t.Errorf("[0] = %q", prims[0].TofuType)
	}
	if prims[1].TofuType != "google_compute_instance" {
		t.Errorf("[1] = %q", prims[1].TofuType)
	}
	if mt := prims[1].Attributes["machine_type"]; mt != "e2-small" {
		t.Errorf("machine_type = %v, want e2-small", mt)
	}
}

func TestEmitCompute_SizeMapping(t *testing.T) {
	a := gcp.New()
	for _, tc := range []struct {
		size string
		want string
	}{
		{"small", "e2-small"},
		{"medium", "e2-medium"},
		{"large", "e2-standard-2"},
		{"xlarge", "n2-standard-4"},
	} {
		t.Run(tc.size, func(t *testing.T) {
			prims, _ := a.Emit(context.Background(), computeTarget(map[string]any{"size": tc.size}), cloud.ResolvedRefs{})
			var got string
			for _, p := range prims {
				if p.TofuType == "google_compute_instance" {
					got = p.Attributes["machine_type"].(string)
				}
			}
			if got != tc.want {
				t.Errorf("size=%s machine_type=%q want=%q", tc.size, got, tc.want)
			}
		})
	}
}

func TestEmitCompute_ZoneDistribution(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), computeTarget(map[string]any{"replicas": 4}), cloud.ResolvedRefs{})
	want := []string{"us-central1-a", "us-central1-b", "us-central1-c", "us-central1-a"}
	idx := 0
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		if p.Attributes["zone"] != want[idx] {
			t.Errorf("instance[%d] zone = %v, want %s", idx, p.Attributes["zone"], want[idx])
		}
		idx++
	}
	if idx != 4 {
		t.Errorf("got %d instances, want 4", idx)
	}
}

func TestEmitCompute_CustomImage(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), computeTarget(map[string]any{"imageRef": "debian-cloud/debian-12"}), cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		bootDisk := p.Attributes["boot_disk"].([]any)[0].(map[string]any)
		init := bootDisk["initialize_params"].([]any)[0].(map[string]any)
		if init["image"] != "debian-cloud/debian-12" {
			t.Errorf("image = %v", init["image"])
		}
	}
}

func TestEmitCompute_CustomDiskSize(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), computeTarget(map[string]any{
		"storage": map[string]any{"sizeGB": 100},
	}), cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		bootDisk := p.Attributes["boot_disk"].([]any)[0].(map[string]any)
		init := bootDisk["initialize_params"].([]any)[0].(map[string]any)
		if init["size"] != 100 {
			t.Errorf("size = %v, want 100", init["size"])
		}
	}
}

func TestEmitCompute_NetworkRef(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), computeTarget(nil), cloud.ResolvedRefs{"subnetworkId": "projects/p/regions/us-central1/subnetworks/x"})
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		nic := p.Attributes["network_interface"].([]any)[0].(map[string]any)
		if nic["subnetwork"] == nil {
			t.Errorf("expected subnetwork in nic, got %v", nic)
		}
	}
}
