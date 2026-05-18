package graph

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
	"github.com/klehmer/nimbusfab/pkg/provisioner/upstream"
)

func TestFromIR_BasicRoundTrip(t *testing.T) {
	irComps := []ir.Component{
		{Name: "app", Type: "compute", Refs: []ir.ComponentRef{
			{Component: "net", Output: "subnet_ids", As: "subnetId"},
		}},
		{Name: "net", Type: "network"},
	}
	pairs := []upstream.PairingError{
		{Component: "app", Ref: ir.ComponentRef{Component: "net", Output: "subnet_ids", As: "subnetId"},
			Cloud: "aws", Region: "us-west-2", Reason: "no match"},
	}
	gComps, gPairs := FromIR(irComps, pairs)
	if len(gComps) != 2 {
		t.Fatalf("want 2 components, got %d", len(gComps))
	}
	if gComps[0].Name != "app" || len(gComps[0].Refs) != 1 || gComps[0].Refs[0].Component != "net" {
		t.Errorf("component[0]: %+v", gComps[0])
	}
	if len(gPairs) != 1 || gPairs[0].Component != "app" || gPairs[0].Ref.Component != "net" {
		t.Errorf("pair[0]: %+v", gPairs[0])
	}
}

func TestFromIR_EmptyInputs(t *testing.T) {
	gComps, gPairs := FromIR(nil, nil)
	if len(gComps) != 0 || len(gPairs) != 0 {
		t.Errorf("expected empty outputs; got %d comps, %d pairs", len(gComps), len(gPairs))
	}
}
