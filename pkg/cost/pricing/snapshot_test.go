package pricing_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cost/pricing"
)

func TestLoadSnapshots_AWSPresent(t *testing.T) {
	snaps, err := pricing.LoadSnapshots()
	if err != nil {
		t.Fatalf("LoadSnapshots: %v", err)
	}
	aws, ok := snaps["aws"]
	if !ok {
		t.Fatal("aws snapshot missing")
	}
	if len(aws.Entries) == 0 {
		t.Error("aws snapshot has 0 entries")
	}
	if aws.Currency != "USD" {
		t.Errorf("currency = %q", aws.Currency)
	}
}

func TestCanonicalKey_Deterministic(t *testing.T) {
	a := pricing.CanonicalKey(map[string]any{"region": "us-east-1", "service": "AmazonEC2"})
	b := pricing.CanonicalKey(map[string]any{"service": "AmazonEC2", "region": "us-east-1"})
	if a != b {
		t.Errorf("canonical not deterministic: %q vs %q", a, b)
	}
	if a != "region=us-east-1;service=AmazonEC2" {
		t.Errorf("canonical = %q", a)
	}
}

func TestCanonicalKey_DropsEmptyValues(t *testing.T) {
	got := pricing.CanonicalKey(map[string]any{"region": "us-east-1", "tenancy": "", "instanceType": nil})
	if got != "region=us-east-1" {
		t.Errorf("canonical = %q, want region=us-east-1", got)
	}
}
