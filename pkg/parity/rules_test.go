package parity_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/parity"
)

func sampleReport() *parity.ParityReport {
	return &parity.ParityReport{
		Component: "db", Type: "database", Size: "small",
		Score: 0.6,
		Comparisons: []parity.AttrComparison{
			{Attribute: "compute.memoryGB", Kind: "numeric", MinValue: 4.0, MaxValue: 8.0, Score: 0.5},
			{Attribute: "compute.vCPU", Kind: "numeric", MinValue: 2.0, MaxValue: 2.0, AllMatch: true, Score: 1.0},
			{Attribute: "features.multiAZ", Kind: "boolean",
				Values: map[string]any{"aws/us-east-1": true, "gcp/us-central1": false}, AllMatch: false, Score: 0},
		},
	}
}

func TestRules_NoRules_NoViolations(t *testing.T) {
	e, _ := parity.NewEngine()
	v, err := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{})
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	if len(v) != 0 {
		t.Errorf("no rules should yield no violations, got %d", len(v))
	}
}

func TestRules_DefaultMinScore_BelowThreshold(t *testing.T) {
	e, _ := parity.NewEngine()
	v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
		Default: parity.ModeRules{Mode: "warn", MinScore: 0.8},
	})
	if len(v) != 1 || v[0].Policy != "minScore" {
		t.Errorf("expected one minScore violation, got %+v", v)
	}
	if v[0].Action != "warn" {
		t.Errorf("action = %q", v[0].Action)
	}
}

func TestRules_PerComponent_ExactPolicy(t *testing.T) {
	e, _ := parity.NewEngine()
	v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
		Components: map[string]parity.ComponentRules{
			"db": {Mode: "block", Attributes: map[string]parity.AttributePolicy{
				"compute.memoryGB": {Policy: "exact"},
			}},
		},
	})
	if len(v) != 1 || v[0].Policy != "exact" || v[0].Action != "block" {
		t.Errorf("expected one block exact violation, got %+v", v)
	}
}

func TestRules_PerComponent_MaxRatio(t *testing.T) {
	e, _ := parity.NewEngine()
	v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
		Components: map[string]parity.ComponentRules{
			"db": {Mode: "warn", Attributes: map[string]parity.AttributePolicy{
				"compute.memoryGB": {Policy: "maxRatio", MaxRatio: 1.5},
			}},
		},
	})
	if len(v) != 1 || v[0].Policy != "maxRatio" {
		t.Errorf("expected one maxRatio violation, got %+v", v)
	}
}

func TestRules_PerComponent_RequireAll(t *testing.T) {
	e, _ := parity.NewEngine()
	v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
		Components: map[string]parity.ComponentRules{
			"db": {Mode: "block", Attributes: map[string]parity.AttributePolicy{
				"features.multiAZ": {Policy: "requireAll"},
			}},
		},
	})
	if len(v) != 1 || v[0].Policy != "requireAll" {
		t.Errorf("expected one requireAll violation, got %+v", v)
	}
}

func TestRules_OffMode_NoViolations(t *testing.T) {
	e, _ := parity.NewEngine()
	v, _ := e.EvaluateRules(context.Background(), sampleReport(), parity.ProjectRules{
		Default: parity.ModeRules{Mode: "block", MinScore: 0.9},
		Components: map[string]parity.ComponentRules{
			"db": {Mode: "off"},
		},
	})
	if len(v) != 0 {
		t.Errorf("off mode should suppress: %+v", v)
	}
}
