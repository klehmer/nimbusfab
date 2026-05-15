package tofu

import "testing"

func TestExecRunner_DefaultBinaryName(t *testing.T) {
	e := NewExecRunner()
	if e.bin() != "tofu" {
		t.Errorf("default bin = %q, want \"tofu\"", e.bin())
	}
	e.Binary = "/usr/local/bin/tofu"
	if e.bin() != "/usr/local/bin/tofu" {
		t.Errorf("custom bin = %q", e.bin())
	}
}

func TestPlanHasChanges_DetectsCreate(t *testing.T) {
	j := []byte(`{"resource_changes":[{"address":"aws_vpc.x","change":{"actions":["create"]}}]}`)
	if !planHasChanges(j) {
		t.Error("planHasChanges = false, want true for create")
	}
}

func TestPlanHasChanges_NoOpOnly(t *testing.T) {
	j := []byte(`{"resource_changes":[{"address":"aws_vpc.x","change":{"actions":["no-op"]}}]}`)
	if planHasChanges(j) {
		t.Error("planHasChanges = true, want false for no-op only")
	}
}

func TestPlanHasChanges_Empty(t *testing.T) {
	if planHasChanges([]byte(`{"resource_changes":[]}`)) {
		t.Error("empty resource_changes -> true, want false")
	}
}
