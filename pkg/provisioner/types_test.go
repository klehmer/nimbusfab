package provisioner

import "testing"

func TestPartialFailurePolicy_Default(t *testing.T) {
	if string(PartialFailureLeave) != "leave" {
		t.Fatalf("PartialFailureLeave = %q, want \"leave\"", PartialFailureLeave)
	}
	if string(PartialFailureRollback) != "rollback" {
		t.Fatalf("PartialFailureRollback = %q, want \"rollback\"", PartialFailureRollback)
	}
	if string(PartialFailureRetryFailed) != "retry-failed" {
		t.Fatalf("PartialFailureRetryFailed = %q, want \"retry-failed\"", PartialFailureRetryFailed)
	}
}
