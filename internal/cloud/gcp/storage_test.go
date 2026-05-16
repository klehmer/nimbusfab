package gcp_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func storageTarget(spec map[string]any) ir.DeploymentTarget {
	full := map[string]any{"__type": "storage", "__component": "uploads"}
	for k, v := range spec {
		full[k] = v
	}
	return ir.DeploymentTarget{Cloud: "gcp", Region: "us-central1", Spec: full}
}

func TestEmitStorage_DefaultShape(t *testing.T) {
	a := gcp.New()
	prims, err := a.Emit(context.Background(), storageTarget(nil), cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(prims) != 1 {
		t.Fatalf("len = %d, want 1 (GCS has no container sub-resource)", len(prims))
	}
	b := prims[0]
	if b.TofuType != "google_storage_bucket" {
		t.Errorf("type = %q", b.TofuType)
	}
	if b.Attributes["location"] != "US-CENTRAL1" {
		t.Errorf("location = %v", b.Attributes["location"])
	}
	if b.Attributes["storage_class"] != "STANDARD" {
		t.Errorf("storage_class = %v", b.Attributes["storage_class"])
	}
	if b.Attributes["uniform_bucket_level_access"] != true {
		t.Errorf("uniform_bucket_level_access = %v", b.Attributes["uniform_bucket_level_access"])
	}
	if b.Attributes["public_access_prevention"] != "enforced" {
		t.Errorf("public_access_prevention = %v", b.Attributes["public_access_prevention"])
	}
	versioning := b.Attributes["versioning"].([]any)[0].(map[string]any)
	if versioning["enabled"] != true {
		t.Errorf("versioning enabled = %v", versioning["enabled"])
	}
}

func TestEmitStorage_DerivedNameBounds(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), storageTarget(nil), cloud.ResolvedRefs{})
	name := prims[0].Attributes["name"].(string)
	if len(name) < 3 || len(name) > 63 {
		t.Errorf("derived name %q (len=%d) violates 3-63 char rule", name, len(name))
	}
}

func TestEmitStorage_DerivedNameDeterministic(t *testing.T) {
	a := gcp.New()
	first, _ := a.Emit(context.Background(), storageTarget(nil), cloud.ResolvedRefs{})
	second, _ := a.Emit(context.Background(), storageTarget(nil), cloud.ResolvedRefs{})
	if first[0].Attributes["name"] != second[0].Attributes["name"] {
		t.Errorf("non-deterministic name: %v vs %v", first[0].Attributes["name"], second[0].Attributes["name"])
	}
}

func TestEmitStorage_UserProvidedName(t *testing.T) {
	a := gcp.New()
	prims, err := a.Emit(context.Background(), storageTarget(map[string]any{"name": "my-bucket-123"}), cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if prims[0].Attributes["name"] != "my-bucket-123" {
		t.Errorf("name = %v", prims[0].Attributes["name"])
	}
}

func TestEmitStorage_VersioningDisabled(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), storageTarget(map[string]any{"versioning": false}), cloud.ResolvedRefs{})
	versioning := prims[0].Attributes["versioning"].([]any)[0].(map[string]any)
	if versioning["enabled"] != false {
		t.Errorf("versioning enabled = %v, want false", versioning["enabled"])
	}
}

func TestEmitStorage_PublicAccessAllowed(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), storageTarget(map[string]any{"publicAccess": "allowed"}), cloud.ResolvedRefs{})
	if prims[0].Attributes["public_access_prevention"] != "inherited" {
		t.Errorf("prevention = %v, want inherited", prims[0].Attributes["public_access_prevention"])
	}
}

func TestEmitStorage_NameTooShortRejected(t *testing.T) {
	a := gcp.New()
	_, err := a.Emit(context.Background(), storageTarget(map[string]any{"name": "ab"}), cloud.ResolvedRefs{})
	if err == nil {
		t.Error("expected error for too-short bucket name")
	}
}
