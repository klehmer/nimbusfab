package parity_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/parity"
)

func TestLoadContracts_AllFourTypes(t *testing.T) {
	c, err := parity.LoadContracts()
	if err != nil {
		t.Fatalf("LoadContracts: %v", err)
	}
	for _, typeName := range []string{"database", "compute", "storage", "network"} {
		if len(c.SizesFor(typeName)) == 0 {
			t.Errorf("no sizes for type %q", typeName)
		}
	}
}

func TestLookup_AllFourSizes(t *testing.T) {
	c, _ := parity.LoadContracts()
	for _, size := range []string{"small", "medium", "large", "xlarge"} {
		if _, ok := c.Lookup("database", size); !ok {
			t.Errorf("database/%s missing", size)
		}
		if _, ok := c.Lookup("compute", size); !ok {
			t.Errorf("compute/%s missing", size)
		}
	}
}

func TestLookup_UnknownTypeOrSize(t *testing.T) {
	c, _ := parity.LoadContracts()
	if _, ok := c.Lookup("nonexistent", "small"); ok {
		t.Error("unknown type should return ok=false")
	}
	if _, ok := c.Lookup("database", "tiny"); ok {
		t.Error("unknown size should return ok=false")
	}
}

func TestDatabaseSmall_HasExpectedFloors(t *testing.T) {
	c, _ := parity.LoadContracts()
	f, _ := c.Lookup("database", "small")
	if f.Compute == nil || f.Compute.MinVCPU != 2 || f.Compute.MinMemoryGB != 2 {
		t.Errorf("small compute floor: %+v", f.Compute)
	}
	if f.Storage == nil || f.Storage.MinSizeGB != 100 {
		t.Errorf("small storage floor: %+v", f.Storage)
	}
	if f.Features["pointInTimeRestore"] != "required" {
		t.Errorf("PITR feature: %v", f.Features)
	}
}
