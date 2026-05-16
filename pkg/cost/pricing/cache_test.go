package pricing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cost/pricing"
)

func TestCache_LookupKnownEC2(t *testing.T) {
	c := pricing.NewCache()
	entry, err := c.Lookup(context.Background(), "aws", map[string]any{
		"service": "AmazonEC2", "instanceType": "t3.small", "region": "us-east-1",
		"tenancy": "Shared", "operatingSystem": "Linux", "preInstalledSw": "NA", "capacitystatus": "Used",
	})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if entry.UnitPrice != 0.0208 {
		t.Errorf("unitPrice = %v, want 0.0208", entry.UnitPrice)
	}
	if entry.UnitOfMeasure != "Hrs" {
		t.Errorf("unitOfMeasure = %q", entry.UnitOfMeasure)
	}
	if entry.Source != "snapshot" {
		t.Errorf("source = %q", entry.Source)
	}
}

func TestCache_LookupUnknownCloud(t *testing.T) {
	c := pricing.NewCache()
	_, err := c.Lookup(context.Background(), "azure", map[string]any{"foo": "bar"})
	if !errors.Is(err, pricing.ErrPricingMissing) {
		t.Errorf("expected ErrPricingMissing, got %v", err)
	}
}

func TestCache_LookupUnknownKey(t *testing.T) {
	c := pricing.NewCache()
	_, err := c.Lookup(context.Background(), "aws", map[string]any{"instanceType": "exotic.nonexistent"})
	if !errors.Is(err, pricing.ErrPricingMissing) {
		t.Errorf("expected ErrPricingMissing, got %v", err)
	}
}

func TestCache_RefreshNotImplemented(t *testing.T) {
	c := pricing.NewCache()
	err := c.Refresh(context.Background(), "aws", nil)
	if !errors.Is(err, pricing.ErrNotImplementedYet) {
		t.Errorf("expected ErrNotImplementedYet, got %v", err)
	}
}

func TestAsPricingProvider_RoundTrip(t *testing.T) {
	c := pricing.NewCache()
	p := pricing.AsPricingProvider(c)
	unit, err := p.Price(context.Background(), "aws", map[string]any{
		"service": "AmazonEC2", "instanceType": "t3.medium", "region": "us-east-1",
		"tenancy": "Shared", "operatingSystem": "Linux", "preInstalledSw": "NA", "capacitystatus": "Used",
	})
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if unit.UnitPrice != 0.0416 {
		t.Errorf("unitPrice = %v", unit.UnitPrice)
	}
}
