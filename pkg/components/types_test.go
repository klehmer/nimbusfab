package components_test

import (
	"encoding/json"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/components"
)

func TestDefaultRegistry_HasAllFourTypes(t *testing.T) {
	r := components.DefaultRegistry()
	for _, name := range []string{"network", "compute", "database", "storage"} {
		if _, ok := r.Type(name); !ok {
			t.Errorf("DefaultRegistry missing type %q", name)
		}
	}
}

func TestTypes_SchemasAreValidJSON(t *testing.T) {
	for _, tp := range []components.Type{components.Network(), components.Compute(), components.Database(), components.Storage()} {
		var v any
		if err := json.Unmarshal(tp.SpecSchema(), &v); err != nil {
			t.Errorf("%s schema not valid JSON: %v", tp.Name(), err)
		}
	}
}

func TestTypes_OutputsDeclared(t *testing.T) {
	cases := map[string][]string{
		"network":  {"vpc_id", "subnet_ids", "route_table_ids"},
		"compute":  {"instance_ids", "private_ips", "security_group_id"},
		"database": {"endpoint", "port", "db_name"},
		"storage":  {"bucket_name", "bucket_arn", "bucket_url"},
	}
	r := components.DefaultRegistry()
	for typeName, wantOutputs := range cases {
		tp, _ := r.Type(typeName)
		outputs := tp.Outputs()
		for _, name := range wantOutputs {
			if _, ok := outputs[name]; !ok {
				t.Errorf("%s missing output %q", typeName, name)
			}
		}
	}
}
