package parity_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/parity"
)

func TestEngine_Compare_BuildsReport(t *testing.T) {
	e, err := parity.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	rep, err := e.Compare(context.Background(), parity.CompareInput{
		Component: "orders-db", Type: "database", Size: "small",
		Targets: []parity.TargetProfile{
			{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
				Class: "database",
				Database: &parity.DatabaseProfile{
					Engine: "postgres", Version: "16",
					Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 2},
					Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"},
				},
				Features: map[string]bool{"pointInTimeRestore": true},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if rep.Component != "orders-db" || rep.Type != "database" || rep.Size != "small" {
		t.Errorf("report identity: %+v", rep)
	}
	if rep.Contract.Type != "database" {
		t.Errorf("contract not populated: %+v", rep.Contract)
	}
	if rep.Score < 0.99 {
		t.Errorf("single-target score = %f, want ~1.0", rep.Score)
	}
}

func TestEngine_Compare_RecordsFloorWarning(t *testing.T) {
	e, _ := parity.NewEngine()
	rep, _ := e.Compare(context.Background(), parity.CompareInput{
		Component: "tiny-db", Type: "database", Size: "small",
		Targets: []parity.TargetProfile{
			{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
				Class: "database",
				Database: &parity.DatabaseProfile{
					Engine:  "postgres",
					Compute: parity.ComputeProfile{VCPU: 1, MemoryGB: 1},
					Storage: parity.StorageProfile{SizeGB: 50, Class: "ssd"},
				},
			}},
		},
	})
	if len(rep.Warnings) == 0 {
		t.Error("expected floor warning for below-spec database")
	}
}

func TestEngine_Compare_ExplicitSize_NoContract(t *testing.T) {
	e, _ := parity.NewEngine()
	rep, _ := e.Compare(context.Background(), parity.CompareInput{
		Component: "custom", Type: "compute", Size: "",
		Targets: []parity.TargetProfile{
			{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
				Class:   "compute",
				Compute: &parity.ComputeProfile{VCPU: 8, MemoryGB: 32},
			}},
		},
	})
	if rep.Contract.Type != "" {
		t.Errorf("expected empty contract for size=\"\", got %+v", rep.Contract)
	}
}
