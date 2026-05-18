package ir

import (
	"encoding/json"
	"testing"
)

func TestStackDriftRoundtrip(t *testing.T) {
	in := Stack{Name: "dev", Drift: &DriftConfig{Interval: "4h"}}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Stack
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Drift == nil || out.Drift.Interval != "4h" {
		t.Errorf("Drift roundtrip failed: %+v", out)
	}
}
