package gcp_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPricingKey_ComputeInstance(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "small"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		key, err := a.PricingKey(context.Background(), p)
		if err != nil {
			t.Fatalf("PricingKey: %v", err)
		}
		if key["service"] != "Compute Engine" {
			t.Errorf("service = %v", key["service"])
		}
		if key["resourceGroup"] != "E2" {
			t.Errorf("resourceGroup = %v, want E2", key["resourceGroup"])
		}
		if key["machineType"] != "e2-small" {
			t.Errorf("machineType = %v", key["machineType"])
		}
		if key["region"] != "us-central1" {
			t.Errorf("region = %v", key["region"])
		}
	}
}

func TestPricingKey_ComputeInstanceN2(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "compute", "__component": "web", "size": "xlarge"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_compute_instance" {
			continue
		}
		key, _ := a.PricingKey(context.Background(), p)
		if key["resourceGroup"] != "N2" {
			t.Errorf("resourceGroup = %v, want N2", key["resourceGroup"])
		}
	}
}

func TestPricingKey_CloudSQLPostgres(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "database", "__component": "orders", "engine": "postgres", "size": "small"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_sql_database_instance" {
			continue
		}
		key, _ := a.PricingKey(context.Background(), p)
		if key["service"] != "Cloud SQL" {
			t.Errorf("service = %v", key["service"])
		}
		if key["resourceGroup"] != "SQLGen2InstancesF1Micro" {
			t.Errorf("resourceGroup = %v", key["resourceGroup"])
		}
		if key["tier"] != "db-f1-micro" {
			t.Errorf("tier = %v", key["tier"])
		}
		if key["engine"] != "POSTGRES" {
			t.Errorf("engine = %v", key["engine"])
		}
	}
}

func TestPricingKey_CloudSQLMySQLCustomTier(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "us-central1",
		Spec: map[string]any{"__type": "database", "__component": "orders", "engine": "mysql", "size": "large"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_sql_database_instance" {
			continue
		}
		key, _ := a.PricingKey(context.Background(), p)
		if key["resourceGroup"] != "SQLGen2InstancesCustom" {
			t.Errorf("resourceGroup = %v", key["resourceGroup"])
		}
		if key["engine"] != "MYSQL" {
			t.Errorf("engine = %v", key["engine"])
		}
	}
}

func TestPricingKey_StorageBucket(t *testing.T) {
	a := gcp.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "gcp", Region: "europe-west1",
		Spec: map[string]any{"__type": "storage", "__component": "uploads"},
	}, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType != "google_storage_bucket" {
			continue
		}
		key, _ := a.PricingKey(context.Background(), p)
		if key["service"] != "Cloud Storage" {
			t.Errorf("service = %v", key["service"])
		}
		if key["resourceGroup"] != "StandardStorage" {
			t.Errorf("resourceGroup = %v", key["resourceGroup"])
		}
		if key["region"] != "europe-west1" {
			t.Errorf("region = %v, want europe-west1", key["region"])
		}
	}
}

func TestPricingKey_FreePrimitives(t *testing.T) {
	a := gcp.New()
	for _, tt := range []string{
		"google_compute_network",
		"google_compute_subnetwork",
		"google_compute_firewall",
		"google_sql_database",
	} {
		key, err := a.PricingKey(context.Background(), ir.ResourcePrimitive{
			TofuType: tt, ID: "x.gcp-us-central1.y",
		})
		if err != nil || key != nil {
			t.Errorf("%s: expected nil/nil, got %v / %v", tt, key, err)
		}
	}
}
