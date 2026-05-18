package graph

import (
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "regenerate testdata/*.golden.svg files")

func TestRenderSVG_ValidXML(t *testing.T) {
	out, _ := Layout(Input{Components: []Component{{Name: "net"}, {Name: "app",
		Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}}}})
	svg := RenderSVG(out)
	var v any
	if err := xml.Unmarshal(svg, &v); err != nil {
		t.Fatalf("RenderSVG produced invalid XML: %v\nbody:\n%s", err, svg)
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Errorf("RenderSVG output missing <svg> root")
	}
}

func TestRenderSVG_DiamondGolden(t *testing.T) {
	in := Input{Components: []Component{
		{Name: "d", Refs: []Ref{{Component: "b", Output: "x", As: "b"}, {Component: "c", Output: "x", As: "c"}}},
		{Name: "b", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "c", Refs: []Ref{{Component: "a", Output: "x", As: "a"}}},
		{Name: "a"},
	}}
	out, _ := Layout(in)
	got := RenderSVG(out)
	goldenPath := filepath.Join("testdata", "diamond_tb.golden.svg")
	if *updateGolden {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (rerun with -update-golden to create it)", err)
	}
	if string(got) != string(want) {
		t.Errorf("rendered SVG differs from golden.\nGot:\n%s\nWant:\n%s", got, want)
	}
}

func TestRenderSVG_UnmatchedEdgeIsDashed(t *testing.T) {
	out, _ := Layout(Input{
		Components: []Component{
			{Name: "app", Refs: []Ref{{Component: "net", Output: "vpc_id", As: "v"}}},
			{Name: "net"},
		},
		PairingErrors: []PairingError{
			{Component: "app", Ref: Ref{Component: "net", Output: "vpc_id", As: "v"}, Cloud: "aws", Region: "us-east-2"},
		},
	})
	svg := RenderSVG(out)
	if !strings.Contains(string(svg), `stroke-dasharray`) {
		t.Errorf("unmatched edge should use stroke-dasharray, got:\n%s", svg)
	}
}
