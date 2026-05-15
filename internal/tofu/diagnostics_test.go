package tofu

import (
	"strings"
	"testing"
)

func TestParseDiagnostic_StateLock(t *testing.T) {
	raw := `{"@level":"error","@message":"Error acquiring the state lock","@module":"terraform.ui","type":"diagnostic","diagnostic":{"severity":"error","summary":"Error acquiring the state lock","detail":"Lock Info:\n  ID:        abc-123\n"}}`
	diag, err := ParseDiagnostic(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseDiagnostic: %v", err)
	}
	if diag.Code != ErrTofuStateLocked {
		t.Errorf("Code = %q, want %q", diag.Code, ErrTofuStateLocked)
	}
}

func TestParseDiagnostic_NonError(t *testing.T) {
	raw := `{"@level":"info","@message":"Initializing","type":"version"}`
	diag, err := ParseDiagnostic(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseDiagnostic: %v", err)
	}
	if diag.Code != "" {
		t.Errorf("non-error event should yield empty Code, got %q", diag.Code)
	}
}

func TestParseDiagnostic_OpaqueDiagnostic(t *testing.T) {
	raw := `{"type":"diagnostic","diagnostic":{"severity":"error","summary":"some unrelated failure","detail":""}}`
	diag, _ := ParseDiagnostic(strings.NewReader(raw))
	if diag.Code != ErrTofuDiagnostic {
		t.Errorf("Code = %q, want %q", diag.Code, ErrTofuDiagnostic)
	}
}
