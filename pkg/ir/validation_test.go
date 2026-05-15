package ir

import "testing"

func TestValidationReport_OK(t *testing.T) {
	cases := []struct {
		name   string
		issues []Issue
		want   bool
	}{
		{"empty", nil, true},
		{"only warnings", []Issue{{Severity: SeverityWarning}}, true},
		{"only info", []Issue{{Severity: SeverityInfo}}, true},
		{"one error", []Issue{{Severity: SeverityError}}, false},
		{"mixed", []Issue{{Severity: SeverityWarning}, {Severity: SeverityError}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ValidationReport{Issues: c.issues}
			if got := r.OK(); got != c.want {
				t.Errorf("OK() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIssue_String(t *testing.T) {
	i := Issue{
		Severity: SeverityError,
		Code:     "ErrSchemaRequiredField",
		Message:  "required field missing",
		Path:     "components[0].name",
		Source:   Source{File: "components/orders-db.yaml", Line: 3, Column: 1},
	}
	got := i.String()
	want := "components/orders-db.yaml:3:1: error: ErrSchemaRequiredField: required field missing (at components[0].name)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
