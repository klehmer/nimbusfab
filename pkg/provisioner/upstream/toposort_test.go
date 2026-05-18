package upstream

import (
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func mkComp(name string, refs ...string) ir.Component {
	out := ir.Component{Name: name}
	for _, r := range refs {
		out.Refs = append(out.Refs, ir.ComponentRef{Component: r, Output: "x", As: "x"})
	}
	return out
}

func names(cs []ir.Component) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func TestToposort_Empty(t *testing.T) {
	got, err := Toposort(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestToposort_Single(t *testing.T) {
	got, err := Toposort([]ir.Component{mkComp("a")})
	if err != nil || len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %v, err=%v", names(got), err)
	}
}

func TestToposort_Linear(t *testing.T) {
	in := []ir.Component{mkComp("c", "b"), mkComp("b", "a"), mkComp("a")}
	got, err := Toposort(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if strings.Join(names(got), ",") != "a,b,c" {
		t.Fatalf("got %v want a,b,c", names(got))
	}
}

func TestToposort_Diamond(t *testing.T) {
	in := []ir.Component{mkComp("d", "b", "c"), mkComp("b", "a"), mkComp("c", "a"), mkComp("a")}
	got, err := Toposort(in)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	pos := map[string]int{}
	for i, c := range got {
		pos[c.Name] = i
	}
	if pos["a"] >= pos["b"] || pos["a"] >= pos["c"] || pos["b"] >= pos["d"] || pos["c"] >= pos["d"] {
		t.Fatalf("bad order: %v", names(got))
	}
}

func TestToposort_StableSecondary(t *testing.T) {
	in := []ir.Component{mkComp("z"), mkComp("a"), mkComp("m")}
	got, _ := Toposort(in)
	if strings.Join(names(got), ",") != "a,m,z" {
		t.Fatalf("got %v want a,m,z", names(got))
	}
}

func TestToposort_CycleIsInternalError(t *testing.T) {
	in := []ir.Component{mkComp("a", "b"), mkComp("b", "a")}
	if _, err := Toposort(in); err == nil {
		t.Fatalf("expected cycle error")
	}
}
