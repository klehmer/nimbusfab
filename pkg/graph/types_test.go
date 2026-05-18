package graph

import "testing"

func TestDirectionDefault(t *testing.T) {
	in := Input{}
	if got := in.DirectionOrDefault(); got != "tb" {
		t.Errorf("DirectionOrDefault: got %q, want \"tb\"", got)
	}
	if got := (Input{Direction: "lr"}).DirectionOrDefault(); got != "lr" {
		t.Errorf("DirectionOrDefault: got %q, want \"lr\"", got)
	}
	if got := (Input{Direction: "bogus"}).DirectionOrDefault(); got != "tb" {
		t.Errorf("DirectionOrDefault on bogus value: got %q, want fallback \"tb\"", got)
	}
}
