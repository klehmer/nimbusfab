package upstream

import "testing"

func TestVarName(t *testing.T) {
	tests := []struct {
		comp, output, want string
	}{
		{"web-network", "vpc_id", "upstream_web_network_vpc_id"},
		{"WebNet", "subnet_ids", "upstream_webnet_subnet_ids"},
		{"3net", "x", "upstream__3net_x"},
		{"orders-db", "endpoint", "upstream_orders_db_endpoint"},
	}
	for _, tc := range tests {
		if got := VarName(tc.comp, tc.output); got != tc.want {
			t.Errorf("VarName(%q,%q)=%q want %q", tc.comp, tc.output, got, tc.want)
		}
	}
}
