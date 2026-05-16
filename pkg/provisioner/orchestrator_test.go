package provisioner

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestTopoSort_LinearChain(t *testing.T) {
	comps := []ir.Component{
		{Name: "c", Refs: []ir.ComponentRef{{Component: "b"}}},
		{Name: "a"},
		{Name: "b", Refs: []ir.ComponentRef{{Component: "a"}}},
	}
	order, err := topoSort(comps)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	if len(order) != 3 || order[0].Name != "a" || order[1].Name != "b" || order[2].Name != "c" {
		t.Errorf("order = %v", names(order))
	}
}

func TestTopoSort_Diamond(t *testing.T) {
	comps := []ir.Component{
		{Name: "a"},
		{Name: "b", Refs: []ir.ComponentRef{{Component: "a"}}},
		{Name: "d", Refs: []ir.ComponentRef{{Component: "a"}}},
		{Name: "c", Refs: []ir.ComponentRef{{Component: "b"}, {Component: "d"}}},
	}
	order, err := topoSort(comps)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	pos := positions(order)
	if pos["a"] >= pos["b"] || pos["a"] >= pos["d"] {
		t.Errorf("a must come before b and d: %v", names(order))
	}
	if pos["b"] >= pos["c"] || pos["d"] >= pos["c"] {
		t.Errorf("b and d must come before c: %v", names(order))
	}
}

func TestTopoSort_Cycle(t *testing.T) {
	comps := []ir.Component{
		{Name: "a", Refs: []ir.ComponentRef{{Component: "b"}}},
		{Name: "b", Refs: []ir.ComponentRef{{Component: "a"}}},
	}
	if _, err := topoSort(comps); err == nil {
		t.Fatal("topoSort: nil err for cycle")
	}
}

func TestTopoSort_UnknownRef(t *testing.T) {
	comps := []ir.Component{
		{Name: "a", Refs: []ir.ComponentRef{{Component: "ghost"}}},
	}
	if _, err := topoSort(comps); err == nil {
		t.Fatal("topoSort: nil err for unknown ref")
	}
}

func TestTopoSort_Duplicate(t *testing.T) {
	comps := []ir.Component{
		{Name: "a"},
		{Name: "a"},
	}
	if _, err := topoSort(comps); err == nil {
		t.Fatal("topoSort: nil err for duplicate")
	}
}

func names(cs []ir.Component) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func positions(cs []ir.Component) map[string]int {
	out := map[string]int{}
	for i, c := range cs {
		out[c.Name] = i
	}
	return out
}
