package gcp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestProfile_ComputeInstance(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "small"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		prof, err := a.Profile(context.Background(), p)
		if err != nil {
			t.Fatalf("Profile: %v", err)
		}
		if prof.Class != "compute" {
			t.Errorf("class = %q", prof.Class)
		}
		if prof.Compute == nil || prof.Compute.VCPU != 2 || prof.Compute.MemoryGB != 2 {
			t.Errorf("compute = %+v", prof.Compute)
		}
		if prof.SKU != "e2-small" {
			t.Errorf("SKU = %q", prof.SKU)
		}
	}
}

func TestProfile_ComputeInstanceXLarge(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "xlarge"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		prof, _ := a.Profile(context.Background(), p)
		if prof.Compute.VCPU != 4 || prof.Compute.MemoryGB != 16 {
			t.Errorf("xlarge compute = %+v", prof.Compute)
		}
	}
}

func TestProfile_CloudSQLPostgres(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "database", "__component": "orders", "engine": "postgres", "size": "small"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_sql_database_instance" {
			continue
		}
		prof, _ := a.Profile(context.Background(), p)
		if prof.Class != "database" {
			t.Errorf("class = %q", prof.Class)
		}
		if prof.Database.Engine != "postgres" {
			t.Errorf("engine = %q", prof.Database.Engine)
		}
		if prof.Database.Version != "POSTGRES_16" {
			t.Errorf("version = %q", prof.Database.Version)
		}
		if prof.SKU != "db-f1-micro" {
			t.Errorf("SKU = %q", prof.SKU)
		}
	}
}

func TestProfile_CloudSQLMultiAZ(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "database", "__component": "orders", "engine": "postgres", "size": "large", "multiAZ": true},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_sql_database_instance" {
			continue
		}
		prof, _ := a.Profile(context.Background(), p)
		if !prof.Database.HA {
			t.Errorf("HA = false, want true for multiAZ")
		}
		if !prof.Features["multiAZ"] {
			t.Errorf("Features[multiAZ] not set")
		}
	}
}

func TestProfile_StorageBucket(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "storage", "__component": "uploads"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_storage_bucket" {
			continue
		}
		prof, _ := a.Profile(context.Background(), p)
		if prof.Class != "storage" {
			t.Errorf("class = %q", prof.Class)
		}
		if prof.Storage.Class != "object" {
			t.Errorf("storage class = %q", prof.Storage.Class)
		}
		if !prof.Features["versioning"] {
			t.Errorf("versioning expected true")
		}
		if prof.Features["publicAccess"] {
			t.Errorf("publicAccess expected false by default")
		}
	}
}

func TestProfile_UnknownType(t *testing.T) {
	a := gcp.New()
	_, err := a.Profile(context.Background(), ir.ResourcePrimitive{
		TofuType: "google_compute_firewall", ID: "x.gcp-us-central1.y",
	})
	if !errors.Is(err, cloud.ErrProfileUnavailable) {
		t.Errorf("err = %v, want ErrProfileUnavailable", err)
	}
}
