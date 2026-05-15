package validator

import (
	"context"
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/internal/dsl/yamlnode"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestValidator_LiftsLoaderErrorAsIssue(t *testing.T) {
	v := New()
	loaderErr := &yamlnode.Error{
		Source: ir.Source{File: "project.yaml", Line: 3, Column: 5},
		Err:    errors.New("mapping values are not allowed here"),
	}
	report, err := v.ValidateLoaderError(context.Background(), loaderErr)
	if err != nil {
		t.Fatalf("ValidateLoaderError: %v", err)
	}
	if report.OK() {
		t.Fatal("report.OK() = true, want false")
	}
	if len(report.Issues) != 1 {
		t.Fatalf("Issues = %d, want 1", len(report.Issues))
	}
	got := report.Issues[0]
	if got.Severity != ir.SeverityError {
		t.Errorf("Severity = %v", got.Severity)
	}
	if got.Code != "ErrYAMLMalformed" {
		t.Errorf("Code = %q", got.Code)
	}
	if got.Source.File != "project.yaml" || got.Source.Line != 3 {
		t.Errorf("Source = %+v", got.Source)
	}
}
