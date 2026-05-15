package ir

import "testing"

func TestSource_String(t *testing.T) {
	cases := []struct {
		name string
		src  Source
		want string
	}{
		{"with column", Source{File: "components/orders-db.yaml", Line: 12, Column: 5}, "components/orders-db.yaml:12:5"},
		{"no column", Source{File: "project.yaml", Line: 1}, "project.yaml:1"},
		{"no line", Source{File: "stack.yaml"}, "stack.yaml"},
		{"empty", Source{}, "<unknown>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.src.String(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
