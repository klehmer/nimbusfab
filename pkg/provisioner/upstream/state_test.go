package upstream

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractOutputs_WellFormed(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "sample_state.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got, err := ExtractOutputs(body)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["vpc_id"] != "vpc-abc123" {
		t.Errorf("vpc_id=%v", got["vpc_id"])
	}
	subnets, ok := got["subnet_ids"].([]any)
	if !ok || len(subnets) != 2 || subnets[0] != "subnet-1" {
		t.Errorf("subnet_ids=%v", got["subnet_ids"])
	}
	if got["port"] != float64(5432) {
		t.Errorf("port=%v (%T)", got["port"], got["port"])
	}
}

func TestExtractOutputs_MalformedJSON(t *testing.T) {
	_, err := ExtractOutputs([]byte("not json"))
	if !errors.Is(err, ErrUpstreamStateUnreadable) {
		t.Fatalf("got %v", err)
	}
}

func TestExtractOutputs_NoOutputsField(t *testing.T) {
	got, err := ExtractOutputs([]byte(`{"version":4}`))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}
