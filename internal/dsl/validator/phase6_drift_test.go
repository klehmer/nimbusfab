package validator

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPhase6_DriftOK(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{
		"dev": {Name: "dev", Drift: &ir.DriftConfig{Interval: "4h"}},
	}}
	rep := &ir.ValidationReport{}
	if err := phase6Drift(proj, rep); err != nil {
		t.Fatalf("phase6: %v", err)
	}
	if len(rep.Issues) != 0 {
		t.Errorf("expected no issues; got %+v", rep.Issues)
	}
}

func TestPhase6_DriftIntervalInvalid(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{
		"dev": {Name: "dev", Drift: &ir.DriftConfig{Interval: "not-a-duration"}},
	}}
	rep := &ir.ValidationReport{}
	_ = phase6Drift(proj, rep)
	if len(rep.Issues) != 1 || rep.Issues[0].Code != "ErrValidatorDriftIntervalInvalid" {
		t.Errorf("expected ErrValidatorDriftIntervalInvalid; got %+v", rep.Issues)
	}
}

func TestPhase6_DriftIntervalTooShort(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{
		"dev": {Name: "dev", Drift: &ir.DriftConfig{Interval: "30s"}},
	}}
	rep := &ir.ValidationReport{}
	_ = phase6Drift(proj, rep)
	if len(rep.Issues) != 1 || rep.Issues[0].Code != "ErrValidatorDriftIntervalTooShort" {
		t.Errorf("expected ErrValidatorDriftIntervalTooShort; got %+v", rep.Issues)
	}
}

func TestPhase6_DriftAbsent(t *testing.T) {
	proj := &ir.Project{Stacks: map[string]ir.Stack{"dev": {Name: "dev"}}}
	rep := &ir.ValidationReport{}
	_ = phase6Drift(proj, rep)
	if len(rep.Issues) != 0 {
		t.Errorf("absent Drift should be valid; got %+v", rep.Issues)
	}
}
