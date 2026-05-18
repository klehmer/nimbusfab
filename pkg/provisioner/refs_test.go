package provisioner

import "testing"

func TestCamelToSnake(t *testing.T) {
	cases := map[string]string{
		"subnetId":  "subnet_id",
		"vpcId":     "vpc_id",
		"subnetIds": "subnet_ids",
		"simple":    "simple",
		"":          "",
	}
	for in, want := range cases {
		got := camelToSnake(in)
		if got != want {
			t.Errorf("camelToSnake(%q) = %q, want %q", in, got, want)
		}
	}
}
