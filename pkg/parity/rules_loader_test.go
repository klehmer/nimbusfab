package parity_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/parity"
)

func TestLoadRules_FullExample(t *testing.T) {
	body := []byte(`
apiVersion: parity.dev/v1alpha1
kind: ProjectParityRules
parity:
  default:
    mode: warn
    minScore: 0.7
  components:
    orders-db:
      mode: block
      minScore: 0.9
      attributes:
        compute.vCPU:
          policy: exact
        compute.memoryGB:
          policy: maxRatio
          maxRatio: 2.0
        features.pointInTimeRestore:
          policy: requireAll
    analytics-warehouse:
      mode: off
`)
	rules, err := parity.LoadRules(body)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if rules.Default.Mode != "warn" || rules.Default.MinScore != 0.7 {
		t.Errorf("default: %+v", rules.Default)
	}
	db := rules.Components["orders-db"]
	if db.Mode != "block" || db.MinScore != 0.9 {
		t.Errorf("orders-db: %+v", db)
	}
	if db.Attributes["compute.vCPU"].Policy != "exact" {
		t.Errorf("vCPU policy: %+v", db.Attributes)
	}
	if db.Attributes["compute.memoryGB"].MaxRatio != 2.0 {
		t.Errorf("memoryGB ratio: %+v", db.Attributes)
	}
	if rules.Components["analytics-warehouse"].Mode != "off" {
		t.Errorf("analytics-warehouse mode: %+v", rules.Components["analytics-warehouse"])
	}
}

func TestLoadRulesFromFile_MissingFileIsNoRules(t *testing.T) {
	rules, err := parity.LoadRulesFromFile("/nonexistent-path-deliberately/parity.yaml")
	if err != nil {
		t.Fatalf("LoadRulesFromFile: %v", err)
	}
	if rules.Default.Mode != "" || len(rules.Components) != 0 {
		t.Errorf("missing file should yield empty rules, got %+v", rules)
	}
}
