package inventory_test

import (
	"encoding/json"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/inventory"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestComponent_UnmarshalIR_RefsRoundTrip(t *testing.T) {
	original := ir.Component{
		Name: "app", Type: "compute",
		Refs: []ir.ComponentRef{
			{Component: "net", Output: "subnet_ids", As: "subnetId"},
			{Component: "db", Output: "endpoint", As: "dbHost"},
		},
	}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	comp := inventory.Component{IRJSON: body}
	got, err := comp.UnmarshalIR()
	if err != nil {
		t.Fatalf("UnmarshalIR: %v", err)
	}
	if got.Name != "app" || got.Type != "compute" {
		t.Errorf("basic fields: %+v", got)
	}
	if len(got.Refs) != 2 {
		t.Fatalf("expected 2 refs; got %d", len(got.Refs))
	}
	if got.Refs[0].Component != "net" || got.Refs[0].Output != "subnet_ids" {
		t.Errorf("ref[0]=%+v", got.Refs[0])
	}
}

func TestComponent_UnmarshalIR_EmptyJSON(t *testing.T) {
	got, err := (&inventory.Component{}).UnmarshalIR()
	if err != nil {
		t.Errorf("empty IRJSON should not error: %v", err)
	}
	if got.Name != "" || len(got.Refs) != 0 {
		t.Errorf("expected zero-value ir.Component, got %+v", got)
	}
}

func TestComponent_UnmarshalIR_Malformed(t *testing.T) {
	comp := inventory.Component{IRJSON: []byte("not json")}
	_, err := comp.UnmarshalIR()
	if err == nil {
		t.Error("expected error on malformed JSON")
	}
}
