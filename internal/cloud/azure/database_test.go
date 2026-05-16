package azure_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitDatabase_PostgresShape(t *testing.T) {
	a := azure.New()
	prims, err := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "database", "__component": "orders", "engine": "postgres", "size": "small"},
	}, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var rg, server, db int
	for _, p := range prims {
		switch p.TofuType {
		case "azurerm_resource_group":
			rg++
		case "azurerm_postgresql_flexible_server":
			server++
		case "azurerm_postgresql_flexible_server_database":
			db++
		}
	}
	if rg != 1 || server != 1 || db != 1 {
		t.Errorf("counts: rg=%d server=%d db=%d", rg, server, db)
	}
}

func TestEmitDatabase_SizeMapping(t *testing.T) {
	cases := map[string]string{
		"small":  "Standard_B1ms",
		"medium": "Standard_B2s",
		"large":  "Standard_D2s_v3",
		"xlarge": "Standard_D4s_v3",
	}
	a := azure.New()
	for size, want := range cases {
		prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
			Cloud: "azure", Region: "eastus",
			Spec: map[string]any{"__type": "database", "__component": "db", "engine": "postgres", "size": size},
		}, cloud.ResolvedRefs{})
		for _, p := range prims {
			if p.TofuType == "azurerm_postgresql_flexible_server" {
				if p.Attributes["sku_name"] != want {
					t.Errorf("%s: sku_name = %v, want %v", size, p.Attributes["sku_name"], want)
				}
			}
		}
	}
}

func TestEmitDatabase_MariaDBClassic(t *testing.T) {
	a := azure.New()
	prims, _ := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "database", "__component": "old", "engine": "mariadb", "size": "small"},
	}, cloud.ResolvedRefs{})
	var found bool
	for _, p := range prims {
		if p.TofuType == "azurerm_mariadb_server" {
			found = true
		}
	}
	if !found {
		t.Error("expected azurerm_mariadb_server (classic) for engine: mariadb")
	}
}

func TestEmitDatabase_UnsupportedEngine(t *testing.T) {
	a := azure.New()
	_, err := a.Emit(context.Background(), ir.DeploymentTarget{
		Cloud: "azure", Region: "eastus",
		Spec: map[string]any{"__type": "database", "__component": "db", "engine": "oracle", "size": "small"},
	}, cloud.ResolvedRefs{})
	if err == nil {
		t.Error("expected error for unsupported engine")
	}
}
