package upstream

import (
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPreflightPairing_SameCloudRegionOK(t *testing.T) {
	comps := []ir.Component{
		{Name: "app", Type: "compute",
			Refs:    []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
		{Name: "net", Type: "network",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 0 {
		t.Errorf("expected no errors, got %+v", got)
	}
}

func TestPreflightPairing_CrossRegion(t *testing.T) {
	comps := []ir.Component{
		{Name: "app", Type: "compute",
			Refs:    []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-west-2"}}},
		{Name: "net", Type: "network",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(got), got)
	}
	if got[0].Component != "app" || got[0].Cloud != "aws" || got[0].Region != "us-west-2" {
		t.Errorf("error fields: %+v", got[0])
	}
}

func TestPreflightPairing_CrossCloud(t *testing.T) {
	comps := []ir.Component{
		{Name: "app",
			Refs:    []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{{Cloud: "azure", Region: "eastus"}}},
		{Name: "net",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(got), got)
	}
}

func TestPreflightPairing_MultipleTargetsMixed(t *testing.T) {
	// app exists in aws/east + azure/eastus. net exists only in aws/east.
	// Expect ONE pairing error (for the azure target only).
	comps := []ir.Component{
		{Name: "app",
			Refs: []ir.ComponentRef{{Component: "net", Output: "subnet_ids", As: "s"}},
			Targets: []ir.DeploymentTarget{
				{Cloud: "aws", Region: "us-east-1"},
				{Cloud: "azure", Region: "eastus"},
			}},
		{Name: "net",
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	got := PreflightPairing(comps)
	if len(got) != 1 {
		t.Fatalf("expected 1 error, got %d", len(got))
	}
	if got[0].Cloud != "azure" {
		t.Errorf("expected error for azure target, got %+v", got[0])
	}
}

func TestPreflightPairing_NoRefsNoErrors(t *testing.T) {
	comps := []ir.Component{
		{Name: "isolated", Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
	}
	if got := PreflightPairing(comps); len(got) != 0 {
		t.Errorf("expected no errors, got %+v", got)
	}
}

func TestPreflightPairing_ErrorWrapsSentinel(t *testing.T) {
	comps := []ir.Component{
		{Name: "app",
			Refs:    []ir.ComponentRef{{Component: "net", Output: "x", As: "x"}},
			Targets: []ir.DeploymentTarget{{Cloud: "aws", Region: "us-east-1"}}},
		{Name: "net",
			Targets: []ir.DeploymentTarget{{Cloud: "azure", Region: "eastus"}}},
	}
	got := PreflightPairing(comps)
	if len(got) == 0 {
		t.Fatal("expected at least one error")
	}
	wrapped := got[0].AsError()
	if !errors.Is(wrapped, ErrCrossTargetRefUnsupported) {
		t.Errorf("AsError() does not wrap ErrCrossTargetRefUnsupported: %v", wrapped)
	}
}
