package loader

import (
	"context"
	"testing"
)

func TestLoadStackValues_Present(t *testing.T) {
	values, err := LoadStackValues(context.Background(), "testdata/multi-file", "dev")
	if err != nil {
		t.Fatalf("LoadStackValues: %v", err)
	}
	if values.Vars["aws_region"] != "us-east-1" {
		t.Errorf("vars[aws_region] = %v", values.Vars["aws_region"])
	}
	if values.Vars["db_size"] != "small" {
		t.Errorf("vars[db_size] = %v", values.Vars["db_size"])
	}
	if v, ok := values.Vars["pi_enabled"].(bool); !ok || v {
		t.Errorf("vars[pi_enabled] should be the bool false, got %v (%T)", values.Vars["pi_enabled"], values.Vars["pi_enabled"])
	}
	if len(values.DisabledComponents) != 1 || values.DisabledComponents[0] != "analytics-warehouse" {
		t.Errorf("DisabledComponents = %v", values.DisabledComponents)
	}
	if len(values.DisabledTargets) != 1 {
		t.Fatalf("DisabledTargets = %v, want 1", values.DisabledTargets)
	}
	if values.DisabledTargets[0].Component != "web-network" || values.DisabledTargets[0].Cloud != "azure" {
		t.Errorf("DisabledTargets[0] = %+v", values.DisabledTargets[0])
	}
}

func TestLoadStackValues_Absent(t *testing.T) {
	values, err := LoadStackValues(context.Background(), "testdata/multi-file", "no-such-stack")
	if err != nil {
		t.Fatalf("LoadStackValues: %v", err)
	}
	if values == nil {
		t.Fatal("expected empty StackValues, got nil")
	}
	if len(values.Vars) != 0 {
		t.Errorf("Vars should be empty when stack file missing, got %v", values.Vars)
	}
}
