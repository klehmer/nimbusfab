package graph

import "testing"

func TestLayout_Empty(t *testing.T) {
	out, err := Layout(Input{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out.Nodes) != 0 || len(out.Edges) != 0 {
		t.Errorf("expected empty layout; got %d nodes, %d edges", len(out.Nodes), len(out.Edges))
	}
}

func TestLayout_SingleNode_TB(t *testing.T) {
	out, err := Layout(Input{Components: []Component{{Name: "net", Type: "network"}}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out.Nodes) != 1 || out.Nodes[0].Name != "net" {
		t.Fatalf("nodes: %+v", out.Nodes)
	}
	if out.Nodes[0].X < 0 || out.Nodes[0].Y < 0 {
		t.Errorf("negative coordinates: %+v", out.Nodes[0])
	}
}

func TestLayout_LinearChain_TB(t *testing.T) {
	// app -> net (app refs net); rendered top-down with net above app.
	in := Input{Components: []Component{
		{Name: "app", Type: "compute", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
		{Name: "net", Type: "network"},
	}}
	out, err := Layout(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out.Nodes) != 2 || len(out.Edges) != 1 {
		t.Fatalf("nodes=%d edges=%d", len(out.Nodes), len(out.Edges))
	}
	byName := map[string]NodeBox{}
	for _, n := range out.Nodes {
		byName[n.Name] = n
	}
	if byName["net"].Y >= byName["app"].Y {
		t.Errorf("net.Y=%d should be above app.Y=%d", byName["net"].Y, byName["app"].Y)
	}
}

func TestLayout_Diamond_TB(t *testing.T) {
	// d depends on b,c; both depend on a. a must be on top.
	in := Input{Components: []Component{
		{Name: "d", Refs: []Ref{{Component: "b", Output: "x", As: "b"}, {Component: "c", Output: "x", As: "c"}}},
		{Name: "b", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "c", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "a"},
	}}
	out, _ := Layout(in)
	rank := map[string]int{}
	for _, n := range out.Nodes {
		rank[n.Name] = n.Y
	}
	if !(rank["a"] < rank["b"] && rank["a"] < rank["c"] && rank["b"] < rank["d"] && rank["c"] < rank["d"]) {
		t.Errorf("bad y-order: %+v", rank)
	}
	if len(out.Edges) != 4 {
		t.Errorf("want 4 edges, got %d", len(out.Edges))
	}
}

func TestLayout_PairingErrorEdgeKind(t *testing.T) {
	in := Input{
		Components: []Component{
			{Name: "app", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
			{Name: "net"},
		},
		PairingErrors: []PairingError{
			{Component: "app", Ref: Ref{Component: "net", Output: "vpc_id", As: "v"}, Cloud: "aws", Region: "us-east-2", Reason: "no match"},
		},
	}
	out, _ := Layout(in)
	if len(out.Edges) != 1 || out.Edges[0].Kind != "unmatched" {
		t.Errorf("edges: %+v", out.Edges)
	}
	if !out.HasErrors {
		t.Error("HasErrors should be true")
	}
}

func TestLayout_LinearChain_LR(t *testing.T) {
	// app -> net; LR should put net LEFT of app.
	in := Input{
		Direction: "lr",
		Components: []Component{
			{Name: "app", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
			{Name: "net"},
		},
	}
	out, err := Layout(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	byName := map[string]NodeBox{}
	for _, n := range out.Nodes {
		byName[n.Name] = n
	}
	if byName["net"].X >= byName["app"].X {
		t.Errorf("net.X=%d should be left of app.X=%d", byName["net"].X, byName["app"].X)
	}
	// All Y values should be in the same column when there's one node per rank.
	if byName["net"].Y != byName["app"].Y {
		t.Errorf("net.Y=%d app.Y=%d should be equal (one-per-rank LR layout)", byName["net"].Y, byName["app"].Y)
	}
	if out.Direction != "lr" {
		t.Errorf("Direction: got %q", out.Direction)
	}
}

func TestLayout_Diamond_LR(t *testing.T) {
	in := Input{
		Direction: "lr",
		Components: []Component{
			{Name: "d", Refs: []Ref{{Component: "b", Output: "x", As: "b"}, {Component: "c", Output: "x", As: "c"}}},
			{Name: "b", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
			{Name: "c", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
			{Name: "a"},
		},
	}
	out, _ := Layout(in)
	x := map[string]int{}
	for _, n := range out.Nodes {
		x[n.Name] = n.X
	}
	if !(x["a"] < x["b"] && x["a"] < x["c"] && x["b"] < x["d"] && x["c"] < x["d"]) {
		t.Errorf("bad x-order: %+v", x)
	}
}
