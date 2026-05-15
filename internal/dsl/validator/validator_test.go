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

func TestValidate_APIVersionMissing(t *testing.T) {
	proj := &ir.Project{Name: "x", Stacks: map[string]ir.Stack{"dev": {}}}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrMissingAPIVersion") {
		t.Errorf("missing ErrMissingAPIVersion: %+v", report.Issues)
	}
}

func TestValidate_APIVersionUnknown(t *testing.T) {
	proj := &ir.Project{APIVersion: "infra.dev/v999", Name: "x", Stacks: map[string]ir.Stack{"dev": {}}}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrUnknownAPIVersion") {
		t.Errorf("missing ErrUnknownAPIVersion: %+v", report.Issues)
	}
}

func TestValidate_APIVersionOK(t *testing.T) {
	proj := &ir.Project{
		APIVersion: "infra.dev/v1alpha1",
		Name:       "x",
		Stacks:     map[string]ir.Stack{"dev": {}},
	}
	report, err := New().Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if hasCode(report, "ErrMissingAPIVersion") || hasCode(report, "ErrUnknownAPIVersion") {
		t.Errorf("unexpected APIVersion issue: %+v", report.Issues)
	}
}

func hasCode(report *ir.ValidationReport, code string) bool {
	for _, i := range report.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
