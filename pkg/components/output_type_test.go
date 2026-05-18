package components

import "testing"

func TestOutputType_TofuType(t *testing.T) {
	tests := []struct {
		kind, want string
	}{
		{"string", "string"},
		{"list<string>", "list(string)"},
		{"integer", "number"},
		{"number", "number"},
		{"bool", "bool"},
		{"", "string"}, // default
		{"unknown_kind", "string"},
	}
	for _, tc := range tests {
		got := OutputType{Kind: tc.kind}.TofuType()
		if got != tc.want {
			t.Errorf("Kind=%q: got %q, want %q", tc.kind, got, tc.want)
		}
	}
}
