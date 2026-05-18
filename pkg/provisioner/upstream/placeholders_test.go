package upstream

import (
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/components"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPlanPlaceholders_NetworkRef(t *testing.T) {
	reg := components.DefaultRegistry()
	refs := []ir.ComponentRef{
		{Component: "web-network", Output: "vpc_id", As: "vpcId"},
		{Component: "web-network", Output: "subnet_ids", As: "subnetIds"},
	}
	netComp := ir.Component{Name: "web-network", Type: "network"}
	got, err := PlanPlaceholders(refs, []ir.Component{netComp}, reg)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if want := `"__nimbusfab_placeholder_upstream_web_network_vpc_id__"`; got["upstream_web_network_vpc_id"] != want {
		t.Errorf("vpc_id: got %q want %q", got["upstream_web_network_vpc_id"], want)
	}
	v := got["upstream_web_network_subnet_ids"]
	if !strings.HasPrefix(v, `["__nimbusfab_placeholder_`) || !strings.HasSuffix(v, `__"]`) {
		t.Errorf("subnet_ids placeholder shape unexpected: %q", v)
	}
}

func TestPlanPlaceholders_DatabaseRef(t *testing.T) {
	reg := components.DefaultRegistry()
	refs := []ir.ComponentRef{{Component: "db", Output: "port", As: "port"}}
	dbComp := ir.Component{Name: "db", Type: "database"}
	got, err := PlanPlaceholders(refs, []ir.Component{dbComp}, reg)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got["upstream_db_port"] != "0" {
		t.Errorf("port: got %q want 0", got["upstream_db_port"])
	}
}

func TestPlanPlaceholders_UnknownComponentIgnored(t *testing.T) {
	reg := components.DefaultRegistry()
	refs := []ir.ComponentRef{{Component: "ghost", Output: "x", As: "x"}}
	got, err := PlanPlaceholders(refs, nil, reg)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map; got %v", got)
	}
}
