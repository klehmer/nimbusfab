package parity_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/parity"
)

func TestRenderText_BasicShape(t *testing.T) {
	rep := &parity.ParityReport{
		Component: "orders-db", Type: "database", Size: "small",
		Score: 0.85,
		Targets: []parity.TargetProfile{
			{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
				Class: "database", SKU: "db.t3.medium",
				Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 250}},
			}},
		},
		Warnings: []string{"aws/us-east-1: example warning"},
	}
	var buf bytes.Buffer
	parity.RenderText(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "orders-db") {
		t.Errorf("missing component: %s", out)
	}
	if !strings.Contains(out, "Parity score:") {
		t.Errorf("missing score line: %s", out)
	}
	if !strings.Contains(out, "Warnings:") || !strings.Contains(out, "example warning") {
		t.Errorf("missing warnings: %s", out)
	}
}

func TestRenderViolations_Empty(t *testing.T) {
	var buf bytes.Buffer
	parity.RenderViolations(&buf, nil)
	if !strings.Contains(buf.String(), "none") {
		t.Errorf("expected 'none' for empty: %s", buf.String())
	}
}

func TestRenderViolations_Populated(t *testing.T) {
	var buf bytes.Buffer
	parity.RenderViolations(&buf, []parity.Violation{
		{Component: "db", Attribute: "compute.vCPU", Policy: "exact", Detail: "differ", Action: "warn"},
	})
	out := buf.String()
	if !strings.Contains(out, "[warn]") || !strings.Contains(out, "compute.vCPU") {
		t.Errorf("missing violation: %s", out)
	}
}
