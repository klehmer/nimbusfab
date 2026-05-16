package bridge_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klehmer/nimbusfab/internal/state/bridge"
)

func TestParse_OneVPC(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "state_one_vpc.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	snap, err := bridge.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if snap.TofuVersion != "1.7.0" {
		t.Errorf("TofuVersion = %q", snap.TofuVersion)
	}
	if snap.SerialNumber != 4 {
		t.Errorf("SerialNumber = %d", snap.SerialNumber)
	}
	if len(snap.Resources) != 1 {
		t.Fatalf("Resources len = %d", len(snap.Resources))
	}
	r := snap.Resources[0]
	if r.Address != "aws_vpc.web" || r.Type != "aws_vpc" || r.Name != "web" {
		t.Errorf("resource shape: %+v", r)
	}
	if r.CloudResourceID == "" {
		t.Error("CloudResourceID empty; expected arn or id fallback")
	}
	if r.AttributesHash == "" {
		t.Error("AttributesHash empty")
	}
	if snap.Outputs["vpc_id"] != "vpc-0abc123" {
		t.Errorf("Outputs[vpc_id] = %v", snap.Outputs["vpc_id"])
	}
}

func TestParse_EmptyState(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "state_empty.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	snap, err := bridge.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(snap.Resources) != 0 || len(snap.Outputs) != 0 {
		t.Errorf("expected empty snapshot, got %+v", snap)
	}
}

func TestParse_DeterministicAttributesHash(t *testing.T) {
	raw, _ := os.ReadFile(filepath.Join("testdata", "state_one_vpc.json"))
	a, _ := bridge.Parse(raw)
	b, _ := bridge.Parse(raw)
	if a.Resources[0].AttributesHash != b.Resources[0].AttributesHash {
		t.Errorf("attribute hash not deterministic: %q vs %q",
			a.Resources[0].AttributesHash, b.Resources[0].AttributesHash)
	}
}
