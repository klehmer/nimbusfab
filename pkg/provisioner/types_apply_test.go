package provisioner

import "testing"

func TestRunStatus_Constants(t *testing.T) {
	if string(RunStatusSucceeded) != "succeeded" {
		t.Errorf("RunStatusSucceeded = %q", RunStatusSucceeded)
	}
	if string(RunStatusReverted) != "reverted" {
		t.Errorf("RunStatusReverted = %q", RunStatusReverted)
	}
}

func TestApplyStatus_Constants(t *testing.T) {
	if string(ApplyPartialFailure) != "partial_failure" {
		t.Errorf("ApplyPartialFailure = %q", ApplyPartialFailure)
	}
	if string(ApplyRollbackFailed) != "rollback_failed" {
		t.Errorf("ApplyRollbackFailed = %q", ApplyRollbackFailed)
	}
}

func TestStateSnapshot_ZeroValue(t *testing.T) {
	var s StateSnapshot
	if s.Resources != nil {
		t.Error("zero StateSnapshot should have nil Resources")
	}
}
