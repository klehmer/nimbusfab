package gcp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func dbTarget(spec map[string]any) ir.DeploymentTarget {
	full := map[string]any{"__type": "database", "__component": "orders", "engine": "postgres", "size": "small"}
	for k, v := range spec {
		full[k] = v
	}
	return ir.DeploymentTarget{Cloud: "gcp", Region: "us-central1", Spec: full}
}

func TestEmitDatabase_Postgres(t *testing.T) {
	a := gcp.New()
	prims, err := a.Emit(context.Background(), dbTarget(nil), cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(prims) != 2 {
		t.Fatalf("len = %d, want 2", len(prims))
	}
	inst := prims[0]
	if inst.TofuType != "google_sql_database_instance" {
		t.Errorf("[0] = %q", inst.TofuType)
	}
	if inst.Attributes["database_version"] != "POSTGRES_16" {
		t.Errorf("version = %v", inst.Attributes["database_version"])
	}
	settings := inst.Attributes["settings"].([]any)[0].(map[string]any)
	if settings["tier"] != "db-f1-micro" {
		t.Errorf("tier = %v", settings["tier"])
	}
	if settings["availability_type"] != "ZONAL" {
		t.Errorf("availability_type = %v", settings["availability_type"])
	}
}

func TestEmitDatabase_MySQL(t *testing.T) {
	a := gcp.New()
	prims, err := a.Emit(context.Background(), dbTarget(map[string]any{"engine": "mysql", "size": "large"}), cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	inst := prims[0]
	if inst.Attributes["database_version"] != "MYSQL_8_0" {
		t.Errorf("version = %v", inst.Attributes["database_version"])
	}
	settings := inst.Attributes["settings"].([]any)[0].(map[string]any)
	if settings["tier"] != "db-custom-2-7680" {
		t.Errorf("tier = %v", settings["tier"])
	}
	if settings["disk_size"] != 100 {
		t.Errorf("disk_size = %v", settings["disk_size"])
	}
}

func TestEmitDatabase_MariaDBRejected(t *testing.T) {
	a := gcp.New()
	_, err := a.Emit(context.Background(), dbTarget(map[string]any{"engine": "mariadb"}), cloud.ResolvedRefs{})
	if err == nil {
		t.Fatal("expected error for mariadb engine")
	}
	if !errors.Is(err, gcp.ErrAdapterGCPMariaDBUnsupported) {
		t.Errorf("err = %v, want ErrAdapterGCPMariaDBUnsupported", err)
	}
}

func TestEmitDatabase_UnsupportedEngine(t *testing.T) {
	a := gcp.New()
	_, err := a.Emit(context.Background(), dbTarget(map[string]any{"engine": "oracle"}), cloud.ResolvedRefs{})
	if !errors.Is(err, gcp.ErrAdapterGCPUnsupportedEngine) {
		t.Errorf("err = %v, want ErrAdapterGCPUnsupportedEngine", err)
	}
}

func TestEmitDatabase_MultiAZ(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), dbTarget(map[string]any{"multiAZ": true}), cloud.ResolvedRefs{})
	settings := prims[0].Attributes["settings"].([]any)[0].(map[string]any)
	if settings["availability_type"] != "REGIONAL" {
		t.Errorf("availability_type = %v, want REGIONAL", settings["availability_type"])
	}
}

func TestEmitDatabase_CustomVersion(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), dbTarget(map[string]any{"version": "POSTGRES_15"}), cloud.ResolvedRefs{})
	if prims[0].Attributes["database_version"] != "POSTGRES_15" {
		t.Errorf("version = %v", prims[0].Attributes["database_version"])
	}
}
