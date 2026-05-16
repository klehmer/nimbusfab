package validator

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestValidate_Schema_MissingRequiredName(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Stacks:     map[string]ir.Stack{"dev": {}},
		// Name intentionally absent.
	}
	report, err := New(components.DefaultRegistry()).Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrSchemaRequiredField") {
		t.Errorf("missing ErrSchemaRequiredField: %+v", report.Issues)
	}
}

func TestValidate_Schema_InvalidName(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "Has Spaces",
		Stacks:     map[string]ir.Stack{"dev": {}},
	}
	report, err := New(components.DefaultRegistry()).Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !hasCode(report, "ErrSchemaPattern") {
		t.Errorf("missing ErrSchemaPattern: %+v", report.Issues)
	}
}

func TestValidate_Schema_OK(t *testing.T) {
	proj := &ir.Project{
		APIVersion: ir.APIVersionV1Alpha1,
		Name:       "orders",
		Stacks:     map[string]ir.Stack{"dev": {}},
	}
	report, err := New(components.DefaultRegistry()).Validate(context.Background(), proj)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if hasCode(report, "ErrSchemaRequiredField") || hasCode(report, "ErrSchemaPattern") {
		t.Errorf("unexpected schema issue: %+v", report.Issues)
	}
}
